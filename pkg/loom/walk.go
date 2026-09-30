package loom

import (
	"fmt"
	"strings"
	"text/template"
)

// leaf is a templated string value located in the document.
type leaf struct {
	path string             // dotted/indexed location, e.g. spec.instance.hostname
	src  string             // original template text
	tmpl *template.Template // parsed template
	set  func(any)          // writes the rendered value back into the parent container
	refs []string           // self-reference paths (self key stripped); "" means the whole document
	deps []*leaf            // leaves that must render first
}

// collectLeaves walks the document, appending a leaf for every string value
// that contains the left delimiter and is not skipped. set writes a new value
// into the parent container so the rendered result lands back in the document.
func collectLeaves(node any, path string, set func(any), cfg *config, out *[]*leaf) {
	switch n := node.(type) {
	case map[string]any:
		for k := range n {
			collectLeaves(n[k], joinKey(path, k), func(v any) { n[k] = v }, cfg, out)
		}
	case []any:
		for i := range n {
			collectLeaves(n[i], fmt.Sprintf("%s[%d]", path, i), func(v any) { n[i] = v }, cfg, out)
		}
	case string:
		if strings.Contains(n, leftDelim) && (cfg.skip == nil || !cfg.skip(path)) {
			*out = append(*out, &leaf{path: path, src: n, set: set})
		}
	}
}

// parse builds the leaf's template and derives its self-references.
func (l *leaf) parse(cfg *config) error {
	t, err := buildTemplate(l.src, cfg)
	if err != nil {
		return &FieldError{Path: l.path, Err: err}
	}
	l.tmpl = t
	l.refs = selfRefs(t, cfg.selfKey)
	return nil
}

func joinKey(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}
