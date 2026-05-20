package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// OTLPRelay is the tink-server-side OTLP receiver. It accepts trace and log
// payloads from agents (which only have one outbound network route -- back to
// tink-server) and forwards them verbatim to an operator-configured upstream
// OTLP collector.
//
// Forwarding the raw protobuf preserves span IDs, parent links, timestamps,
// and resource attributes exactly as the agent recorded them, which is what
// keeps agent spans stitched into the workflow trace at full fidelity.
type OTLPRelay struct {
	log         logr.Logger
	traceClient coltracepb.TraceServiceClient
	logsClient  collogspb.LogsServiceClient
	upstream    *grpc.ClientConn
}

// traceServer adapts the relay to coltracepb.TraceServiceServer.
type traceServer struct {
	coltracepb.UnimplementedTraceServiceServer
	r *OTLPRelay
}

// logsServer adapts the relay to collogspb.LogsServiceServer. A separate
// type is required because both server interfaces declare a method named
// Export and a single Go type cannot satisfy both at once.
type logsServer struct {
	collogspb.UnimplementedLogsServiceServer
	r *OTLPRelay
}

// NewOTLPRelay dials the upstream OTLP collector and returns a relay ready to
// be registered on a gRPC server. When endpoint is empty the relay is
// disabled (returns nil, nil) and the caller should skip registration.
func NewOTLPRelay(_ context.Context, log logr.Logger, endpoint string, insecureTransport bool) (*OTLPRelay, error) {
	if endpoint == "" {
		return nil, nil
	}
	var creds credentials.TransportCredentials
	if insecureTransport {
		creds = insecure.NewCredentials()
	} else {
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("otlp relay: dial upstream %q: %w", endpoint, err)
	}
	return &OTLPRelay{
		log:         log.WithName("otlp-relay").WithValues("upstream", endpoint),
		traceClient: coltracepb.NewTraceServiceClient(conn),
		logsClient:  collogspb.NewLogsServiceClient(conn),
		upstream:    conn,
	}, nil
}

// Close releases the upstream gRPC connection.
func (r *OTLPRelay) Close() error {
	if r == nil || r.upstream == nil {
		return nil
	}
	return r.upstream.Close()
}

// Register wires the relay onto the supplied gRPC server.
func (r *OTLPRelay) Register(gs *grpc.Server) {
	if r == nil {
		return
	}
	coltracepb.RegisterTraceServiceServer(gs, &traceServer{r: r})
	collogspb.RegisterLogsServiceServer(gs, &logsServer{r: r})
}

func (t *traceServer) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	if t.r == nil || t.r.traceClient == nil {
		return nil, errors.New("otlp relay: traces not configured")
	}
	resp, err := t.r.traceClient.Export(ctx, req)
	if err != nil {
		t.r.log.V(1).Info("forward traces failed", "error", err)
		return nil, err
	}
	return resp, nil
}

func (l *logsServer) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	if l.r == nil || l.r.logsClient == nil {
		return nil, errors.New("otlp relay: logs not configured")
	}
	resp, err := l.r.logsClient.Export(ctx, req)
	if err != nil {
		l.r.log.V(1).Info("forward logs failed", "error", err)
		return nil, err
	}
	return resp, nil
}
