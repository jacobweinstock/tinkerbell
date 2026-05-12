package transform

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Hardware converts a v1alpha1 Hardware into a v1alpha2 Hardware.
//
// Field handling:
//
//   - AgentID, Auto: copied unchanged.
//   - BMCRef (*corev1.TypedLocalObjectReference) -> Spec.References.Builtin.BMC.
//   - Interfaces []Interface -> NetworkInterfaces map[MAC]NetworkInterface.
//     Each interface is keyed by its DHCP MAC (lowercased). Interfaces
//     without a MAC are an error.
//   - DHCP fields: Hostname/DomainName/LeaseTime/NameServers/TimeServers/
//     ClasslessStaticRoutes/TFTPServerName/BootFileName/VLANID and the IP
//     block are folded into NetworkInterface.DHCP.IPv4 and
//     NetworkInterface.IPAM.IPv4. DisableDHCP -> DHCPv4.Disabled.
//   - DHCP.Arch / DHCP.UEFI: hoisted to Spec.Attributes (first interface
//     with a non-empty value wins).
//   - Netboot.AllowPXE *bool: AllowPXE == false maps to Netboot.Disabled.
//   - Netboot.IPXE: URL/Contents/Binary -> URL/Script/Binary.
//   - Netboot.OSIE: BaseURL/Kernel/Initrd -> KernelURL/InitrdURL with
//     simple path joining when Kernel/Initrd are not already absolute
//     URLs.
//   - UserData / VendorData -> Spec.Instance.Userdata / Vendordata.
//   - References map -> Spec.References.Additional.
//   - Disks -> Spec.StorageDevices (Disk.Device -> StorageDevice.Name).
//   - HardwareMetadata.Instance.SSHKeys -> Spec.Instance.SSHKeys.
//
// Dropped (no v2 equivalent):
//
//   - Resources, TinkVersion, all HardwareMetadata fields except
//     Instance.SSHKeys, Interfaces[].Isoboot, Interfaces[].DHCP.IfaceName,
//     Netboot.AllowWorkflow.
//   - The v1 status subresource (HardwareStatus) is removed in v2.
func Hardware(src *v1.Hardware) (*v2.Hardware, error) {
	if src == nil {
		return nil, fmt.Errorf("Hardware: nil source")
	}

	out := &v2.Hardware{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v2.GroupVersion.String(),
			Kind:       "Hardware",
		},
		ObjectMeta: cleanObjectMeta(src.ObjectMeta),
		Spec: v2.HardwareSpec{
			AgentID: src.Spec.AgentID,
			Auto:    v2.AutoCapabilities{EnrollmentEnabled: src.Spec.Auto.EnrollmentEnabled},
		},
	}

	if len(src.Spec.Interfaces) > 0 {
		nis, attrs, err := convertInterfaces(src.Spec.Interfaces)
		if err != nil {
			return nil, fmt.Errorf("Hardware %s/%s: %w", src.Namespace, src.Name, err)
		}
		out.Spec.NetworkInterfaces = nis
		if attrs != nil {
			out.Spec.Attributes = attrs
		}
	}

	if refs := convertHardwareReferences(src.Spec.References, hardwareBMCRef(src)); refs != nil {
		out.Spec.References = refs
	}

	if inst := convertInstance(src); inst != nil {
		out.Spec.Instance = inst
	}

	if len(src.Spec.Disks) > 0 {
		out.Spec.StorageDevices = make([]v2.StorageDevice, 0, len(src.Spec.Disks))
		for _, d := range src.Spec.Disks {
			out.Spec.StorageDevices = append(out.Spec.StorageDevices, v2.StorageDevice{Name: d.Device})
		}
	}

	return out, nil
}

func convertInterfaces(in []v1.Interface) (v2.NetworkInterfaces, *v2.Attributes, error) {
	out := v2.NetworkInterfaces{}
	var attrs *v2.Attributes
	for i, iface := range in {
		if iface.DHCP == nil || iface.DHCP.MAC == "" {
			return nil, nil, fmt.Errorf("interface %d: DHCP.MAC is required for v1alpha2 NetworkInterfaces map key", i)
		}
		mac := v2.MAC(strings.ToLower(iface.DHCP.MAC))

		ni := v2.NetworkInterface{}
		ni.DHCP = &v2.DHCP{IPv4: convertDHCPv4(iface.DHCP, iface.DisableDHCP)}
		if ip := convertIPAM(iface.DHCP.IP); ip != nil {
			ni.IPAM = ip
		}
		if iface.Netboot != nil {
			ni.Netboot = convertNetboot(iface.Netboot)
		}

		if iface.DHCP.Arch != "" || iface.DHCP.UEFI {
			if attrs == nil {
				attrs = &v2.Attributes{}
			}
			if attrs.Arch == "" && iface.DHCP.Arch != "" {
				attrs.Arch = iface.DHCP.Arch
			}
			if iface.DHCP.UEFI {
				attrs.UEFI = true
			}
		}

		out[mac] = ni
	}
	return out, attrs, nil
}

func convertDHCPv4(src *v1.DHCP, disable bool) *v2.DHCPv4 {
	if src == nil {
		return nil
	}
	out := &v2.DHCPv4{
		Disabled:       disable,
		BootFileName:   src.BootFileName,
		TFTPServerName: src.TFTPServerName,
		DomainName:     src.DomainName,
	}
	if src.Hostname != "" {
		h := src.Hostname
		out.Hostname = &h
	}
	if src.LeaseTime > 0 {
		lt := src.LeaseTime
		out.LeaseTimeSeconds = &lt
	}
	if src.VLANID != "" {
		v := src.VLANID
		out.VLANID = &v
	}
	for _, ns := range src.NameServers {
		out.Nameservers = append(out.Nameservers, v2.Nameserver(ns))
	}
	for _, ts := range src.TimeServers {
		out.NTPServers = append(out.NTPServers, v2.Timeserver(ts))
	}
	for _, r := range src.ClasslessStaticRoutes {
		out.ClasslessStaticRoutes = append(out.ClasslessStaticRoutes, v2.ClasslessStaticRoute{
			DestinationDescriptor: r.DestinationDescriptor,
			Router:                r.Router,
		})
	}
	return out
}

func convertIPAM(in *v1.IP) *v2.IPAM {
	if in == nil || in.Address == "" {
		return nil
	}
	prefix := netmaskToPrefix(in.Netmask)
	ip := &v2.IP{
		Address: in.Address,
		Gateway: in.Gateway,
		Prefix:  prefix,
	}
	out := &v2.IPAM{}
	if in.Family == 6 {
		out.IPv6 = ip
	} else {
		out.IPv4 = ip
	}
	return out
}

// netmaskToPrefix converts a dotted-decimal IPv4 mask such as
// "255.255.255.0" into a decimal prefix length string ("24"). If the
// input is empty or unparseable the original value is returned so the
// information is not lost; v2 validation will then flag it.
func netmaskToPrefix(in string) string {
	if in == "" {
		return ""
	}
	ip := net.ParseIP(in)
	if ip == nil {
		return in
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return in
	}
	ones, bits := net.IPv4Mask(ip4[0], ip4[1], ip4[2], ip4[3]).Size()
	if bits == 0 {
		return in
	}
	return strconv.Itoa(ones)
}

func convertNetboot(src *v1.Netboot) *v2.Netboot {
	out := &v2.Netboot{}
	if src.AllowPXE != nil && !*src.AllowPXE {
		out.Disabled = true
	}
	if src.IPXE != nil {
		out.IPXE = &v2.IPXE{
			Binary: src.IPXE.Binary,
			URL:    src.IPXE.URL,
			Script: src.IPXE.Contents,
		}
	}
	// Note: v1.Netboot.OSIE has moved to Hardware.Spec.Instance.OSIE in
	// v2 and is lifted there separately by firstOSIE().
	return out
}

func convertOSIE(src *v1.OSIE) *v2.OSIE {
	out := &v2.OSIE{}
	out.KernelURL = joinURL(src.BaseURL, src.Kernel)
	out.InitrdURL = joinURL(src.BaseURL, src.Initrd)
	if out.KernelURL == "" && out.InitrdURL == "" {
		return nil
	}
	return out
}

func joinURL(base, child string) string {
	if child == "" {
		return ""
	}
	if base == "" {
		return child
	}
	// If child is already absolute, return as-is.
	if strings.Contains(child, "://") {
		return child
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(child, "/")
}

func convertHardwareReferences(in map[string]v1.Reference, bmcRef *v2.SimpleReference) *v2.References {
	out := &v2.References{}
	any := false
	if len(in) > 0 {
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(in))
		for k := range in {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		add := make(map[string]v2.Reference, len(in))
		for _, k := range keys {
			r := in[k]
			add[k] = v2.Reference{
				Group:     r.Group,
				Name:      r.Name,
				Namespace: r.Namespace,
				Resource:  r.Resource,
				Version:   r.Version,
			}
		}
		out.Additional = add
		any = true
	}
	if bmcRef != nil {
		out.Builtin = v2.BuiltinReferences{BMC: *bmcRef}
		any = true
	}
	if !any {
		return nil
	}
	return out
}

// hardwareBMCRef extracts the BMC reference from a v1 Hardware. v1
// uses a TypedLocalObjectReference which is namespace-implicit; the
// returned v2 SimpleReference inherits the Hardware's namespace.
func hardwareBMCRef(src *v1.Hardware) *v2.SimpleReference {
	if src.Spec.BMCRef == nil || src.Spec.BMCRef.Name == "" {
		return nil
	}
	return &v2.SimpleReference{Name: src.Spec.BMCRef.Name, Namespace: src.Namespace}
}

func convertInstance(src *v1.Hardware) *v2.Instance {
	out := &v2.Instance{}
	any := false
	if src.Spec.UserData != nil {
		out.Userdata = src.Spec.UserData
		any = true
	}
	if src.Spec.VendorData != nil {
		out.Vendordata = src.Spec.VendorData
		any = true
	}
	if src.Spec.Metadata != nil && src.Spec.Metadata.Instance != nil && len(src.Spec.Metadata.Instance.SSHKeys) > 0 {
		out.SSHKeys = append([]string(nil), src.Spec.Metadata.Instance.SSHKeys...)
		any = true
	}
	if osie := firstOSIE(src.Spec.Interfaces); osie != nil {
		out.OSIE = osie
		any = true
	}
	if !any {
		return nil
	}
	return out
}

// firstOSIE walks the v1 Interfaces and returns the first non-nil
// Netboot.OSIE converted to v2. v1 stored OSIE per-interface; v2 has a
// single Instance.OSIE.
func firstOSIE(in []v1.Interface) *v2.OSIE {
	for _, iface := range in {
		if iface.Netboot == nil || iface.Netboot.OSIE == nil {
			continue
		}
		return convertOSIE(iface.Netboot.OSIE)
	}
	return nil
}
