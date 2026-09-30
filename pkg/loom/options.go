package loom

import (
	"text/template"
	"time"
)

const leftDelim = "{{"

// config is the resolved set of rendering options.
type config struct {
	funcs          template.FuncMap
	missingKeyErr  bool
	selfKey        string
	skip           func(path string) bool
	maxOutputBytes int
	timeout        time.Duration
}

func defaults() config {
	return config{
		missingKeyErr:  true,
		selfKey:        "self",
		maxOutputBytes: 1 << 20, // 1 MiB per field
		timeout:        2 * time.Second,
	}
}

// Option configures a RenderValue call.
type Option func(*config)

// WithFuncs registers template helper functions available to every templated
// field.
func WithFuncs(funcs template.FuncMap) Option {
	return func(c *config) { c.funcs = funcs }
}

// WithMissingKeyError controls whether referencing a missing map key is an
// error (true, the default) or renders as "<no value>" (false).
func WithMissingKeyError(b bool) Option {
	return func(c *config) { c.missingKeyErr = b }
}

// WithSelfKey sets the root key under which the document is exposed to its own
// templates. The default is "self".
func WithSelfKey(key string) Option {
	return func(c *config) {
		if key != "" {
			c.selfKey = key
		}
	}
}

// WithSkip excludes string values whose path skip reports true for from
// rendering. Skipped values stay readable through the self key, unrendered.
// Paths look like "spec.interfaces[0].dhcp.mac".
func WithSkip(skip func(path string) bool) Option {
	return func(c *config) { c.skip = skip }
}

// WithMaxOutputBytes caps the rendered size of any single field. Exceeding it
// fails with an error that is ErrOutputTooLarge. The default is 1 MiB; 0
// disables the cap.
func WithMaxOutputBytes(n int) Option {
	return func(c *config) { c.maxOutputBytes = max(n, 0) }
}

// WithRenderTimeout bounds the time spent rendering any single field. Exceeding
// it fails with an error that is ErrRenderTimeout. The default is 2s; 0
// disables the timeout.
func WithRenderTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = max(d, 0) }
}
