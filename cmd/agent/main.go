package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/cenkalti/backoff/v5"
	"github.com/go-logr/logr"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/tinkerbell/tinkerbell/pkg/build"
	"github.com/tinkerbell/tinkerbell/pkg/otel"
	"github.com/tinkerbell/tinkerbell/tink/agent"
)

const (
	// name is the name of the agent.
	name = "tink-agent"
)

func main() {
	var exitCode int
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, r)
			fmt.Fprintln(os.Stderr, string(debug.Stack()))
			exitCode = 2
		}
		os.Exit(exitCode)
	}()

	ctx, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer done()

	// some CLI defaults are defined here. See the specific flag registration function. If a default is not defined there, it is set here.
	c := &config{
		Options: &agent.Options{
			Transport: agent.Transport{
				GRPC: agent.GRPCTransport{},
				NATS: agent.NATSTransport{
					ServerAddrPort: netip.AddrPortFrom(netip.IPv4Unspecified(), 0),
				},
			},
			TransportSelected: agent.GRPCTransportType,
			RuntimeSelected:   agent.DockerRuntimeType,
			BackoffOptions: agent.BackoffOptions{
				MaxInterval: backoff.DefaultMaxInterval,
			},
		},
	}

	rc := &ff.Command{
		Name:     name,
		Usage:    "tink-agent [flags]",
		LongHelp: "Tink Agent runs the workflows.",
		Flags:    RegisterAllFlags(c),
	}

	if err := rc.Parse(os.Args[1:], ff.WithEnvVarPrefix("AGENT")); err != nil {
		e := errors.New(ffhelp.Command(rc).String())
		if !errors.Is(err, ff.ErrHelp) {
			e = fmt.Errorf("%w\n%s", e, err)
		}

		fmt.Fprintf(os.Stderr, "%v\n", e)

		exitCode = 1
		return
	}

	// For legacy flags, we need to check the environment variables without the prefix.
	SetFromEnvLegacy(c)

	// TODO(jacobweinstock): do input validation. required fields, etc.
	// ID is required
	// tink server address is required, maybe, depending on the transport

	log := defaultLogger(c.LogLevel).WithValues("agentID", c.AgentID)
	log.Info("starting Agent", "runtime", c.Options.RuntimeSelected, "transport", c.Options.TransportSelected, "version", build.GitRevision())
	log.V(4).Info("agent configuration", "config", c)

	// Install the W3C TraceContext propagator and lift any TRACEPARENT env
	// var (or tinkerbell_traceparent= kernel cmdline arg, as injected by
	// smee's iPXE template in Phase 3) into the context so downstream agent
	// code can re-parent spans into the workflow trace.
	otel.EnsureW3CPropagator()
	ctx = otel.ContextWithEnvTraceparent(ctx)
	ctx = otel.ContextWithCmdlineTraceparent(ctx)

	// Stand up the agent's OTel tracer + log providers pointed at the
	// tink-server gRPC endpoint. tink-server hosts an OTLP relay that
	// forwards what we send here to the operator-configured external
	// collector, so the agent never needs to know the collector address.
	// Skipped when the gRPC transport is not in use (file/NATS) -- those
	// modes have no relay endpoint to target.
	if c.Options.TransportSelected == agent.GRPCTransportType && c.Options.Transport.GRPC.ServerAddrPort != "" {
		otelCfg := otel.Config{
			Servicename: name,
			Endpoint:    c.Options.Transport.GRPC.ServerAddrPort,
			Insecure:    c.Options.Transport.GRPC.TLSInsecure || !c.Options.Transport.GRPC.TLSEnabled,
			InstanceID:  c.AgentID,
			Logger:      log,
		}
		newCtx, otelShutdown, err := otel.Init(ctx, otelCfg)
		if err != nil {
			log.Error(err, "failed to init OpenTelemetry; continuing without traces")
		} else {
			ctx = newCtx
			defer otelShutdown()
		}
		if _, logsShutdown, err := otel.InitLogs(ctx, otelCfg); err != nil {
			log.Error(err, "failed to init OpenTelemetry logs; continuing without log export")
		} else {
			defer func() { _ = logsShutdown() }()
		}
	}

	if err := c.Options.ConfigureAndRun(ctx, log, c.AgentID); err != nil {
		log.Error(err, "failed to configure and run agent")
		exitCode = 1
		return
	}
	log.Info("stopped Agent")
}

// defaultLogger uses the slog logr implementation.
func defaultLogger(level int) logr.Logger {
	// source file and function can be long. This makes the logs less readable.
	// for improved readability, truncate source file to last 3 parts and remove the function entirely.
	customAttr := func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.SourceKey {
			ss, ok := a.Value.Any().(*slog.Source)
			if !ok || ss == nil {
				return a
			}
			ss.Function = ""
			p := strings.Split(ss.File, "/")
			// log the file path from tinkerbell/tinkerbell to the end.
			var idx int
			for i, v := range p {
				if v == "tinkerbell" {
					idx = i
					break
				}
			}
			ss.File = filepath.Join(p[idx:]...)

			return a
		}

		// This changes the slog.Level string representation to an integer.
		// This makes it so that the V-levels passed in to the CLI show up as is in the logs.
		if a.Key == slog.LevelKey {
			b, ok := a.Value.Any().(slog.Level)
			if !ok {
				return a
			}
			a.Value = slog.Float64Value(math.Abs(float64(b)))
			return a
		}

		return a
	}
	opts := &slog.HandlerOptions{
		AddSource:   true,
		Level:       slog.Level(-level),
		ReplaceAttr: customAttr,
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, opts))

	return logr.FromSlogHandler(log.Handler())
}
