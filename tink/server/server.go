package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/go-logr/logr"
	grpcprometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tinkerbell/tinkerbell/pkg/listener"
	"github.com/tinkerbell/tinkerbell/pkg/proto"
	grpcinternal "github.com/tinkerbell/tinkerbell/tink/server/internal/grpc"
	"github.com/tinkerbell/tinkerbell/tink/server/tunnel"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

// Registry is the Prometheus registry for all Tink server gRPC metrics.
// It is separate from the default registry so that gRPC metrics can be
// served on a dedicated /tink-server/metrics endpoint.
var Registry = prometheus.NewRegistry()

// grpcServerMetrics is an isolated set of gRPC server metrics registered
// on [Registry] instead of the global default.
var grpcServerMetrics = grpcprometheus.NewServerMetrics()

func init() {
	Registry.MustRegister(grpcServerMetrics, grpcinternal.AuthenticationFailures, tunnel.AuthorizationDenials)
}

type Config struct {
	Backend grpcinternal.Backend
	// V4 and V6 are the per-family listen addresses.
	V4     listener.Bind
	V6     listener.Bind
	Logger logr.Logger
	Auto   AutoCapabilities
	TLS    TLS
	// Tunnel, when set, serves the Agent services inside WireGuard tunnels and
	// enforces its mode on the plain listener.
	Tunnel *tunnel.Manager
}

type AutoCapabilities struct {
	Enrollment Enrollment
	Discovery  Discovery
}

type Enrollment struct {
	Enabled               bool
	WorkflowRuleSetLister grpcinternal.WorkflowRuleSetLister
	WorkflowCreator       grpcinternal.WorkflowCreator
}

type Discovery struct {
	Enabled           bool
	Namespace         string
	EnrollmentEnabled bool
	HardwareCreator   grpcinternal.HardwareCreator
	HardwareFilterer  grpcinternal.HardwareFilterer
}

type TLS struct {
	Cert credentials.TransportCredentials
}

// Option is a functional option type.
type Option func(*Config)

// WithAutoDiscoveryNamespace sets the namespace for auto discovery.
func WithAutoDiscoveryNamespace(ns string) Option {
	return func(c *Config) {
		c.Auto.Discovery.Namespace = ns
	}
}

// WithAutoDiscoveryAutoEnrollmentEnabled sets the value for hardware.spec.auto.enrollmentEnabled when auto discovery creates Hardware objects.
func WithAutoDiscoveryAutoEnrollmentEnabled(enabled bool) Option {
	return func(c *Config) {
		c.Auto.Discovery.EnrollmentEnabled = enabled
	}
}

// WithBackend sets the backend for the server.
func WithBackend(b grpcinternal.Backend) Option {
	return func(c *Config) {
		c.Backend = b
	}
}

// WithBindAddrPort sets the IPv4 bind address and port for the server.
func WithBindAddrPort(addrPort netip.AddrPort) Option {
	return func(c *Config) {
		c.V4 = listener.Bind{Addr: addrPort.Addr(), Port: addrPort.Port(), Enabled: true}
	}
}

// WithLogger sets the logger for the server.
func WithLogger(l logr.Logger) Option {
	return func(c *Config) {
		c.Logger = l
	}
}

// WithTLSCert sets the TLS key file for the server.
func WithTLSCert(cert credentials.TransportCredentials) Option {
	return func(c *Config) {
		c.TLS.Cert = cert
	}
}

func NewConfig(opts ...Option) *Config {
	c := &Config{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Config) Start(ctx context.Context, log logr.Logger) error {
	s := &grpcinternal.Handler{
		Backend: c.Backend,
		Logger:  log,
		NowFunc: time.Now,
		AutoCapabilities: grpcinternal.AutoCapabilities{
			Enrollment: grpcinternal.AutoEnrollment{
				Enabled:               c.Auto.Enrollment.Enabled,
				WorkflowRuleSetLister: c.Auto.Enrollment.WorkflowRuleSetLister,
				WorkflowCreator:       c.Auto.Enrollment.WorkflowCreator,
			},
			Discovery: grpcinternal.AutoDiscovery{
				Enabled:           c.Auto.Discovery.Enabled,
				Namespace:         c.Auto.Discovery.Namespace,
				EnrollmentEnabled: c.Auto.Discovery.EnrollmentEnabled,
				HardwareCreator:   c.Auto.Discovery.HardwareCreator,
				HardwareFilterer:  c.Auto.Discovery.HardwareFilterer,
			},
		},
	}

	if c.Tunnel != nil {
		s.Tunnel = c.Tunnel
	}

	type served struct {
		gs  *grpc.Server
		lis net.Listener
	}
	var servers []*grpc.Server
	var all []served
	closeAll := func() {
		for _, sv := range all {
			_ = sv.lis.Close()
		}
	}

	serveTunnel := c.Tunnel != nil
	servePlain := c.Tunnel == nil || c.Tunnel.Mode() != tunnel.ModeRequired

	if servePlain {
		params := []grpc.ServerOption{
			grpc.StatsHandler(otelgrpc.NewServerHandler()),
		}
		unary := []grpc.UnaryServerInterceptor{grpcServerMetrics.UnaryServerInterceptor()}
		if c.Tunnel != nil {
			unary = append(unary, c.Tunnel.UnaryInterceptor(s.PlainGuard))
		}
		params = append(params, grpc.ChainUnaryInterceptor(unary...), grpc.StreamInterceptor(grpcServerMetrics.StreamServerInterceptor()))
		if c.TLS.Cert != nil {
			params = append(params, grpc.Creds(c.TLS.Cert))
		}
		gs := grpc.NewServer(params...)
		proto.RegisterWorkflowServiceServer(gs, s)
		reflection.Register(gs)
		grpcServerMetrics.InitializeMetrics(gs)
		servers = append(servers, gs)

		for _, b := range []listener.Bind{c.V4, c.V6} {
			if !b.Enabled {
				continue
			}
			if b.Port == 0 {
				closeAll()
				return fmt.Errorf("tink server %s is enabled but has no bind port", b.Addr)
			}
			lis, err := listener.TCP(ctx, b.Addr, int(b.Port))
			if err != nil {
				closeAll()
				return fmt.Errorf("failed to listen: %w", err)
			}
			all = append(all, served{gs: gs, lis: lis})
		}
		if len(all) == 0 && !serveTunnel {
			return errors.New("tink server has no enabled IPv4 or IPv6 listener")
		}
	} else {
		log.Info("WireGuard is required, not serving the plain gRPC listener")
	}

	if serveTunnel {
		// No TLS inside the tunnels: WireGuard already authenticates and encrypts.
		gs := grpc.NewServer(
			grpc.StatsHandler(otelgrpc.NewServerHandler()),
			grpc.ChainUnaryInterceptor(grpcServerMetrics.UnaryServerInterceptor(), c.Tunnel.UnaryInterceptor(nil)),
			grpc.ChainStreamInterceptor(grpcServerMetrics.StreamServerInterceptor(), c.Tunnel.StreamInterceptor()),
		)
		proto.RegisterWorkflowServiceServer(gs, s)
		proto.RegisterAgentServiceServer(gs, grpcinternal.NewAgentService(s))
		grpcServerMetrics.InitializeMetrics(gs)
		servers = append(servers, gs)
		for _, lis := range c.Tunnel.Listeners() {
			all = append(all, served{gs: gs, lis: lis})
		}
	}

	// Bounded by done so a Serve failure cannot leave this parked until the
	// caller's context happens to end.
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		time.Sleep(1 * time.Second)
		log.Info("Initiating graceful shutdown")
		var wg sync.WaitGroup
		for _, gs := range servers {
			wg.Go(func() {
				timer := time.AfterFunc(5*time.Second, func() {
					log.Info("Server couldn't stop gracefully in time, doing force stop")
					gs.Stop()
				})
				defer timer.Stop()
				gs.GracefulStop() // gracefully stop server after in-flight server streaming rpc finishes
			})
		}
		wg.Wait()
		log.Info("Server stopped")
	}()

	// A graceful shutdown closes every listener, so each Serve returns nil and
	// g.Wait reports success. A Serve that fails on its own stops every server,
	// otherwise the others keep serving and g.Wait never returns.
	var g errgroup.Group
	for _, sv := range all {
		if t, ok := sv.lis.(interface{ Tunnel() string }); ok {
			log.Info("starting gRPC server in WireGuard tunnel", "tunnel", t.Tunnel(), "address", sv.lis.Addr().String())
		} else {
			log.Info("starting gRPC server", "bindAddr", sv.lis.Addr().String())
		}
		g.Go(func() error {
			if err := sv.gs.Serve(sv.lis); err != nil {
				for _, gs := range servers {
					gs.Stop()
				}
				return err
			}
			return nil
		})
	}
	err := g.Wait()
	close(done)
	// The tunnels carry the gRPC servers' connections, so they close only once a
	// graceful stop has let in-flight requests finish.
	<-stopped
	if c.Tunnel != nil {
		c.Tunnel.Close()
	}
	if err != nil {
		log.Error(err, "failed to serve")
		return err
	}

	return nil
}

type allInterfaces interface {
	grpcinternal.Backend
	grpcinternal.HardwareCreator
	grpcinternal.HardwareFilterer
	grpcinternal.WorkflowRuleSetLister
	grpcinternal.WorkflowCreator
}

// SetBackends is a helper function to set a single backend implementation for all backend interfaces.
// This is useful for backends that implement multiple interfaces, such as the kube backend.
func (c *Config) SetBackends(b allInterfaces) {
	c.Backend = b
	c.Auto.Discovery.HardwareCreator = b
	c.Auto.Discovery.HardwareFilterer = b
	c.Auto.Enrollment.WorkflowRuleSetLister = b
	c.Auto.Enrollment.WorkflowCreator = b
}
