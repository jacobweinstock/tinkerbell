package transform

import (
	"fmt"

	v1bmc "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	v2bmc "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell/bmc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Workflow converts a v1alpha1 Workflow's spec into a v1alpha2 Workflow.
// Status is intentionally left zero-valued because the result is
// archive-only and is never applied.
//
// Spec rewrites:
//
//   - TemplateRef (string) -> Spec.Tasks []WorkflowTask, with one entry
//     per Task that the referenced Template produced. The mapping is
//     supplied by the caller via refs. If refs does not contain the
//     TemplateRef, a single placeholder TaskRef using the original name
//     is emitted so the archive is still parseable.
//   - HardwareRef (string) -> WorkflowTask.Hardware.HardwareRef
//     (SimpleReference). Applied to every generated WorkflowTask.
//   - HardwareMap: dropped (no v2 equivalent).
//   - BootOptions.ToggleAllowNetboot -> BootOptions.ToggleNetboot.
//   - BootOptions.BootMode "iso" -> "isoboot" (v2 removed the "iso"
//     enum value). Other values pass through.
//   - BootOptions.CustombootConfig.{PreparingActions,PostActions}
//     ([]bmc.Action) -> Customboot.{PreOperations,PostOperations}
//     ([]bmc.Operations).
//   - Spec.Disabled (*bool, true) -> tinkerbell.org/disabled annotation
//     (value "true"). The v2 Workflow has no Disabled field on the
//     spec; the controller reads the annotation instead.
func Workflow(src *v1.Workflow, refs TemplateRefs) (*v2.Workflow, error) {
	if src == nil {
		return nil, fmt.Errorf("Workflow: nil source")
	}

	meta := cleanObjectMeta(src.ObjectMeta)
	if src.Spec.Disabled != nil && *src.Spec.Disabled {
		if meta.Annotations == nil {
			meta.Annotations = map[string]string{}
		}
		meta.Annotations["tinkerbell.org/disabled"] = "true"
	}

	// Build the list of WorkflowTasks. Resolve the Template name to
	// the v1alpha2 Task refs produced during Template transform.
	var taskRefs []v2.SimpleReference
	if r, ok := refs[src.Spec.TemplateRef]; ok && len(r) > 0 {
		taskRefs = r
	} else if src.Spec.TemplateRef != "" {
		taskRefs = []v2.SimpleReference{{Name: src.Spec.TemplateRef, Namespace: src.Namespace}}
	}

	var hardware *v2.WorkflowHardware
	if src.Spec.HardwareRef != "" || !src.Spec.BootOptions.IsZero() {
		hardware = &v2.WorkflowHardware{
			BootOptions: convertWorkflowBootOptions(src.Spec.BootOptions),
		}
		if src.Spec.HardwareRef != "" {
			hardware.HardwareRef = &v2.SimpleReference{
				Name:      src.Spec.HardwareRef,
				Namespace: src.Namespace,
			}
		}
	}

	wfTasks := make([]v2.WorkflowTask, 0, len(taskRefs))
	for _, ref := range taskRefs {
		wt := v2.WorkflowTask{TaskRef: ref}
		if hardware != nil {
			h := *hardware
			wt.Hardware = &h
		}
		wfTasks = append(wfTasks, wt)
	}

	return &v2.Workflow{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v2.GroupVersion.String(),
			Kind:       "Workflow",
		},
		ObjectMeta: meta,
		Spec: v2.WorkflowSpec{
			Tasks: wfTasks,
		},
	}, nil
}

func convertWorkflowBootOptions(in v1.BootOptions) v2.BootOptions {
	out := v2.BootOptions{
		ToggleNetboot: in.ToggleAllowNetboot,
		ISOURL:        in.ISOURL,
		BootMode:      convertBootMode(in.BootMode),
	}
	if !in.CustombootConfig.IsZero() {
		cb := &v2.Customboot{
			PreOperations:  convertBMCActions(in.CustombootConfig.PreparingActions),
			PostOperations: convertBMCActions(in.CustombootConfig.PostActions),
		}
		out.Customboot = cb
	}
	return out
}

func convertBootMode(in v1.BootMode) v2.BootMode {
	if in == "iso" {
		return v2.BootModeIsoboot
	}
	return v2.BootMode(in)
}

// convertBMCActions converts v1 bmc.Action entries to v2 bmc.Operations.
// PowerAction, BootDevice, and VirtualMediaAction are copied directly
// because their string values match. The deprecated
// OneTimeBootDeviceAction is folded into BootDevice using the first
// device in the slice.
func convertBMCActions(in []v1bmc.Action) []v2bmc.Operations {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2bmc.Operations, 0, len(in))
	for _, a := range in {
		op := v2bmc.Operations{}
		if a.PowerAction != nil {
			pa := v2bmc.PowerAction(string(*a.PowerAction))
			op.PowerAction = &pa
		}
		if a.BootDevice != nil {
			op.BootDevice = &v2bmc.BootDeviceConfig{
				Device:     v2bmc.BootDevice(string(a.BootDevice.Device)),
				Persistent: a.BootDevice.Persistent,
				EFIBoot:    a.BootDevice.EFIBoot,
			}
		} else if a.OneTimeBootDeviceAction != nil && len(a.OneTimeBootDeviceAction.Devices) > 0 {
			op.BootDevice = &v2bmc.BootDeviceConfig{
				Device:  v2bmc.BootDevice(string(a.OneTimeBootDeviceAction.Devices[0])),
				EFIBoot: a.OneTimeBootDeviceAction.EFIBoot,
			}
		}
		if a.VirtualMediaAction != nil {
			op.VirtualMediaAction = &v2bmc.VirtualMediaAction{
				MediaURL: a.VirtualMediaAction.MediaURL,
				Kind:     v2bmc.VirtualMediaKind(string(a.VirtualMediaAction.Kind)),
			}
		}
		out = append(out, op)
	}
	return out
}
