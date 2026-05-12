package transform

import (
	"testing"

	v1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	v2 "github.com/tinkerbell/tinkerbell/api/v1alpha2/tinkerbell"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTemplate_singleTask(t *testing.T) {
	data := `version: "0.1"
name: provision
global_timeout: 6000
tasks:
- name: install
  worker: "{{.device_1}}"
  volumes:
    - /dev:/dev
  environment:
    DEBUG: "true"
  actions:
  - name: stream
    image: quay.io/tinkerbell/actions/image2disk:v1.0.0
    timeout: 90
    environment:
      IMG_URL: https://example.com/disk.img
    command:
      - /usr/bin/image2disk
      - --device
      - /dev/sda
`
	src := &v1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "ubuntu", Namespace: "tinkerbell"},
		Spec:       v1.TemplateSpec{Data: &data},
	}
	tasks, err := Template(src)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	timeout := int64(90)
	want := &v2.Task{
		TypeMeta: metav1.TypeMeta{APIVersion: "tinkerbell.org/v1alpha2", Kind: "Task"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ubuntu",
			Namespace: "tinkerbell",
		},
		Spec: v2.TaskSpec{
			Actions: []v2.Action{{
				Name:    "stream",
				Image:   "quay.io/tinkerbell/actions/image2disk:v1.0.0",
				Command: "/usr/bin/image2disk",
				Args:    []string{"--device", "/dev/sda"},
				EnvVars: []v2.EnvVar{{Key: "IMG_URL", Value: "https://example.com/disk.img"}},
				TimeoutSeconds: &timeout,
			}},
			EnvVars: []v2.EnvVar{{Key: "DEBUG", Value: "true"}},
			Volumes: []v2.Volume{"/dev:/dev"},
		},
	}
	if diff := cmp.Diff(want, tasks[0]); diff != "" {
		t.Errorf("Template mismatch (-want +got):\n%s", diff)
	}
}

func TestTemplate_multipleTasksFanOut(t *testing.T) {
	data := `version: "0.1"
name: t
tasks:
- name: first
  actions:
  - {name: a1, image: img1}
- name: second
  actions:
  - {name: a2, image: img2}
`
	src := &v1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "tpl", Namespace: "ns"},
		Spec:       v1.TemplateSpec{Data: &data},
	}
	tasks, err := Template(src)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if got, want := tasks[0].Name, "tpl-first"; got != want {
		t.Errorf("task[0].Name = %q, want %q", got, want)
	}
	if got, want := tasks[1].Name, "tpl-second"; got != want {
		t.Errorf("task[1].Name = %q, want %q", got, want)
	}
}

func TestTemplate_emptyData(t *testing.T) {
	src := &v1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "tpl", Namespace: "ns"},
	}
	if _, err := Template(src); err == nil {
		t.Error("expected error for empty spec.data")
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		wantCmd  string
		wantArgs []string
	}{
		{"absolute path", []string{"/bin/foo", "a", "b"}, "/bin/foo", []string{"a", "b"}},
		{"non-path", []string{"foo", "a"}, "", []string{"foo", "a"}},
		{"single absolute", []string{"/bin/foo"}, "/bin/foo", nil},
		{"empty", nil, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd, gotArgs := splitCommand(tc.in)
			if gotCmd != tc.wantCmd {
				t.Errorf("cmd = %q, want %q", gotCmd, tc.wantCmd)
			}
			if diff := cmp.Diff(tc.wantArgs, gotArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
