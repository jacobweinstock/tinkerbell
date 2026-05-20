package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/pkg/otel"
)

// getLogger returns a logger based on the configuration.
// If level is negative, returns a logger that discards all output.
func getLogger(level int) logr.Logger {
	if level < 0 {
		return logr.Discard()
	}
	return defaultLogger(level)
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

			p := strings.Split(ss.File, "/")
			// log the file path from tinkerbell/tinkerbell to the end.
			var idx int

			for i, v := range p {
				if v == "tinkerbell" {
					if i+2 < len(p) {
						idx = i + 2
						break
					}
				}
				// This trims the source file for 3rd party packages to include
				// just enough information to identify the package. Without this,
				// the source file can be long and make the log line more cluttered
				// and hard to read.
				if v == "mod" {
					if i+1 < len(p) {
						idx = i + 1
						break
					}
				}
			}
			ss.File = filepath.Join(p[idx:]...)
			ss.File = fmt.Sprintf("%s:%d", ss.File, ss.Line)
			a.Value = slog.StringValue(ss.File)
			a.Key = "caller"

			return a
		}

		// This changes the slog.Level string representation to an integer.
		// This makes it so that the V-levels passed in to the CLI show up as is in the logs.
		if a.Key == slog.LevelKey {
			b, ok := a.Value.Any().(slog.Level)
			if !ok {
				return a
			}
			a.Value = slog.StringValue(strconv.Itoa(int(b)))
			return a
		}

		return a
	}
	opts := &slog.HandlerOptions{
		AddSource:   true,
		Level:       slog.Level(-level),
		ReplaceAttr: customAttr,
	}
	log := slog.New(teeHandler{
		primary: slog.NewJSONHandler(os.Stdout, opts),
		// otelslog is a no-op until pkg/otel.InitLogs installs a global
		// LoggerProvider; safe to wire unconditionally.
		secondary: otel.NewSlogHandler("tinkerbell"),
	})

	return logr.FromSlogHandler(log.Handler())
}

// teeHandler fans out every record to two slog.Handlers (stdout JSON + OTel
// log bridge). Both handlers see Handle/WithAttrs/WithGroup; Enabled returns
// true if either side wants the record so OTel-only consumers can pick up
// records the primary would have filtered.
type teeHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (h teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.primary.Enabled(ctx, l) || h.secondary.Enabled(ctx, l)
}

func (h teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var primaryErr, secondaryErr error
	if h.primary.Enabled(ctx, r.Level) {
		primaryErr = h.primary.Handle(ctx, r.Clone())
	}
	if h.secondary.Enabled(ctx, r.Level) {
		secondaryErr = h.secondary.Handle(ctx, r)
	}
	if primaryErr != nil {
		return primaryErr
	}
	return secondaryErr
}

func (h teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return teeHandler{primary: h.primary.WithAttrs(attrs), secondary: h.secondary.WithAttrs(attrs)}
}

func (h teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{primary: h.primary.WithGroup(name), secondary: h.secondary.WithGroup(name)}
}
