package loom

import (
	"bytes"
	"text/template"
	"time"
)

// buildTemplate parses tmpl with the configured functions and missing-key
// behavior.
func buildTemplate(tmpl string, cfg *config) (*template.Template, error) {
	t := template.New("loom")
	if cfg.funcs != nil {
		t = t.Funcs(cfg.funcs)
	}
	if cfg.missingKeyErr {
		t = t.Option("missingkey=error")
	}
	return t.Parse(tmpl)
}

// limitWriter bounds template execution. It caps the number of bytes written
// (max, 0 disables) and fails once deadline passes (zero disables).
// text/template stops as soon as a Write returns an error, so this reliably
// interrupts a template that emits or expands text.
type limitWriter struct {
	buf      bytes.Buffer
	max      int
	deadline time.Time
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if !w.deadline.IsZero() && time.Now().After(w.deadline) {
		return 0, ErrRenderTimeout
	}
	if w.max > 0 && w.buf.Len()+len(p) > w.max {
		n, _ := w.buf.Write(p[:w.max-w.buf.Len()])
		return n, ErrOutputTooLarge
	}
	return w.buf.Write(p)
}

// execTemplate executes t against root under the configured size cap and time
// budget.
func execTemplate(t *template.Template, root map[string]any, cfg *config) (string, error) {
	w := &limitWriter{max: cfg.maxOutputBytes}
	if cfg.timeout > 0 {
		w.deadline = time.Now().Add(cfg.timeout)
	}
	if err := t.Execute(w, root); err != nil {
		return "", err
	}
	return w.buf.String(), nil
}
