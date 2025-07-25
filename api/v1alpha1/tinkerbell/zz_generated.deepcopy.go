//go:build !ignore_autogenerated

/*
Copyright 2025 Tinkerbell.

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

// Code generated by controller-gen. DO NOT EDIT.

package tinkerbell

import (
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Action) DeepCopyInto(out *Action) {
	*out = *in
	if in.Command != nil {
		in, out := &in.Command, &out.Command
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Environment != nil {
		in, out := &in.Environment, &out.Environment
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.ExecutionStart != nil {
		in, out := &in.ExecutionStart, &out.ExecutionStart
		*out = (*in).DeepCopy()
	}
	if in.ExecutionStop != nil {
		in, out := &in.ExecutionStop, &out.ExecutionStop
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Action.
func (in *Action) DeepCopy() *Action {
	if in == nil {
		return nil
	}
	out := new(Action)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AllowNetbootStatus) DeepCopyInto(out *AllowNetbootStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AllowNetbootStatus.
func (in *AllowNetbootStatus) DeepCopy() *AllowNetbootStatus {
	if in == nil {
		return nil
	}
	out := new(AllowNetbootStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AutoCapabilities) DeepCopyInto(out *AutoCapabilities) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AutoCapabilities.
func (in *AutoCapabilities) DeepCopy() *AutoCapabilities {
	if in == nil {
		return nil
	}
	out := new(AutoCapabilities)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BootOptions) DeepCopyInto(out *BootOptions) {
	*out = *in
	in.CustombootConfig.DeepCopyInto(&out.CustombootConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BootOptions.
func (in *BootOptions) DeepCopy() *BootOptions {
	if in == nil {
		return nil
	}
	out := new(BootOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BootOptionsStatus) DeepCopyInto(out *BootOptionsStatus) {
	*out = *in
	out.AllowNetboot = in.AllowNetboot
	if in.Jobs != nil {
		in, out := &in.Jobs, &out.Jobs
		*out = make(map[string]JobStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BootOptionsStatus.
func (in *BootOptionsStatus) DeepCopy() *BootOptionsStatus {
	if in == nil {
		return nil
	}
	out := new(BootOptionsStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CurrentState) DeepCopyInto(out *CurrentState) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CurrentState.
func (in *CurrentState) DeepCopy() *CurrentState {
	if in == nil {
		return nil
	}
	out := new(CurrentState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CustombootConfig) DeepCopyInto(out *CustombootConfig) {
	*out = *in
	if in.PreparingActions != nil {
		in, out := &in.PreparingActions, &out.PreparingActions
		*out = make([]bmc.Action, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.PostActions != nil {
		in, out := &in.PostActions, &out.PostActions
		*out = make([]bmc.Action, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CustombootConfig.
func (in *CustombootConfig) DeepCopy() *CustombootConfig {
	if in == nil {
		return nil
	}
	out := new(CustombootConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DHCP) DeepCopyInto(out *DHCP) {
	*out = *in
	if in.NameServers != nil {
		in, out := &in.NameServers, &out.NameServers
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.TimeServers != nil {
		in, out := &in.TimeServers, &out.TimeServers
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.IP != nil {
		in, out := &in.IP, &out.IP
		*out = new(IP)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DHCP.
func (in *DHCP) DeepCopy() *DHCP {
	if in == nil {
		return nil
	}
	out := new(DHCP)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Disk) DeepCopyInto(out *Disk) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Disk.
func (in *Disk) DeepCopy() *Disk {
	if in == nil {
		return nil
	}
	out := new(Disk)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Hardware) DeepCopyInto(out *Hardware) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Hardware.
func (in *Hardware) DeepCopy() *Hardware {
	if in == nil {
		return nil
	}
	out := new(Hardware)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Hardware) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HardwareList) DeepCopyInto(out *HardwareList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Hardware, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HardwareList.
func (in *HardwareList) DeepCopy() *HardwareList {
	if in == nil {
		return nil
	}
	out := new(HardwareList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *HardwareList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HardwareMetadata) DeepCopyInto(out *HardwareMetadata) {
	*out = *in
	if in.Manufacturer != nil {
		in, out := &in.Manufacturer, &out.Manufacturer
		*out = new(MetadataManufacturer)
		**out = **in
	}
	if in.Instance != nil {
		in, out := &in.Instance, &out.Instance
		*out = new(MetadataInstance)
		(*in).DeepCopyInto(*out)
	}
	if in.Custom != nil {
		in, out := &in.Custom, &out.Custom
		*out = new(MetadataCustom)
		(*in).DeepCopyInto(*out)
	}
	if in.Facility != nil {
		in, out := &in.Facility, &out.Facility
		*out = new(MetadataFacility)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HardwareMetadata.
func (in *HardwareMetadata) DeepCopy() *HardwareMetadata {
	if in == nil {
		return nil
	}
	out := new(HardwareMetadata)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HardwareSpec) DeepCopyInto(out *HardwareSpec) {
	*out = *in
	out.Auto = in.Auto
	if in.BMCRef != nil {
		in, out := &in.BMCRef, &out.BMCRef
		*out = new(v1.TypedLocalObjectReference)
		(*in).DeepCopyInto(*out)
	}
	if in.Interfaces != nil {
		in, out := &in.Interfaces, &out.Interfaces
		*out = make([]Interface, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.References != nil {
		in, out := &in.References, &out.References
		*out = make(map[string]Reference, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Metadata != nil {
		in, out := &in.Metadata, &out.Metadata
		*out = new(HardwareMetadata)
		(*in).DeepCopyInto(*out)
	}
	if in.Disks != nil {
		in, out := &in.Disks, &out.Disks
		*out = make([]Disk, len(*in))
		copy(*out, *in)
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = make(map[string]resource.Quantity, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.UserData != nil {
		in, out := &in.UserData, &out.UserData
		*out = new(string)
		**out = **in
	}
	if in.VendorData != nil {
		in, out := &in.VendorData, &out.VendorData
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HardwareSpec.
func (in *HardwareSpec) DeepCopy() *HardwareSpec {
	if in == nil {
		return nil
	}
	out := new(HardwareSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HardwareStatus) DeepCopyInto(out *HardwareStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HardwareStatus.
func (in *HardwareStatus) DeepCopy() *HardwareStatus {
	if in == nil {
		return nil
	}
	out := new(HardwareStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IP) DeepCopyInto(out *IP) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IP.
func (in *IP) DeepCopy() *IP {
	if in == nil {
		return nil
	}
	out := new(IP)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IPXE) DeepCopyInto(out *IPXE) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IPXE.
func (in *IPXE) DeepCopy() *IPXE {
	if in == nil {
		return nil
	}
	out := new(IPXE)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Interface) DeepCopyInto(out *Interface) {
	*out = *in
	if in.Netboot != nil {
		in, out := &in.Netboot, &out.Netboot
		*out = new(Netboot)
		(*in).DeepCopyInto(*out)
	}
	if in.DHCP != nil {
		in, out := &in.DHCP, &out.DHCP
		*out = new(DHCP)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Interface.
func (in *Interface) DeepCopy() *Interface {
	if in == nil {
		return nil
	}
	out := new(Interface)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobStatus) DeepCopyInto(out *JobStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobStatus.
func (in *JobStatus) DeepCopy() *JobStatus {
	if in == nil {
		return nil
	}
	out := new(JobStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataCustom) DeepCopyInto(out *MetadataCustom) {
	*out = *in
	if in.PreinstalledOperatingSystemVersion != nil {
		in, out := &in.PreinstalledOperatingSystemVersion, &out.PreinstalledOperatingSystemVersion
		*out = new(MetadataInstanceOperatingSystem)
		**out = **in
	}
	if in.PrivateSubnets != nil {
		in, out := &in.PrivateSubnets, &out.PrivateSubnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataCustom.
func (in *MetadataCustom) DeepCopy() *MetadataCustom {
	if in == nil {
		return nil
	}
	out := new(MetadataCustom)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataFacility) DeepCopyInto(out *MetadataFacility) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataFacility.
func (in *MetadataFacility) DeepCopy() *MetadataFacility {
	if in == nil {
		return nil
	}
	out := new(MetadataFacility)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstance) DeepCopyInto(out *MetadataInstance) {
	*out = *in
	if in.OperatingSystem != nil {
		in, out := &in.OperatingSystem, &out.OperatingSystem
		*out = new(MetadataInstanceOperatingSystem)
		**out = **in
	}
	if in.Ips != nil {
		in, out := &in.Ips, &out.Ips
		*out = make([]*MetadataInstanceIP, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MetadataInstanceIP)
				**out = **in
			}
		}
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Storage != nil {
		in, out := &in.Storage, &out.Storage
		*out = new(MetadataInstanceStorage)
		(*in).DeepCopyInto(*out)
	}
	if in.SSHKeys != nil {
		in, out := &in.SSHKeys, &out.SSHKeys
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstance.
func (in *MetadataInstance) DeepCopy() *MetadataInstance {
	if in == nil {
		return nil
	}
	out := new(MetadataInstance)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceIP) DeepCopyInto(out *MetadataInstanceIP) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceIP.
func (in *MetadataInstanceIP) DeepCopy() *MetadataInstanceIP {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceIP)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceOperatingSystem) DeepCopyInto(out *MetadataInstanceOperatingSystem) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceOperatingSystem.
func (in *MetadataInstanceOperatingSystem) DeepCopy() *MetadataInstanceOperatingSystem {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceOperatingSystem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorage) DeepCopyInto(out *MetadataInstanceStorage) {
	*out = *in
	if in.Disks != nil {
		in, out := &in.Disks, &out.Disks
		*out = make([]*MetadataInstanceStorageDisk, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MetadataInstanceStorageDisk)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	if in.Raid != nil {
		in, out := &in.Raid, &out.Raid
		*out = make([]*MetadataInstanceStorageRAID, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MetadataInstanceStorageRAID)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	if in.Filesystems != nil {
		in, out := &in.Filesystems, &out.Filesystems
		*out = make([]*MetadataInstanceStorageFilesystem, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MetadataInstanceStorageFilesystem)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorage.
func (in *MetadataInstanceStorage) DeepCopy() *MetadataInstanceStorage {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageDisk) DeepCopyInto(out *MetadataInstanceStorageDisk) {
	*out = *in
	if in.Partitions != nil {
		in, out := &in.Partitions, &out.Partitions
		*out = make([]*MetadataInstanceStorageDiskPartition, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MetadataInstanceStorageDiskPartition)
				**out = **in
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageDisk.
func (in *MetadataInstanceStorageDisk) DeepCopy() *MetadataInstanceStorageDisk {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageDisk)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageDiskPartition) DeepCopyInto(out *MetadataInstanceStorageDiskPartition) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageDiskPartition.
func (in *MetadataInstanceStorageDiskPartition) DeepCopy() *MetadataInstanceStorageDiskPartition {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageDiskPartition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageFile) DeepCopyInto(out *MetadataInstanceStorageFile) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageFile.
func (in *MetadataInstanceStorageFile) DeepCopy() *MetadataInstanceStorageFile {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageFile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageFilesystem) DeepCopyInto(out *MetadataInstanceStorageFilesystem) {
	*out = *in
	if in.Mount != nil {
		in, out := &in.Mount, &out.Mount
		*out = new(MetadataInstanceStorageMount)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageFilesystem.
func (in *MetadataInstanceStorageFilesystem) DeepCopy() *MetadataInstanceStorageFilesystem {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageFilesystem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageMount) DeepCopyInto(out *MetadataInstanceStorageMount) {
	*out = *in
	if in.Files != nil {
		in, out := &in.Files, &out.Files
		*out = make([]*MetadataInstanceStorageFile, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MetadataInstanceStorageFile)
				**out = **in
			}
		}
	}
	if in.Create != nil {
		in, out := &in.Create, &out.Create
		*out = new(MetadataInstanceStorageMountFilesystemOptions)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageMount.
func (in *MetadataInstanceStorageMount) DeepCopy() *MetadataInstanceStorageMount {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageMount)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageMountFilesystemOptions) DeepCopyInto(out *MetadataInstanceStorageMountFilesystemOptions) {
	*out = *in
	if in.Options != nil {
		in, out := &in.Options, &out.Options
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageMountFilesystemOptions.
func (in *MetadataInstanceStorageMountFilesystemOptions) DeepCopy() *MetadataInstanceStorageMountFilesystemOptions {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageMountFilesystemOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataInstanceStorageRAID) DeepCopyInto(out *MetadataInstanceStorageRAID) {
	*out = *in
	if in.Devices != nil {
		in, out := &in.Devices, &out.Devices
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataInstanceStorageRAID.
func (in *MetadataInstanceStorageRAID) DeepCopy() *MetadataInstanceStorageRAID {
	if in == nil {
		return nil
	}
	out := new(MetadataInstanceStorageRAID)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MetadataManufacturer) DeepCopyInto(out *MetadataManufacturer) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MetadataManufacturer.
func (in *MetadataManufacturer) DeepCopy() *MetadataManufacturer {
	if in == nil {
		return nil
	}
	out := new(MetadataManufacturer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Netboot) DeepCopyInto(out *Netboot) {
	*out = *in
	if in.AllowPXE != nil {
		in, out := &in.AllowPXE, &out.AllowPXE
		*out = new(bool)
		**out = **in
	}
	if in.AllowWorkflow != nil {
		in, out := &in.AllowWorkflow, &out.AllowWorkflow
		*out = new(bool)
		**out = **in
	}
	if in.IPXE != nil {
		in, out := &in.IPXE, &out.IPXE
		*out = new(IPXE)
		**out = **in
	}
	if in.OSIE != nil {
		in, out := &in.OSIE, &out.OSIE
		*out = new(OSIE)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Netboot.
func (in *Netboot) DeepCopy() *Netboot {
	if in == nil {
		return nil
	}
	out := new(Netboot)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OSIE) DeepCopyInto(out *OSIE) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OSIE.
func (in *OSIE) DeepCopy() *OSIE {
	if in == nil {
		return nil
	}
	out := new(OSIE)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Reference) DeepCopyInto(out *Reference) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Reference.
func (in *Reference) DeepCopy() *Reference {
	if in == nil {
		return nil
	}
	out := new(Reference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Task) DeepCopyInto(out *Task) {
	*out = *in
	if in.Actions != nil {
		in, out := &in.Actions, &out.Actions
		*out = make([]Action, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Environment != nil {
		in, out := &in.Environment, &out.Environment
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Task.
func (in *Task) DeepCopy() *Task {
	if in == nil {
		return nil
	}
	out := new(Task)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Template) DeepCopyInto(out *Template) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Template.
func (in *Template) DeepCopy() *Template {
	if in == nil {
		return nil
	}
	out := new(Template)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Template) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TemplateConfig) DeepCopyInto(out *TemplateConfig) {
	*out = *in
	if in.KVs != nil {
		in, out := &in.KVs, &out.KVs
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TemplateConfig.
func (in *TemplateConfig) DeepCopy() *TemplateConfig {
	if in == nil {
		return nil
	}
	out := new(TemplateConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TemplateList) DeepCopyInto(out *TemplateList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Template, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TemplateList.
func (in *TemplateList) DeepCopy() *TemplateList {
	if in == nil {
		return nil
	}
	out := new(TemplateList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TemplateList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TemplateSpec) DeepCopyInto(out *TemplateSpec) {
	*out = *in
	if in.Data != nil {
		in, out := &in.Data, &out.Data
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TemplateSpec.
func (in *TemplateSpec) DeepCopy() *TemplateSpec {
	if in == nil {
		return nil
	}
	out := new(TemplateSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TemplateStatus) DeepCopyInto(out *TemplateStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TemplateStatus.
func (in *TemplateStatus) DeepCopy() *TemplateStatus {
	if in == nil {
		return nil
	}
	out := new(TemplateStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Workflow) DeepCopyInto(out *Workflow) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Workflow.
func (in *Workflow) DeepCopy() *Workflow {
	if in == nil {
		return nil
	}
	out := new(Workflow)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Workflow) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowCondition) DeepCopyInto(out *WorkflowCondition) {
	*out = *in
	if in.Time != nil {
		in, out := &in.Time, &out.Time
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowCondition.
func (in *WorkflowCondition) DeepCopy() *WorkflowCondition {
	if in == nil {
		return nil
	}
	out := new(WorkflowCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowList) DeepCopyInto(out *WorkflowList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Workflow, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowList.
func (in *WorkflowList) DeepCopy() *WorkflowList {
	if in == nil {
		return nil
	}
	out := new(WorkflowList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WorkflowList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowRuleSet) DeepCopyInto(out *WorkflowRuleSet) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowRuleSet.
func (in *WorkflowRuleSet) DeepCopy() *WorkflowRuleSet {
	if in == nil {
		return nil
	}
	out := new(WorkflowRuleSet)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WorkflowRuleSet) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowRuleSetList) DeepCopyInto(out *WorkflowRuleSetList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]WorkflowRuleSet, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowRuleSetList.
func (in *WorkflowRuleSetList) DeepCopy() *WorkflowRuleSetList {
	if in == nil {
		return nil
	}
	out := new(WorkflowRuleSetList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WorkflowRuleSetList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowRuleSetSpec) DeepCopyInto(out *WorkflowRuleSetSpec) {
	*out = *in
	if in.Rules != nil {
		in, out := &in.Rules, &out.Rules
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.Workflow.DeepCopyInto(&out.Workflow)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowRuleSetSpec.
func (in *WorkflowRuleSetSpec) DeepCopy() *WorkflowRuleSetSpec {
	if in == nil {
		return nil
	}
	out := new(WorkflowRuleSetSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowRuleSetStatus) DeepCopyInto(out *WorkflowRuleSetStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowRuleSetStatus.
func (in *WorkflowRuleSetStatus) DeepCopy() *WorkflowRuleSetStatus {
	if in == nil {
		return nil
	}
	out := new(WorkflowRuleSetStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowRuleSetWorkflow) DeepCopyInto(out *WorkflowRuleSetWorkflow) {
	*out = *in
	if in.Disabled != nil {
		in, out := &in.Disabled, &out.Disabled
		*out = new(bool)
		**out = **in
	}
	in.Template.DeepCopyInto(&out.Template)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowRuleSetWorkflow.
func (in *WorkflowRuleSetWorkflow) DeepCopy() *WorkflowRuleSetWorkflow {
	if in == nil {
		return nil
	}
	out := new(WorkflowRuleSetWorkflow)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowSpec) DeepCopyInto(out *WorkflowSpec) {
	*out = *in
	if in.Disabled != nil {
		in, out := &in.Disabled, &out.Disabled
		*out = new(bool)
		**out = **in
	}
	if in.HardwareMap != nil {
		in, out := &in.HardwareMap, &out.HardwareMap
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	in.BootOptions.DeepCopyInto(&out.BootOptions)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowSpec.
func (in *WorkflowSpec) DeepCopy() *WorkflowSpec {
	if in == nil {
		return nil
	}
	out := new(WorkflowSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowStatus) DeepCopyInto(out *WorkflowStatus) {
	*out = *in
	in.BootOptions.DeepCopyInto(&out.BootOptions)
	if in.GlobalExecutionStop != nil {
		in, out := &in.GlobalExecutionStop, &out.GlobalExecutionStop
		*out = (*in).DeepCopy()
	}
	if in.CurrentState != nil {
		in, out := &in.CurrentState, &out.CurrentState
		*out = new(CurrentState)
		**out = **in
	}
	if in.Tasks != nil {
		in, out := &in.Tasks, &out.Tasks
		*out = make([]Task, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]WorkflowCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WorkflowStatus.
func (in *WorkflowStatus) DeepCopy() *WorkflowStatus {
	if in == nil {
		return nil
	}
	out := new(WorkflowStatus)
	in.DeepCopyInto(out)
	return out
}
