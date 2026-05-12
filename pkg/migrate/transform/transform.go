// Package transform contains pure functions that translate Tinkerbell
// v1alpha1 API objects into their v1alpha2 equivalents.
//
// The functions in this package have no I/O dependencies and never call
// into the Kubernetes API. They are designed to be driven by a streaming
// runner that reads v1alpha1 objects from disk, calls the appropriate
// transform, and writes the result back to disk.
//
// Handling categories mirror the migration plan documented in
// docs/technical/V1ALPHA1_TO_V1ALPHA2_MIGRATION.md:
//
//   - apply:   Hardware, Template (1:N split to Task), WorkflowRuleSet
//              (renamed to Policy), bmc.Machine (renamed to BMC).
//   - archive: Workflow (spec transformed; status dropped). bmc.Job is
//              copied verbatim by the runner and has no transform here.
//   - drop:    bmc.Task — discarded by the runner.
package transform

import (
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
)

// TemplateRefs maps a v1alpha1 Template name to the list of v1alpha2
// Task SimpleReferences that were produced when the Template was split.
// It is built by the runner during the Template transform phase and
// passed to the Workflow transform phase so Workflow.Spec.Tasks can be
// populated with the correct TaskRefs.
type TemplateRefs map[string][]v2.SimpleReference
