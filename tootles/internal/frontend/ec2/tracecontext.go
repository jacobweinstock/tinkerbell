package ec2

import "context"

// endpointCtxKey is an unexported context key used to thread the matched route
// endpoint (e.g. "/meta-data/instance-id") from the HTTP handler down into the
// backend so it can be attached as a span attribute. Using context avoids
// expanding the Client interface for a tracing concern.
type endpointCtxKey struct{}

// WithEndpoint returns a copy of ctx carrying the given endpoint string.
func WithEndpoint(ctx context.Context, endpoint string) context.Context {
	return context.WithValue(ctx, endpointCtxKey{}, endpoint)
}

// EndpointFromContext returns the endpoint stored in ctx by WithEndpoint, or
// "" if none is set.
func EndpointFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(endpointCtxKey{}).(string); ok {
		return v
	}
	return ""
}
