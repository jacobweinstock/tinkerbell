package kube

import (
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IndexType string

const (
	IndexTypeMACAddr         IndexType = MACAddrIndex
	IndexTypeIPAddr          IndexType = IPAddrIndex
	IndexTypeHardwareName    IndexType = "hardware.metadata.name"
	IndexTypeMachineName     IndexType = "machine.metadata.name"
	IndexTypeWorkflowAgentID IndexType = WorkflowByAgentID
	IndexTypeHardwareAgentID IndexType = HardwareByAgentID
)

// Indexes that are currently known.
var Indexes = map[IndexType]Index{
	IndexTypeMACAddr: {
		Obj:          &tinkerbell.Hardware{},
		Field:        MACAddrIndex,
		ExtractValue: MACAddrs,
	},
	IndexTypeIPAddr: {
		Obj:          &tinkerbell.Hardware{},
		Field:        IPAddrIndex,
		ExtractValue: IPAddrs,
	},
	IndexTypeHardwareName: {
		Obj:          &tinkerbell.Hardware{},
		Field:        HardwareNameIndex,
		ExtractValue: HardwareNameFunc,
	},
	IndexTypeMachineName: {
		Obj:          &bmc.Machine{},
		Field:        MachineNameIndex,
		ExtractValue: MachineNameFunc,
	},
	IndexTypeWorkflowAgentID: {
		Obj:          &tinkerbell.Workflow{},
		Field:        WorkflowByAgentID,
		ExtractValue: WorkflowByAgentIDFunc,
	},
	IndexTypeHardwareAgentID: {
		Obj:          &tinkerbell.Hardware{},
		Field:        HardwareByAgentID,
		ExtractValue: HardwareByAgentIDFunc,
	},
}

// MACAddrIndex is an index used with a controller-runtime client to lookup hardware by MAC.
const MACAddrIndex = ".Spec.Interfaces.MAC"

// MACAddrs returns a list of MAC addresses for a Hardware object.
func MACAddrs(obj client.Object) []string {
	hw, ok := obj.(*tinkerbell.Hardware)
	if !ok {
		return nil
	}
	return GetMACs(hw)
}

// GetMACs retrieves all MACs associated with h.
func GetMACs(h *tinkerbell.Hardware) []string {
	var macs []string
	for _, i := range h.Spec.Interfaces {
		if i.DHCP != nil && i.DHCP.MAC != "" {
			macs = append(macs, i.DHCP.MAC)
		}
	}

	return macs
}

// IPAddrIndex is an index used with a controller-runtime client to lookup hardware by IP.
const IPAddrIndex = ".Spec.Interfaces.DHCP.IP"

// IPAddrs returns a list of IP addresses for a Hardware object.
func IPAddrs(obj client.Object) []string {
	hw, ok := obj.(*tinkerbell.Hardware)
	if !ok {
		return nil
	}
	return GetIPs(hw)
}

// GetIPs retrieves all IP addresses.
func GetIPs(h *tinkerbell.Hardware) []string {
	var ips []string
	for _, i := range h.Spec.Interfaces {
		if i.DHCP != nil && i.DHCP.IP != nil && i.DHCP.IP.Address != "" {
			ips = append(ips, i.DHCP.IP.Address)
		}
	}
	return ips
}

// NameIndex is an index used with a controller-runtime client to lookup objects by name.
const HardwareNameIndex = ".metadata.name"

func HardwareNameFunc(obj client.Object) []string {
	hw, ok := obj.(*tinkerbell.Hardware)
	if !ok {
		return nil
	}
	return []string{hw.Name}
}

const MachineNameIndex = ".metadata.name"

func MachineNameFunc(obj client.Object) []string {
	m, ok := obj.(*bmc.Machine)
	if !ok {
		return nil
	}
	return []string{m.Name}
}

const WorkflowByAgentID = ".status.agentID"

func WorkflowByAgentIDFunc(obj client.Object) []string {
	wf, ok := obj.(*tinkerbell.Workflow)
	if !ok {
		return nil
	}
	if wf.Status.AgentID == "" {
		return []string{}
	}
	return []string{wf.Status.AgentID}
}

const HardwareByAgentID = ".spec.agentID"

func HardwareByAgentIDFunc(obj client.Object) []string {
	hw, ok := obj.(*tinkerbell.Hardware)
	if !ok {
		return nil
	}
	if hw.Spec.AgentID == "" {
		return []string{}
	}
	return []string{hw.Spec.AgentID}
}
