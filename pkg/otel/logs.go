package otel

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otlploggrpc "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc/credentials"
)

// InitLogs wires up an OTLP/gRPC logs exporter behind a batch processor and
// installs the resulting LoggerProvider as the OTel global LoggerProvider.
// It is a no-op (returns nil provider, nil shutdown, nil err) when c.Endpoint
// is empty.
//
// Endpoint and Insecure mirror the tracing exporter config in Init; the same
// collector endpoint is reused for the logs signal.
//
// The returned shutdown func should be deferred by the caller and will flush
// in-flight log records before returning.
func InitLogs(ctx context.Context, c Config) (*log.LoggerProvider, func() error, error) {
	if c.Endpoint == "" {
		return nil, func() error { return nil }, nil
	}

	attrs := []attribute.KeyValue{semconv.ServiceNameKey.String(c.Servicename)}
	if c.InstanceID != "" {
		attrs = append(attrs, attribute.String("service.instance.id", c.InstanceID))
	}
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OpenTelemetry logs resource: %w", err)
	}

	grpcOpts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(c.Endpoint)}
	if c.Insecure {
		grpcOpts = append(grpcOpts, otlploggrpc.WithInsecure())
	} else {
		grpcOpts = append(grpcOpts, otlploggrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	exporter, err := otlploggrpc.New(ctx, grpcOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to configure OTLP logs exporter: %w", err)
	}

	processor := log.NewBatchProcessor(exporter)
	provider := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(processor),
	)
	otellog.SetLoggerProvider(provider)

	shutdown := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := provider.Shutdown(ctx); err != nil {
			otelglobal.GetErrorHandler().Handle(fmt.Errorf("shutdown of OpenTelemetry LoggerProvider failed: %w", err))
			return err
		}
		return nil
	}
	return provider, shutdown, nil
}

// NewSlogHandler returns a slog.Handler that emits records via the global OTel
// LoggerProvider (installed by InitLogs). The instrumentation scope name is
// used to identify the producing component in the collector.
//
// Safe to call before InitLogs: when no provider is installed the handler
// emits to a no-op logger and records are silently dropped.
func NewSlogHandler(name string) slog.Handler {
	return otelslog.NewHandler(name)
}
