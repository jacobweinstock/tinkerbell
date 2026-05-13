// Package transform contains pure functions that translate Tinkerbell
// v1alpha1 API objects into their v1alpha2 equivalents.
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
