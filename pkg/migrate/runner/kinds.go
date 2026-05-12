package runner

import (
	"errors"
	"io/fs"
	"os"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Handling describes what the runner does with a given source kind.
type Handling string

const (
	// HandlingApply: transform and apply to the cluster.
	HandlingApply Handling = "apply"
	// HandlingArchive: transform and write to target-v1alpha2/archive/,
	// never apply.
	HandlingArchive Handling = "archive"
	// HandlingArchiveVerbatim: copy the source YAML to
	// target-v1alpha2/archive/ with no transform, never apply.
	HandlingArchiveVerbatim Handling = "archive_verbatim"
	// HandlingDrop: log and discard.
	HandlingDrop Handling = "drop"
)

// SourceKind describes one v1alpha1 kind from the runner's
// perspective. The Name is the short lowercase identifier used in the
// workdir layout and state.json. The TargetName is used only for
// HandlingApply kinds to compute the output directory; for archive and
// drop it is empty.
type SourceKind struct {
	Name       string
	TargetName string
	GVR        schema.GroupVersionResource
	Handling   Handling
}

// SourceKinds lists the v1alpha1 kinds in the order the runner
// processes them. Ordering matters for HandlingApply because the
// Workflow transform consumes a TemplateRefs map built when Templates
// are transformed, so template must precede workflow.
var SourceKinds = []SourceKind{
	{
		Name:       "hardware",
		TargetName: "hardware",
		GVR:        schema.GroupVersionResource{Group: "tinkerbell.org", Version: "v1alpha1", Resource: "hardware"},
		Handling:   HandlingApply,
	},
	{
		Name:       "template",
		TargetName: "task",
		GVR:        schema.GroupVersionResource{Group: "tinkerbell.org", Version: "v1alpha1", Resource: "templates"},
		Handling:   HandlingApply,
	},
	{
		Name:       "workflowruleset",
		TargetName: "policy",
		GVR:        schema.GroupVersionResource{Group: "tinkerbell.org", Version: "v1alpha1", Resource: "workflowrulesets"},
		Handling:   HandlingApply,
	},
	{
		Name:       "bmcmachine",
		TargetName: "bmc",
		GVR:        schema.GroupVersionResource{Group: "bmc.tinkerbell.org", Version: "v1alpha1", Resource: "machines"},
		Handling:   HandlingApply,
	},
	{
		Name:     "workflow",
		GVR:      schema.GroupVersionResource{Group: "tinkerbell.org", Version: "v1alpha1", Resource: "workflows"},
		Handling: HandlingArchive,
	},
	{
		Name:     "bmcjob",
		GVR:      schema.GroupVersionResource{Group: "bmc.tinkerbell.org", Version: "v1alpha1", Resource: "jobs"},
		Handling: HandlingArchiveVerbatim,
	},
	{
		Name:     "bmctask",
		GVR:      schema.GroupVersionResource{Group: "bmc.tinkerbell.org", Version: "v1alpha1", Resource: "tasks"},
		Handling: HandlingDrop,
	},
}

// FindSourceKind returns the SourceKind whose Name matches name, or
// nil when no such kind exists. Useful in tests and the kind catalog
// lookup helpers.
func FindSourceKind(name string) *SourceKind {
	for i := range SourceKinds {
		if SourceKinds[i].Name == name {
			return &SourceKinds[i]
		}
	}
	return nil
}

// ApplyKinds returns the lowercase target directory names for the
// kinds that get applied (excludes archive and drop).
func ApplyKinds() []string {
	out := []string{}
	for _, k := range SourceKinds {
		if k.Handling == HandlingApply {
			out = append(out, k.TargetName)
		}
	}
	return out
}

func mkdirAll(p string) error {
	if err := os.MkdirAll(p, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}
