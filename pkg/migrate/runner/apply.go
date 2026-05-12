package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// applyGVRs maps target directory names to the v1alpha2
// GroupVersionResource used when calling the dynamic client.
var applyGVRs = map[string]schema.GroupVersionResource{
	"hardware": {Group: "tinkerbell.org", Version: "v1alpha2", Resource: "hardware"},
	"task":     {Group: "tinkerbell.org", Version: "v1alpha2", Resource: "tasks"},
	"policy":   {Group: "tinkerbell.org", Version: "v1alpha2", Resource: "policies"},
	"bmc":      {Group: "tinkerbell.org", Version: "v1alpha2", Resource: "bmcs"},
}

const fieldManager = "tinkerbell-migrate"

// runApplyObjects applies every YAML file under target-v1alpha2/<kind>/
// (skipping target-v1alpha2/archive/) to the cluster using
// server-side apply. Per-kind state is recorded in
// PhaseState.ApplyObjects.
func (r *Runner) runApplyObjects(ctx context.Context, state *State) error {
	for _, target := range ApplyKinds() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.Phases.ApplyObjects[target] == PhaseDone {
			continue
		}
		state.SetApplyObjects(target, PhaseInProgress)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		if err := r.applyKind(ctx, state, target); err != nil {
			state.SetApplyObjects(target, PhaseFailed)
			_ = state.Save(r.layout)
			return fmt.Errorf("apply %s: %w", target, err)
		}
		state.SetApplyObjects(target, PhaseDone)
		if err := state.Save(r.layout); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) applyKind(ctx context.Context, state *State, target string) error {
	gvr, ok := applyGVRs[target]
	if !ok {
		return fmt.Errorf("no GVR known for target %q", target)
	}
	dir := r.layout.TargetKindDir(target)
	files, err := listYAMLs(dir)
	if err != nil {
		return err
	}
	// applyKind state is per target-kind, not per source-kind. Find the
	// corresponding source kind so per-kind counts stay coherent with
	// the transform phase.
	sourceKind := target
	for _, k := range SourceKinds {
		if k.TargetName == target {
			sourceKind = k.Name
			break
		}
	}
	counts := state.Count(sourceKind)
	for _, name := range files {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		obj, err := decodeUnstructured(data)
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if err := r.client.ServerSideApply(ctx, gvr, true, obj, fieldManager); err != nil {
			counts.Failed++
			return fmt.Errorf("apply %s: %w", path, err)
		}
		counts.Applied++
	}
	return nil
}

func decodeUnstructured(data []byte) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &obj.Object); err != nil {
		return nil, err
	}
	return obj, nil
}
