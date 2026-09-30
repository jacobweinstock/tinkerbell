package loom_test

import (
	"errors"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/tinkerbell/tinkerbell/pkg/loom"
)

func TestRenderValue(t *testing.T) {
	tests := map[string]struct {
		doc  map[string]any
		data map[string]any
		opts []loom.Option
		want map[string]any
	}{
		"self reference": {
			doc:  map[string]any{"name": "example", "greeting": "hello {{ .self.name }}"},
			want: map[string]any{"name": "example", "greeting": "hello example"},
		},
		"nested and list": {
			doc: map[string]any{"spec": map[string]any{
				"arch":  "x86_64",
				"disks": []any{map[string]any{"name": "{{ .self.spec.arch }}-disk"}},
			}},
			want: map[string]any{"spec": map[string]any{
				"arch":  "x86_64",
				"disks": []any{map[string]any{"name": "x86_64-disk"}},
			}},
		},
		"external data": {
			doc:  map[string]any{"greeting": "hello {{ .env.user }}"},
			data: map[string]any{"env": map[string]any{"user": "tink"}},
			want: map[string]any{"greeting": "hello tink"},
		},
		"chained through external data": {
			doc: map[string]any{
				"arch": "{{ .hw.arch }}",
				"msg":  "arch={{ .self.arch }}",
			},
			data: map[string]any{"hw": map[string]any{"arch": "aarch64"}},
			want: map[string]any{"arch": "aarch64", "msg": "arch=aarch64"},
		},
		"rendered values are always strings": {
			doc:  map[string]any{"n": int64(42), "flag": true, "count": "{{ .self.n }}", "on": "{{ .self.flag }}", "mode": `{{ "0644" }}`},
			want: map[string]any{"n": int64(42), "flag": true, "count": "42", "on": "true", "mode": "0644"},
		},
		"literal delimiter": {
			doc:  map[string]any{"jinja": `{{ "{{" }} ds.meta_data.hostname }}`},
			want: map[string]any{"jinja": "{{ ds.meta_data.hostname }}"},
		},
		"missing key disabled": {
			doc:  map[string]any{"greeting": "hello {{ .self.missing }}"},
			opts: []loom.Option{loom.WithMissingKeyError(false)},
			want: map[string]any{"greeting": "hello <no value>"},
		},
		"funcs": {
			doc:  map[string]any{"name": "example", "shout": "{{ .self.name | upper }}"},
			opts: []loom.Option{loom.WithFuncs(template.FuncMap{"upper": strings.ToUpper})},
			want: map[string]any{"name": "example", "shout": "EXAMPLE"},
		},
		"custom self key": {
			doc:  map[string]any{"name": "example", "greeting": "hi {{ .hardware.name }}"},
			opts: []loom.Option{loom.WithSelfKey("hardware")},
			want: map[string]any{"name": "example", "greeting": "hi example"},
		},
		"skipped values stay unrendered and readable": {
			doc: map[string]any{
				"metadata": map[string]any{"note": "{{ .self.x }}"},
				"copy":     "{{ .self.metadata.note }}",
			},
			opts: []loom.Option{loom.WithSkip(func(p string) bool { return strings.HasPrefix(p, "metadata.") })},
			want: map[string]any{
				"metadata": map[string]any{"note": "{{ .self.x }}"},
				"copy":     "{{ .self.x }}",
			},
		},
		"binary output is preserved": {
			doc:  map[string]any{"der": "{{ raw }}"},
			opts: []loom.Option{loom.WithFuncs(template.FuncMap{"raw": func() string { return "\x30\x82\x00\xff\xfe" }})},
			want: map[string]any{"der": "\x30\x82\x00\xff\xfe"},
		},
		"no templates": {
			doc:  map[string]any{"a": "b", "n": int64(1)},
			want: map[string]any{"a": "b", "n": int64(1)},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := loom.RenderValue(tt.doc, tt.data, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenderValueDataIsNotTemplated(t *testing.T) {
	data := map[string]any{"ref": map[string]any{"v": "{{ .self.secret }}"}}
	doc := map[string]any{"secret": "s", "out": "{{ .ref.v }}"}

	got, err := loom.RenderValue(doc, data)
	if err != nil {
		t.Fatal(err)
	}
	if out := got.(map[string]any)["out"]; out != "{{ .self.secret }}" {
		t.Fatalf("out = %q, want the data value verbatim", out)
	}
}

func TestRenderValueErrors(t *testing.T) {
	balloon := template.FuncMap{"balloon": func() string { return strings.Repeat("x", 1<<20) }}

	tests := map[string]struct {
		doc      map[string]any
		data     map[string]any
		opts     []loom.Option
		wantIs   error
		wantPath string
	}{
		"cycle": {
			doc:    map[string]any{"a": "{{ .self.b }}", "b": "{{ .self.a }}"},
			wantIs: loom.ErrReferenceCycle,
		},
		"missing key": {
			doc:      map[string]any{"greeting": "hello {{ .self.missing }}"},
			wantPath: "greeting",
		},
		"parse error": {
			doc:      map[string]any{"list": []any{"{{ .self.x "}},
			wantPath: "list[0]",
		},
		"output too large": {
			doc:      map[string]any{"big": "{{ balloon }}"},
			opts:     []loom.Option{loom.WithFuncs(balloon), loom.WithMaxOutputBytes(1024)},
			wantIs:   loom.ErrOutputTooLarge,
			wantPath: "big",
		},
		"timeout": {
			doc:      map[string]any{"spin": "{{ range .items }}{{ . }}{{ end }}"},
			data:     map[string]any{"items": make([]int, 5_000_000)},
			opts:     []loom.Option{loom.WithRenderTimeout(time.Millisecond)},
			wantIs:   loom.ErrRenderTimeout,
			wantPath: "spin",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loom.RenderValue(tt.doc, tt.data, tt.opts...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v, want %v", err, tt.wantIs)
			}
			if tt.wantPath != "" {
				var fe *loom.FieldError
				if !errors.As(err, &fe) || fe.Path != tt.wantPath {
					t.Errorf("err = %v, want *FieldError at %q", err, tt.wantPath)
				}
			}
		})
	}
}

func TestRenderValueOutputCapDisabled(t *testing.T) {
	funcs := template.FuncMap{"balloon": func() string { return strings.Repeat("x", 4096) }}
	got, err := loom.RenderValue(map[string]any{"big": "{{ balloon }}"}, nil, loom.WithFuncs(funcs), loom.WithMaxOutputBytes(0))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.(map[string]any)["big"].(string)); n != 4096 {
		t.Fatalf("len = %d, want 4096", n)
	}
}
