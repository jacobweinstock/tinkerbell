package loom_test

import (
	"strings"
	"testing"

	"github.com/tinkerbell/tinkerbell/pkg/loom"
)

// FuzzRenderValue checks that RenderValue never panics and that every rendered
// value is a string.
func FuzzRenderValue(f *testing.F) {
	for _, s := range []string{
		"{{ .self.name }}",
		"plain",
		"{{ .self.field }}",
		"{{ .self.a }}{{ .self.field }}",
		`{{ printf "%s" .self.field }}`,
		`{{ "{{" }}`,
		"{{ range .self }}{{ . }}{{ end }}",
		"{{",
		"",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, tmpl string) {
		doc := map[string]any{"name": "x", "a": []any{"{{ .self.name }}"}, "field": tmpl}
		got, err := loom.RenderValue(doc, nil)
		if err != nil {
			return
		}
		if v, ok := got.(map[string]any)["field"].(string); !ok || (!strings.Contains(tmpl, "{{") && v != tmpl) {
			t.Fatalf("field = %#v for template %q", got.(map[string]any)["field"], tmpl)
		}
	})
}
