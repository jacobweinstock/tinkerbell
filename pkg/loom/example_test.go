package loom_test

import (
	"fmt"

	"github.com/tinkerbell/tinkerbell/pkg/loom"
)

func ExampleRenderValue() {
	doc := map[string]any{
		"name": "machine1",
		"fqdn": "{{ .self.name }}.{{ .references.net.domain }}",
	}
	data := map[string]any{"references": map[string]any{"net": map[string]any{"domain": "example.org"}}}

	out, err := loom.RenderValue(doc, data)
	if err != nil {
		panic(err)
	}
	fmt.Println(out.(map[string]any)["fqdn"])
	// Output: machine1.example.org
}
