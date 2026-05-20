// Helpers for stitching Tinkerbell distributed traces. These complement the
// init/exporter plumbing in otel.go (which was adapted from
// equinix-labs/otel-init-go and intentionally left close to that layout).
package otel

import (
	"context"
	"os"
	"strings"

	tinkv1a1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// W3CPropagator returns a composite propagator (TraceContext + Baggage)
// suitable for SetTextMapPropagator. It is the same propagator initTracing
// installs, but exposed so processes that don't run a full tracer provider
// (e.g. the tink agent before Phase 4) can still extract/inject traceparent
// headers via the standard W3C TraceContext format.
func W3CPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// EnsureW3CPropagator installs the W3C composite propagator as the OTel global
// text-map propagator if (and only if) the current global propagator is the
// no-op default. Safe to call from any process; idempotent because Init also
// installs the same propagator.
//
// This is what makes ContextWithEnvTraceparent / ContextWithTraceparentString
// actually do something in processes that have not (yet) called Init.
func EnsureW3CPropagator() {
	// The OTel global default is a no-op propagator that reports zero Fields().
	if len(otel.GetTextMapPropagator().Fields()) == 0 {
		otel.SetTextMapPropagator(W3CPropagator())
	}
}

// SpanContextFromTraceparent parses a W3C "traceparent" string and returns the
// corresponding trace.SpanContext. The second return value is true when the
// parsed SpanContext IsValid().
//
// This is the inverse of TraceparentStringFromContext and does NOT depend on
// the global propagator being initialized.
func SpanContextFromTraceparent(traceparent string) (trace.SpanContext, bool) {
	if traceparent == "" {
		return trace.SpanContext{}, false
	}
	carrier := SimpleCarrier{"traceparent": traceparent}
	ctx := W3CPropagator().Extract(context.Background(), carrier)
	sc := trace.SpanContextFromContext(ctx)
	return sc, sc.IsValid()
}

// ContextWithRemoteTraceparent returns a context whose SpanContext is the one
// encoded in the supplied W3C "traceparent" string, marked as a *remote*
// parent. Subsequent tracer.Start(ctx, ...) calls will create child spans of
// that remote parent.
//
// When the traceparent is empty or invalid the original context is returned
// unchanged.
//
// This is the canonical entry point for any reconciler/handler that has read
// a stored traceparent off a CR (annotation or status field) and wants the
// rest of its work to land under the originating distributed trace.
func ContextWithRemoteTraceparent(ctx context.Context, traceparent string) context.Context {
	sc, ok := SpanContextFromTraceparent(traceparent)
	if !ok {
		return ctx
	}
	return trace.ContextWithRemoteSpanContext(ctx, sc)
}

// StartPhaseSpan starts a new "phase root" span parented to the SpanContext
// encoded in parentTraceparent, optionally with span links to previously
// completed phases. It returns the child context, the started span, and the
// traceparent string of the new span (suitable for persisting to a CR status
// field so subsequent reconciles can re-parent into the same phase).
//
// The returned span is NOT ended; the caller is responsible for calling
// span.End() (typically immediately after persisting the returned traceparent,
// mirroring the workflow.lifecycle root-span pattern).
//
// When parentTraceparent is empty the span starts as a root in a new trace.
func StartPhaseSpan(
	ctx context.Context,
	tracer trace.Tracer,
	name, parentTraceparent string,
	links ...trace.Link,
) (context.Context, trace.Span, string) {
	if parentTraceparent != "" {
		ctx = ContextWithRemoteTraceparent(ctx, parentTraceparent)
	}
	opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindInternal)}
	if len(links) > 0 {
		opts = append(opts, trace.WithLinks(links...))
	}
	ctx, span := tracer.Start(ctx, name, opts...)
	return ctx, span, TraceparentStringFromContext(ctx)
}

// LinkFromTraceparent builds a trace.Link from a stored W3C traceparent
// string. Returns the zero Link and false when the traceparent is empty or
// invalid. Use this to attach inter-phase links to a StartPhaseSpan call.
func LinkFromTraceparent(traceparent string) (trace.Link, bool) {
	sc, ok := SpanContextFromTraceparent(traceparent)
	if !ok {
		return trace.Link{}, false
	}
	return trace.Link{SpanContext: sc}, true
}

// InjectTraceparentIntoAnnotations writes the current span's traceparent into
// the supplied annotations map under tinkv1a1.AnnotationTraceparent. The
// annotations map must be non-nil. No-ops when there is no active span context
// on ctx.
func InjectTraceparentIntoAnnotations(ctx context.Context, annotations map[string]string) {
	if annotations == nil {
		return
	}
	tp := TraceparentStringFromContext(ctx)
	if tp == "" {
		return
	}
	annotations[tinkv1a1.AnnotationTraceparent] = tp
}

// ExtractTraceparentFromAnnotations reads the traceparent annotation from the
// supplied map and returns a context whose remote parent SpanContext is set
// accordingly. The bool reports whether stitching actually occurred (annotation
// present, non-empty, and valid).
func ExtractTraceparentFromAnnotations(ctx context.Context, annotations map[string]string) (context.Context, bool) {
	if len(annotations) == 0 {
		return ctx, false
	}
	tp := annotations[tinkv1a1.AnnotationTraceparent]
	if tp == "" {
		return ctx, false
	}
	newCtx := ContextWithRemoteTraceparent(ctx, tp)
	return newCtx, newCtx != ctx
}

// CmdlineTraceparentKey is the kernel-cmdline argument smee's iPXE template
// uses to forward the workflow traceparent into the booted OS. Exported so
// the agent (or any in-OS consumer) can find it consistently.
const CmdlineTraceparentKey = "tinkerbell_traceparent"

// ContextWithCmdlineTraceparent looks for tinkerbell_traceparent=... in
// /proc/cmdline (the carrier used by smee's iPXE script template) and, when
// present, returns a context whose remote parent SpanContext is that
// traceparent. When the file cannot be read, the key is absent, or the value
// is invalid, the original context is returned unchanged.
//
// This is the in-OS counterpart to ContextWithEnvTraceparent: it is what lets
// tink-agent stitch its spans into the workflow trace on first boot when no
// TRACEPARENT env var has been set by an outer process.
func ContextWithCmdlineTraceparent(ctx context.Context) context.Context {
	if tp := traceparentFromCmdline("/proc/cmdline"); tp != "" {
		return ContextWithRemoteTraceparent(ctx, tp)
	}
	return ctx
}

// traceparentFromCmdline parses the named cmdline-style file and returns the
// value of CmdlineTraceparentKey, or "" if not found / unreadable. Extracted
// for testability.
func traceparentFromCmdline(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // path is provided by caller (default /proc/cmdline)
	if err != nil {
		return ""
	}
	prefix := CmdlineTraceparentKey + "="
	for _, tok := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(tok, prefix); ok {
			return v
		}
	}
	return ""
}
