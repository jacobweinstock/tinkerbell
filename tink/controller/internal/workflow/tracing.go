package workflow

import (
	"context"

	v1alpha1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	tinkotel "github.com/tinkerbell/tinkerbell/pkg/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the OTel instrumentation scope name for all workflow
// controller spans. Kept stable so downstream filters/dashboards work.
const tracerName = "github.com/tinkerbell/tinkerbell/tink/controller/workflow"

// Phase labels persisted in WorkflowStatus.PhaseTraceparents. Reused by
// downstream components (smee, tootles, rufio) so they can find the right
// phase traceparent to stitch into.
const (
	phasePreBMC  = "preBMC"
	phaseBoot    = "boot"
	phaseActions = "actions"
	phasePostBMC = "postBMC"
)

func tracer() trace.Tracer { return otel.Tracer(tracerName) }

// ensureRootTraceparent returns the workflow's root traceparent, creating it
// (and the workflow.lifecycle root span) on the first reconcile where one is
// not yet present.
//
// A traceparent already present on the workflow's annotations (set by an
// upstream system such as CAPT) is honored verbatim. Otherwise the controller
// owns the root and starts workflow.lifecycle ended immediately; subsequent
// reconciles re-parent into it via ContextWithRemoteTraceparent.
func ensureRootTraceparent(ctx context.Context, wflow *v1alpha1.Workflow) string {
	if wflow.Status.Traceparent != "" {
		return wflow.Status.Traceparent
	}
	if tp := wflow.Annotations[v1alpha1.AnnotationTraceparent]; tp != "" {
		wflow.Status.Traceparent = tp
		return tp
	}
	_, span, tp := tinkotel.StartPhaseSpan(ctx, tracer(), "workflow.lifecycle", "")
	span.SetAttributes(
		attribute.String("workflow.name", wflow.Name),
		attribute.String("workflow.namespace", wflow.Namespace),
		attribute.String("workflow.uid", string(wflow.UID)),
		attribute.String("workflow.hardwareRef", wflow.Spec.HardwareRef),
		attribute.String("workflow.templateRef", wflow.Spec.TemplateRef),
	)
	span.End()
	wflow.Status.Traceparent = tp
	return tp
}

// ensurePhaseTraceparent returns the traceparent for the named phase,
// creating it on first call. The new phase span is parented to the workflow's
// root traceparent and linked to all previously-completed phases for
// readability.
//
// Like the root span, the phase span is started and immediately ended; later
// reconciles re-parent into it via ContextWithRemoteTraceparent so every
// phase-scoped child span lands under one parent regardless of which reconcile
// pass created it.
func ensurePhaseTraceparent(ctx context.Context, wflow *v1alpha1.Workflow, phase string) string {
	if tp, ok := wflow.Status.PhaseTraceparents[phase]; ok && tp != "" {
		return tp
	}
	rootTP := ensureRootTraceparent(ctx, wflow)

	var links []trace.Link
	for _, prev := range []string{phasePreBMC, phaseBoot, phaseActions, phasePostBMC} {
		if prev == phase {
			break
		}
		if l, ok := tinkotel.LinkFromTraceparent(wflow.Status.PhaseTraceparents[prev]); ok {
			links = append(links, l)
		}
	}

	_, span, tp := tinkotel.StartPhaseSpan(ctx, tracer(), "workflow."+phase, rootTP, links...)
	span.SetAttributes(
		attribute.String("workflow.name", wflow.Name),
		attribute.String("workflow.namespace", wflow.Namespace),
		attribute.String("workflow.phase", phase),
	)
	span.End()
	if wflow.Status.PhaseTraceparents == nil {
		wflow.Status.PhaseTraceparents = map[string]string{}
	}
	wflow.Status.PhaseTraceparents[phase] = tp
	return tp
}
