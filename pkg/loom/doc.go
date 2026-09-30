// Package loom renders Go text/template expressions embedded in the string
// values of an already-decoded document, resolving them against the document
// itself (self-reference) and against caller-supplied data.
//
// It is derived from https://github.com/jacobweinstock/loom, without YAML
// input/output and type inference: callers here decode and re-type documents
// themselves, so every rendered value is a string.
//
// Only string values containing "{{" are rendered, so a template can change a
// value but never the document's shape. A literal "{{" is written as
// {{ "{{" }}.
//
// # Self-reference and evaluation order
//
// The document is exposed to its own templates under the self key (default
// "self"; see WithSelfKey), so a field can reference a sibling:
//
//	name: example
//	greeting: "hello {{ .self.name }}"
//
// Fields may reference other templated fields. loom builds a dependency graph
// from the self-references and evaluates fields in dependency order, so each
// field observes the rendered value of everything it depends on. Each field is
// evaluated exactly once. If the references form a cycle, RenderValue returns an
// error that is ErrReferenceCycle.
//
// Caller data is merged into the same root, so templates may also reference
// external values (for example {{ .references.net.spec.domain }}). Data values
// are never themselves templated.
//
// # Resource bounds
//
// Each field is rendered under an output-size cap (WithMaxOutputBytes, default
// 1 MiB) and a time budget (WithRenderTimeout, default 2s). A breach is reported
// as a *FieldError that is ErrOutputTooLarge or ErrRenderTimeout. The budget is
// enforced as output is produced, so a function that spins without writing is
// interrupted only on a best-effort basis.
package loom
