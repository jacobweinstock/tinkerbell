package transform

import (
	"strings"
	"testing"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ptr[T any](v T) *T { return &v }

func TestHardware_minimal(t *testing.T) {
	src := &v1.Hardware{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1", Namespace: "default"},
		Spec: v1.HardwareSpec{
			AgentID: "agent-1",
			Interfaces: []v1.Interface{{
				DHCP: &v1.DHCP{MAC: "AA:BB:CC:DD:EE:FF"},
			}},
		},
	}
	got, err := Hardware(src)
	if err != nil {
		t.Fatalf("Hardware: %v", err)
	}
	if got.Spec.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", got.Spec.AgentID, "agent-1")
	}
	if _, ok := got.Spec.NetworkInterfaces["aa:bb:cc:dd:ee:ff"]; !ok {
		t.Errorf("NetworkInterfaces: missing lowercased MAC key, got keys: %v", keysOf(got.Spec.NetworkInterfaces))
	}
}

func TestHardware_fullTransform(t *testing.T) {
	src := &v1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "n1",
			Namespace:   "ns",
			Labels:      map[string]string{"app": "tink"},
			Annotations: map[string]string{"foo": "bar"},
		},
		Spec: v1.HardwareSpec{
			AgentID: "agent-1",
			Auto:    v1.AutoCapabilities{EnrollmentEnabled: true},
			BMCRef:  &corev1.TypedLocalObjectReference{Name: "bmc-1"},
			Interfaces: []v1.Interface{{
				DisableDHCP: false,
				DHCP: &v1.DHCP{
					MAC:         "52:54:00:01:02:03",
					Hostname:    "node-1",
					DomainName:  "example.com",
					LeaseTime:   3600,
					NameServers: []string{"1.1.1.1", "8.8.8.8"},
					TimeServers: []string{"time.example.com"},
					Arch:        "x86_64",
					UEFI:        true,
					IP: &v1.IP{
						Address: "192.168.1.10",
						Netmask: "255.255.255.0",
						Gateway: "192.168.1.1",
						Family:  4,
					},
					BootFileName:   "ipxe.efi",
					TFTPServerName: "192.168.1.5",
				},
				Netboot: &v1.Netboot{
					AllowPXE: ptr(true),
					IPXE:     &v1.IPXE{URL: "http://x/", Contents: "#!ipxe\n", Binary: "ipxe.efi"},
					OSIE: &v1.OSIE{
						BaseURL: "http://osie/",
						Kernel:  "vmlinuz",
						Initrd:  "initrd.img",
					},
				},
			}},
			References: map[string]v1.Reference{
				"lvm": {Group: "g", Version: "v", Resource: "r", Name: "lvm-cfg", Namespace: "ns"},
			},
			Disks:      []v1.Disk{{Device: "/dev/sda"}},
			UserData:   ptr("#cloud-config\n"),
			VendorData: ptr("vendor\n"),
		},
	}
	got, err := Hardware(src)
	if err != nil {
		t.Fatalf("Hardware: %v", err)
	}

	want := &v2.Hardware{
		TypeMeta: metav1.TypeMeta{APIVersion: "tinkerbell.org/v1alpha2", Kind: "Hardware"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "n1",
			Namespace:   "ns",
			Labels:      map[string]string{"app": "tink"},
			Annotations: map[string]string{"foo": "bar"},
		},
		Spec: v2.HardwareSpec{
			AgentID:    "agent-1",
			Auto:       v2.AutoCapabilities{EnrollmentEnabled: true},
			Attributes: &v2.Attributes{Arch: "x86_64", UEFI: true},
			NetworkInterfaces: v2.NetworkInterfaces{
				"52:54:00:01:02:03": v2.NetworkInterface{
					DHCP: &v2.DHCP{IPv4: &v2.DHCPv4{
						BootFileName:     "ipxe.efi",
						TFTPServerName:   "192.168.1.5",
						DomainName:       "example.com",
						Hostname:         ptr("node-1"),
						LeaseTimeSeconds: ptr(int64(3600)),
						Nameservers:      []v2.Nameserver{"1.1.1.1", "8.8.8.8"},
						NTPServers:       []v2.Timeserver{"time.example.com"},
					}},
					IPAM: &v2.IPAM{IPv4: &v2.IP{
						Address: "192.168.1.10",
						Gateway: "192.168.1.1",
						Prefix:  "24",
					}},
					Netboot: &v2.Netboot{
						IPXE: &v2.IPXE{Binary: "ipxe.efi", URL: "http://x/", Script: "#!ipxe\n"},
					},
				},
			},
			References: &v2.References{
				Additional: map[string]v2.Reference{
					"lvm": {Group: "g", Version: "v", Resource: "r", Name: "lvm-cfg", Namespace: "ns"},
				},
				Builtin: v2.BuiltinReferences{BMC: v2.SimpleReference{Name: "bmc-1", Namespace: "ns"}},
			},
			Instance: &v2.Instance{
				Userdata:   ptr("#cloud-config\n"),
				Vendordata: ptr("vendor\n"),
				OSIE:       &v2.OSIE{KernelURL: "http://osie/vmlinuz", InitrdURL: "http://osie/initrd.img"},
			},
			StorageDevices: []v2.StorageDevice{{Name: "/dev/sda"}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Hardware mismatch (-want +got):\n%s", diff)
	}
}

func TestHardware_disableDHCP(t *testing.T) {
	src := &v1.Hardware{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: "ns"},
		Spec: v1.HardwareSpec{
			Interfaces: []v1.Interface{{
				DisableDHCP: true,
				DHCP:        &v1.DHCP{MAC: "aa:bb:cc:dd:ee:01"},
			}},
		},
	}
	got, err := Hardware(src)
	if err != nil {
		t.Fatalf("Hardware: %v", err)
	}
	ni := got.Spec.NetworkInterfaces["aa:bb:cc:dd:ee:01"]
	if ni.DHCP == nil || ni.DHCP.IPv4 == nil || !ni.DHCP.IPv4.Disabled {
		t.Errorf("expected DHCPv4.Disabled=true, got %+v", ni.DHCP)
	}
}

func TestHardware_missingMAC(t *testing.T) {
	src := &v1.Hardware{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: "ns"},
		Spec: v1.HardwareSpec{
			Interfaces: []v1.Interface{{DHCP: &v1.DHCP{}}},
		},
	}
	if _, err := Hardware(src); err == nil || !strings.Contains(err.Error(), "MAC is required") {
		t.Errorf("expected MAC-required error, got %v", err)
	}
}

func TestHardware_nil(t *testing.T) {
	if _, err := Hardware(nil); err == nil {
		t.Error("expected error on nil source")
	}
}

func keysOf(m v2.NetworkInterfaces) []v2.MAC {
	out := make([]v2.MAC, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
