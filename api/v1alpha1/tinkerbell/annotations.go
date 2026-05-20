package tinkerbell

// AnnotationTraceparent is the metadata.annotations key used across all Tinkerbell
// CRs (Hardware, Job, Task, etc.) to carry a W3C TraceContext "traceparent" value
// for stitching distributed traces together.
//
// The Workflow CR does NOT use this annotation; it instead persists its root and
// per-phase traceparents in Status.Traceparent / Status.PhaseTraceparents so they
// survive reconciles authoritatively.
//
// Consumers should read this annotation off any object they reconcile, lift it
// into the request context via pkg/otel.ContextWithRemoteTraceparent, and start
// spans parented to it. Producers (the tink workflow controller) set this
// annotation alongside their existing writes when creating or updating those
// objects.
const AnnotationTraceparent = "tinkerbell.org/traceparent"
