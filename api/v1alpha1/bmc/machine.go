/*
Copyright 2022 Tinkerbell.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bmc

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerState represents power state of a Machine.
type PowerState string

const (
	On      PowerState = "on"
	Off     PowerState = "off"
	Unknown PowerState = "unknown"
)

// MachineConditionType represents the condition of the Machine.
type MachineConditionType string

const (
	// Contactable defines that a connection can be made to the Machine.
	Contactable MachineConditionType = "Contactable"
)

// ConditionStatus represents the status of a Condition.
type ConditionStatus string

const (
	ConditionTrue  ConditionStatus = "True"
	ConditionFalse ConditionStatus = "False"
)

// MachineSpec defines desired machine state.
type MachineSpec struct {
	// Connection contains connection data for a Baseboard Management Controller.
	Connection Connection `json:"connection"`
}

// ProviderName is the bmclib specific provider name. Names are case insensitive.
// +kubebuilder:validation:Pattern=(?i)^(ipmitool|asrockrack|gofish|IntelAMT|dell|supermicro|openbmc)$
type ProviderName string

func (p ProviderName) String() string {
	return string(p)
}

// ProviderOptions hold provider specific configurable options.
type ProviderOptions struct {
	// PreferredOrder allows customizing the order that BMC providers are called.
	// Providers added to this list will be moved to the front of the default order.
	// Provider names are case insensitive.
	// The default order is: ipmitool, asrockrack, gofish, intelamt, dell, supermicro, openbmc.
	// +optional
	PreferredOrder []ProviderName `json:"preferredOrder,omitempty"`
	// IntelAMT contains the options to customize the IntelAMT provider.
	// +optional
	IntelAMT *IntelAMTOptions `json:"intelAMT,omitempty"`

	// IPMITOOL contains the options to customize the Ipmitool provider.
	// +optional
	IPMITOOL *IPMITOOLOptions `json:"ipmitool,omitempty"`

	// Redfish contains the options to customize the Redfish provider.
	// +optional
	Redfish *RedfishOptions `json:"redfish,omitempty"`

	// RPC contains the options to customize the RPC provider.
	// +optional
	RPC *RPCOptions `json:"rpc,omitempty"`
}

// Connection contains connection data for a Baseboard Management Controller.
type Connection struct {
	// Host is the host IP address or hostname of the Machine.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// Port is the port number for connecting with the Machine.
	// +kubebuilder:default:=623
	// +optional
	Port int `json:"port"`

	// AuthSecretRef is the SecretReference that contains authentication information of the Machine.
	// The Secret must contain username and password keys. This is optional as it is not required when using
	// the RPC provider.
	// +optional
	AuthSecretRef corev1.SecretReference `json:"authSecretRef"`

	// InsecureTLS specifies trusted TLS connections.
	InsecureTLS bool `json:"insecureTLS"`

	// ProviderOptions contains provider specific options.
	// +optional
	ProviderOptions *ProviderOptions `json:"providerOptions,omitempty"`
}

// MachineStatus defines the observed state of Machine.
type MachineStatus struct {
	// Power is the current power state of the Machine.
	// +kubebuilder:validation:Enum=on;off;unknown
	// +optional
	Power PowerState `json:"powerState,omitempty"`

	// Conditions represents the latest available observations of an object's current state.
	// +optional
	Conditions []MachineCondition `json:"conditions,omitempty"`
}

// MachineCondition defines an observed condition of a Machine.
type MachineCondition struct {
	// Type of the Machine condition.
	Type MachineConditionType `json:"type"`

	// Status of the condition.
	Status ConditionStatus `json:"status"`

	// LastUpdateTime of the condition.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`

	// Message is a human readable message indicating with details of the last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:generate=false
type MachineSetConditionOption func(*MachineCondition)

// SetCondition applies the cType condition to bm. If the condition already exists,
// it is updated.
func (bm *Machine) SetCondition(cType MachineConditionType, status ConditionStatus, opts ...MachineSetConditionOption) {
	var condition *MachineCondition

	// Check if there's an existing condition.
	for i, c := range bm.Status.Conditions {
		if c.Type == cType {
			condition = &bm.Status.Conditions[i]
			break
		}
	}

	// We didn't find an existing condition so create a new one and append it.
	if condition == nil {
		bm.Status.Conditions = append(bm.Status.Conditions, MachineCondition{
			Type: cType,
		})
		condition = &bm.Status.Conditions[len(bm.Status.Conditions)-1]
	}

	if condition.Status != status {
		condition.Status = status
		condition.LastUpdateTime = metav1.Now()
	}

	for _, opt := range opts {
		opt(condition)
	}
}

// WithMachineConditionMessage sets message m to the MachineCondition.
func WithMachineConditionMessage(m string) MachineSetConditionOption {
	return func(c *MachineCondition) {
		c.Message = m
	}
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=machines,scope=Namespaced,categories=tinkerbell,singular=machine
// +kubebuilder:metadata:labels=clusterctl.cluster.x-k8s.io=
// +kubebuilder:metadata:labels=clusterctl.cluster.x-k8s.io/move=

// Machine is the Schema for the machines API.
type Machine struct {
	metav1.TypeMeta   `json:""`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MachineSpec   `json:"spec,omitempty"`
	Status MachineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MachineList contains a list of Machines.
type MachineList struct {
	metav1.TypeMeta `json:""`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Machine `json:"items"`
}
