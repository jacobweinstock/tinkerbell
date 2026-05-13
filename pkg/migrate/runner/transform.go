package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v1bmc "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/migrate/transform"
	"sigs.k8s.io/yaml"
)

// runTransform runs the transform phase for every kind in catalog
// order. Templates are processed before Workflows so the TemplateRefs
// map built from Template fan-out is available to Workflow.
func (r *Runner) runTransform(ctx context.Context, state *State) (rerr error) {
	r.progress.PhaseStart("transform")
	defer func() { r.progress.PhaseEnd("transform", rerr) }()
	templateRefs := transform.TemplateRefs{}
	for _, k := range SourceKinds {
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.Phases.Transform[k.Name] == PhaseDone {
			// Even if done, we may need to rebuild templateRefs for
			// later kinds (Workflow). Read it back from disk.
			if k.Name == "template" {
				if err := r.rebuildTemplateRefs(templateRefs); err != nil {
					return fmt.Errorf("rebuild template refs from disk: %w", err)
				}
			}
			continue
		}
		state.SetTransform(k.Name, PhaseInProgress)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		if err := r.transformKind(state, k, templateRefs); err != nil {
			state.SetTransform(k.Name, PhaseFailed)
			_ = state.Save(r.layout)
			r.progress.KindEnd("transform", k.Name, err)
			return fmt.Errorf("transform %s: %w", k.Name, err)
		}
		state.SetTransform(k.Name, PhaseDone)
		if err := state.Save(r.layout); err != nil {
			return err
		}
		r.progress.KindEnd("transform", k.Name, nil)
	}
	return nil
}

func (r *Runner) transformKind(state *State, k SourceKind, templateRefs transform.TemplateRefs) error {
	srcDir := r.layout.SourceKindDir(k.Name)
	files, err := listYAMLs(srcDir)
	if err != nil {
		return err
	}
	r.progress.KindStart("transform", k.Name, len(files))
	counts := state.Count(k.Name)
	for _, name := range files {
		path := filepath.Join(srcDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := r.transformOne(k, name, data, templateRefs); err != nil {
			counts.Failed++
			return fmt.Errorf("transform %s: %w", path, err)
		}
		if k.Handling == HandlingDrop {
			counts.Discarded++
		} else {
			counts.Transformed++
		}
		r.progress.KindItem("transform", k.Name)
	}
	return nil
}

// transformOne dispatches a single source file's bytes through the
// per-kind transform and writes the output(s) to the appropriate
// target directory.
func (r *Runner) transformOne(k SourceKind, base string, data []byte, templateRefs transform.TemplateRefs) error {
	switch k.Name {
	case "hardware":
		var in v1.Hardware
		if err := yaml.Unmarshal(data, &in); err != nil {
			return fmt.Errorf("decode v1 Hardware: %w", err)
		}
		out, err := transform.Hardware(&in)
		if err != nil {
			return err
		}
		return r.writeApply(k, out, out.Namespace, out.Name)
	case "template":
		var in v1.Template
		if err := yaml.Unmarshal(data, &in); err != nil {
			return fmt.Errorf("decode v1 Template: %w", err)
		}
		outs, err := transform.Template(&in)
		if err != nil {
			return err
		}
		refs := make([]v2.SimpleReference, 0, len(outs))
		for _, out := range outs {
			if err := r.writeApply(k, out, out.Namespace, out.Name); err != nil {
				return err
			}
			refs = append(refs, v2.SimpleReference{Name: out.Name, Namespace: out.Namespace})
		}
		templateRefs[in.Name] = refs
		return nil
	case "workflowruleset":
		var in v1.WorkflowRuleSet
		if err := yaml.Unmarshal(data, &in); err != nil {
			return fmt.Errorf("decode v1 WorkflowRuleSet: %w", err)
		}
		out, err := transform.WorkflowRuleSet(&in)
		if err != nil {
			return err
		}
		return r.writeApply(k, out, out.Namespace, out.Name)
	case "bmcmachine":
		var in v1bmc.Machine
		if err := yaml.Unmarshal(data, &in); err != nil {
			return fmt.Errorf("decode v1 bmc.Machine: %w", err)
		}
		out, err := transform.BMCMachine(&in)
		if err != nil {
			return err
		}
		return r.writeApply(k, out, out.Namespace, out.Name)
	case "workflow":
		var in v1.Workflow
		if err := yaml.Unmarshal(data, &in); err != nil {
			return fmt.Errorf("decode v1 Workflow: %w", err)
		}
		out, err := transform.Workflow(&in, templateRefs)
		if err != nil {
			return err
		}
		return r.writeArchive(k, out, out.Namespace, out.Name)
	case "bmcjob":
		// Verbatim archive: copy the source bytes through.
		dir := r.layout.ArchiveKindDir(k.Name)
		if err := mkdirAll(dir); err != nil {
			return err
		}
		path := filepath.Join(dir, base)
		return writeFileAtomic(path, data)
	case "bmctask":
		// Drop.
		return nil
	default:
		return fmt.Errorf("unknown kind %q", k.Name)
	}
}

func (r *Runner) writeApply(k SourceKind, obj any, namespace, name string) error {
	if k.TargetName == "" {
		return fmt.Errorf("kind %q has no TargetName but is being written to apply dir", k.Name)
	}
	dir := r.layout.TargetKindDir(k.TargetName)
	if err := mkdirAll(dir); err != nil {
		return err
	}
	data, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, objectFilename(namespace, name))
	return writeFileAtomic(path, data)
}

func (r *Runner) writeArchive(k SourceKind, obj any, namespace, name string) error {
	dir := r.layout.ArchiveKindDir(k.Name)
	if err := mkdirAll(dir); err != nil {
		return err
	}
	data, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, objectFilename(namespace, name))
	return writeFileAtomic(path, data)
}

// rebuildTemplateRefs reads target-v1alpha2/task/*.yaml and reconstructs
// the TemplateRefs map. This is used during resume when the Template
// transform was already marked done in state.json but the Workflow
// transform has not run yet. The reconstruction relies on the
// deterministic Task naming used by transform.Template:
//
//   - single-task templates produce one Task named <template-name>;
//   - multi-task templates produce <template-name>-<task-name>.
//
// Both shapes resolve back to the original template name with the
// help of the source-v1alpha1/template/ directory listing.
func (r *Runner) rebuildTemplateRefs(refs transform.TemplateRefs) error {
	// Walk source templates so we know the original names.
	srcDir := r.layout.SourceKindDir("template")
	files, err := listYAMLs(srcDir)
	if err != nil {
		return err
	}
	taskDir := r.layout.TargetKindDir("task")
	taskFiles, err := listYAMLs(taskDir)
	if err != nil {
		return err
	}
	for _, sf := range files {
		ns, tmplName, ok := parseObjectFilename(sf)
		if !ok {
			continue
		}
		prefix := tmplName
		out := []v2.SimpleReference{}
		for _, tf := range taskFiles {
			tns, tname, ok := parseObjectFilename(tf)
			if !ok || tns != ns {
				continue
			}
			if tname == tmplName || (len(tname) > len(prefix) && tname[:len(prefix)+1] == prefix+"-") {
				out = append(out, v2.SimpleReference{Name: tname, Namespace: tns})
			}
		}
		if len(out) > 0 {
			refs[tmplName] = out
		}
	}
	return nil
}
