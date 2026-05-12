package transform

import (
	"testing"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"
	v2bmc "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell/bmc"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBMCMachine(t *testing.T) {
	now := metav1.Now()
	src := &v1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "bmc1", Namespace: "ns"},
		Spec: v1.MachineSpec{
			Connection: v1.Connection{
				Host:          "10.0.0.10",
				Port:          623,
				AuthSecretRef: corev1.SecretReference{Name: "creds", Namespace: "ns"},
				InsecureTLS:   true,
				ProviderOptions: &v1.ProviderOptions{
					PreferredOrder: []v1.ProviderName{"ipmitool", "redfish"},
					Redfish:        &v1.RedfishOptions{Port: 443, UseBasicAuth: true},
				},
			},
		},
		Status: v1.MachineStatus{
			Power: v1.On,
			Conditions: []v1.MachineCondition{
				{Type: v1.Contactable, Status: v1.ConditionTrue, LastUpdateTime: now, Message: "ok"},
			},
		},
	}
	got, err := BMCMachine(src)
	if err != nil {
		t.Fatalf("BMCMachine: %v", err)
	}
	want := &v2.BMC{
		TypeMeta:   metav1.TypeMeta{APIVersion: "tinkerbell.org/v1alpha2", Kind: "BMC"},
		ObjectMeta: metav1.ObjectMeta{Name: "bmc1", Namespace: "ns"},
		Spec: v2.BMCSpec{
			Connection: v2bmc.Connection{
				Host:        "10.0.0.10",
				InsecureTLS: true,
				AuthRef:     v2bmc.SimpleReference{Name: "creds", Namespace: "ns"},
				ProviderOptions: &v2bmc.ProviderOptions{
					PreferredOrder: []v2bmc.ProviderName{"ipmitool", "redfish"},
					Redfish:        &v2bmc.RedfishOptions{Port: 443, UseBasicAuth: true},
				},
			},
		},
		Status: v2.BMCStatus{
			Conditions: []v2bmc.Condition{
				{Type: "Contactable", Status: "True", LastUpdateTime: now, Message: "ok"},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("BMCMachine mismatch (-want +got):\n%s", diff)
	}
}
