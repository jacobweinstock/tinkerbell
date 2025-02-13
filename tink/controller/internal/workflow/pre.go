package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinkerbell/tinkerbell/pkg/api/v1alpha1/bmc"
	v1alpha1 "github.com/tinkerbell/tinkerbell/pkg/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/tink/controller/internal/workflow/journal"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// prepareWorkflow prepares the workflow for execution.
// The workflow (s.workflow) can be updated even if an error occurs.
// Any patching of the workflow object in a cluster is left up to the caller.
// At the moment prepareWorkflow requires the workflow have a hardwareRef and the object exists.
func (s *state) prepareWorkflow(ctx context.Context) (reconcile.Result, error) {
	// handle bootoptions
	// 1. Handle toggling allowPXE in a hardware object if toggleAllowNetboot is true.
	if s.workflow.Spec.BootOptions.ToggleAllowNetboot && !s.workflow.Status.BootOptions.AllowNetboot.ToggledTrue {
		journal.Log(ctx, "toggling allowPXE true")
		if err := s.toggleHardware(ctx, true); err != nil {
			return reconcile.Result{}, err
		}
	}

	// 2. Handle booting scenarios.
	switch s.workflow.Spec.BootOptions.BootMode {
	case v1alpha1.BootModeNetboot:
		name := jobName(fmt.Sprintf("%s-%s", jobNameNetboot, s.workflow.GetName()))
		if j := s.workflow.Status.BootOptions.Jobs[name.String()]; !j.ExistingJobDeleted || j.UID == "" || !j.Complete {
			journal.Log(ctx, "boot mode netboot")
			hw, err := hardwareFrom(ctx, s.client, s.workflow)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to get hardware: %w", err)
			}
			efiBoot := func() bool {
				for _, iface := range hw.Spec.Interfaces {
					if iface.DHCP != nil && iface.DHCP.UEFI {
						return true
					}
				}
				return false
			}()
			actions := []bmc.Action{
				{
					PowerAction: bmc.PowerHardOff.Ptr(),
				},
				{
					OneTimeBootDeviceAction: &bmc.OneTimeBootDeviceAction{
						Devices: []bmc.BootDevice{
							bmc.PXE,
						},
						EFIBoot: efiBoot,
					},
				},
				{
					PowerAction: bmc.PowerOn.Ptr(),
				},
			}

			r, err := s.handleJob(ctx, actions, name)
			if s.workflow.Status.BootOptions.Jobs[name.String()].Complete && s.workflow.Status.State == v1alpha1.WorkflowStatePreparing {
				s.workflow.Status.State = v1alpha1.WorkflowStatePending
			}
			return r, err
		}
	case v1alpha1.BootModeISO:
		name := jobName(fmt.Sprintf("%s-%s", jobNameISOMount, s.workflow.GetName()))
		if j := s.workflow.Status.BootOptions.Jobs[name.String()]; !j.ExistingJobDeleted || j.UID == "" || !j.Complete {
			journal.Log(ctx, "boot mode iso")
			if s.workflow.Spec.BootOptions.ISOURL == "" {
				return reconcile.Result{}, errors.New("iso url must be a valid url")
			}
			hw, err := hardwareFrom(ctx, s.client, s.workflow)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to get hardware: %w", err)
			}
			efiBoot := func() bool {
				for _, iface := range hw.Spec.Interfaces {
					if iface.DHCP != nil && iface.DHCP.UEFI {
						return true
					}
				}
				return false
			}()
			actions := []bmc.Action{
				{
					PowerAction: bmc.PowerHardOff.Ptr(),
				},
				{
					VirtualMediaAction: &bmc.VirtualMediaAction{
						MediaURL: "", // empty to unmount/eject the media
						Kind:     bmc.VirtualMediaCD,
					},
				},
				{
					VirtualMediaAction: &bmc.VirtualMediaAction{
						MediaURL: s.workflow.Spec.BootOptions.ISOURL,
						Kind:     bmc.VirtualMediaCD,
					},
				},
				{
					OneTimeBootDeviceAction: &bmc.OneTimeBootDeviceAction{
						Devices: []bmc.BootDevice{
							bmc.CDROM,
						},
						EFIBoot: efiBoot,
					},
				},
				{
					PowerAction: bmc.PowerOn.Ptr(),
				},
			}

			r, err := s.handleJob(ctx, actions, name)
			if s.workflow.Status.BootOptions.Jobs[name.String()].Complete && s.workflow.Status.State == v1alpha1.WorkflowStatePreparing {
				s.workflow.Status.State = v1alpha1.WorkflowStatePending
			}
			return r, err
		}
	}
	s.workflow.Status.State = v1alpha1.WorkflowStatePending

	return reconcile.Result{}, nil
}
