package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeCall captures one ServerSideApply invocation.
type fakeCall struct {
	gvr          schema.GroupVersionResource
	namespaced   bool
	namespace    string
	name         string
	fieldManager string
}

// fakeClient is an in-memory ClusterClient for tests.
type fakeClient struct {
	mu      sync.Mutex
	items   map[schema.GroupVersionResource][]*unstructured.Unstructured
	applied []fakeCall
	deleted []fakeCall
}

func (f *fakeClient) List(_ context.Context, gvr schema.GroupVersionResource, _ string, fn func(*unstructured.Unstructured) error) error {
	for _, it := range f.items[gvr] {
		if err := fn(it); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeClient) ServerSideApply(_ context.Context, gvr schema.GroupVersionResource, namespaced bool, obj *unstructured.Unstructured, fm string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, fakeCall{
		gvr: gvr, namespaced: namespaced,
		namespace: obj.GetNamespace(), name: obj.GetName(),
		fieldManager: fm,
	})
	return nil
}

func (f *fakeClient) Delete(_ context.Context, gvr schema.GroupVersionResource, namespaced bool, namespace, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, fakeCall{
		gvr: gvr, namespaced: namespaced,
		namespace: namespace, name: name,
	})
	return nil
}

// mustUnstructured builds an Unstructured from a Go value via JSON.
func mustUnstructured(t *testing.T, apiVersion, kind, namespace, name string, extra map[string]any) *unstructured.Unstructured {
	t.Helper()
	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
	for k, v := range extra {
		obj[k] = v
	}
	// Round-trip through JSON to normalize types.
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, &u.Object); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return u
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	layout := NewLayout(dir)
	if err := layout.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	s, err := LoadState(layout)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s.SetExport("hardware", PhaseDone)
	s.Count("hardware").Exported = 3
	if err := s.Save(layout); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadState(layout)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Phases.Export["hardware"] != PhaseDone {
		t.Fatalf("phase not persisted: %#v", got.Phases.Export)
	}
	if got.Count("hardware").Exported != 3 {
		t.Fatalf("count not persisted: %d", got.Count("hardware").Exported)
	}
}

func TestStateWorkdirMismatch(t *testing.T) {
	dir := t.TempDir()
	layout := NewLayout(dir)
	if err := layout.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	s, err := LoadState(layout)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.Save(layout); err != nil {
		t.Fatalf("save: %v", err)
	}
	other := NewLayout(t.TempDir())
	// Copy state.json to the other dir to simulate a moved/renamed
	// workdir.
	data, err := os.ReadFile(layout.StateFile())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := other.Init(); err != nil {
		t.Fatalf("init other: %v", err)
	}
	if err := os.WriteFile(other.StateFile(), data, 0o600); err != nil {
		t.Fatalf("write other: %v", err)
	}
	if _, err := LoadState(other); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestRunnerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	hardwareGVR := schema.GroupVersionResource{
		Group: "tinkerbell.org", Version: "v1alpha1", Resource: "hardware",
	}
	bmcJobGVR := schema.GroupVersionResource{
		Group: "bmc.tinkerbell.org", Version: "v1alpha1", Resource: "jobs",
	}
	fc := &fakeClient{items: map[schema.GroupVersionResource][]*unstructured.Unstructured{
		hardwareGVR: {
			mustUnstructured(t, "tinkerbell.org/v1alpha1", "Hardware", "ns1", "node-a", nil),
			mustUnstructured(t, "tinkerbell.org/v1alpha1", "Hardware", "ns1", "node-b", nil),
		},
		bmcJobGVR: {
			mustUnstructured(t, "bmc.tinkerbell.org/v1alpha1", "Job", "ns1", "job-a", nil),
		},
	}}
	r, err := New(Config{Workdir: dir, Client: fc})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	state, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Exports written.
	srcDir := r.Layout().SourceKindDir("hardware")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("readdir source: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 exported files, got %d", len(entries))
	}

	// Transform produced target files.
	tgtDir := r.Layout().TargetKindDir("hardware")
	entries, err = os.ReadDir(tgtDir)
	if err != nil {
		t.Fatalf("readdir target: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 transformed files, got %d", len(entries))
	}

	// Apply phase invoked once per target file.
	if len(fc.applied) != 2 {
		t.Fatalf("expected 2 applies, got %d (%+v)", len(fc.applied), fc.applied)
	}
	for _, a := range fc.applied {
		if a.fieldManager != fieldManager {
			t.Fatalf("wrong field manager: %q", a.fieldManager)
		}
		if a.gvr.Version != "v1alpha2" || a.gvr.Resource != "hardware" {
			t.Fatalf("wrong GVR: %#v", a.gvr)
		}
		if !a.namespaced || a.namespace != "ns1" {
			t.Fatalf("expected namespaced apply in ns1, got %+v", a)
		}
	}

	// State: every kind marked done.
	for _, k := range SourceKinds {
		if got := state.Phases.Export[k.Name]; got != PhaseDone {
			t.Errorf("export %s = %q, want done", k.Name, got)
		}
		if got := state.Phases.Transform[k.Name]; got != PhaseDone {
			t.Errorf("transform %s = %q, want done", k.Name, got)
		}
	}
	for _, target := range ApplyKinds() {
		if got := state.Phases.ApplyObjects[target]; got != PhaseDone {
			t.Errorf("apply_objects %s = %q, want done", target, got)
		}
	}
	if state.Phases.ApplyCRDsAdditive != PhaseDone || state.Phases.ApplyCRDsFinal != PhaseDone || state.Phases.DeleteOldCRDs != PhaseDone {
		t.Errorf("crd phase stubs not done: %#v", state.Phases)
	}

	// Archived bmcjob v1alpha1 CR was deleted from the cluster (archive
	// kinds are not applied, but their source CRs must be removed before
	// apply_crds_final drops v1alpha1 from the shared CRD).
	var deletedBMCJob bool
	for _, d := range fc.deleted {
		if d.gvr == bmcJobGVR && d.namespace == "ns1" && d.name == "job-a" {
			deletedBMCJob = true
			break
		}
	}
	if !deletedBMCJob {
		t.Fatalf("expected bmcjob v1alpha1 delete, got %+v", fc.deleted)
	}
	if got := state.Phases.DeleteArchivedObjects["bmcjob"]; got != PhaseDone {
		t.Errorf("delete_archived_objects bmcjob = %q, want done", got)
	}

	// Resume: second Run is a no-op.
	state2, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(fc.applied) != 2 {
		t.Fatalf("resume re-applied: %d", len(fc.applied))
	}
	if len(fc.deleted) != 1 {
		t.Fatalf("resume re-deleted: %d", len(fc.deleted))
	}
	_ = state2
}

func TestRunnerDryRunSkipsApply(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{items: map[schema.GroupVersionResource][]*unstructured.Unstructured{}}
	r, err := New(Config{Workdir: dir, Client: fc, DryRun: true})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(fc.applied) != 0 {
		t.Fatalf("dry-run applied %d objects", len(fc.applied))
	}
}

func TestParseObjectFilename(t *testing.T) {
	cases := []struct {
		in     string
		ns, nm string
		ok     bool
	}{
		{"ns1__node-a.yaml", "ns1", "node-a", true},
		{"__cluster-scoped.yaml", "", "cluster-scoped", true},
		{"no-separator.yaml", "", "", false},
		{"ns1__node-a.json", "", "", false},
	}
	for _, c := range cases {
		ns, nm, ok := parseObjectFilename(c.in)
		if ok != c.ok || ns != c.ns || nm != c.nm {
			t.Errorf("parseObjectFilename(%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.in, ns, nm, ok, c.ns, c.nm, c.ok)
		}
	}
}

// silence the metav1 unused import linter on minimal builds.
var _ = metav1.Time{}
var _ = filepath.Separator

// fakeCRDInstaller records call order for assertions.
type fakeCRDInstaller struct {
	calls    []string
	failNext error
}

func (f *fakeCRDInstaller) ApplyAdditive(context.Context) error {
	f.calls = append(f.calls, "additive")
	return f.takeErr()
}
func (f *fakeCRDInstaller) DeleteOld(context.Context) error {
	f.calls = append(f.calls, "delete_old")
	return f.takeErr()
}
func (f *fakeCRDInstaller) ApplyFinal(context.Context) error {
	f.calls = append(f.calls, "final")
	return f.takeErr()
}
func (f *fakeCRDInstaller) takeErr() error {
	err := f.failNext
	f.failNext = nil
	return err
}

func TestRunnerInvokesCRDPhasesInOrder(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{items: map[schema.GroupVersionResource][]*unstructured.Unstructured{}}
	fi := &fakeCRDInstaller{}
	r, err := New(Config{Workdir: dir, Client: fc, CRDInstaller: fi})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	state, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{"additive", "delete_old", "final"}
	if len(fi.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", fi.calls, want)
	}
	for i, c := range want {
		if fi.calls[i] != c {
			t.Errorf("call %d = %q, want %q", i, fi.calls[i], c)
		}
	}
	if state.Phases.ApplyCRDsAdditive != PhaseDone ||
		state.Phases.DeleteOldCRDs != PhaseDone ||
		state.Phases.ApplyCRDsFinal != PhaseDone {
		t.Errorf("crd phases not done: %#v", state.Phases)
	}

	// Resume: CRD phases are not re-invoked.
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(fi.calls) != len(want) {
		t.Fatalf("resume re-invoked CRDs: %v", fi.calls)
	}
}

func TestRunnerCRDPhaseFailureIsRecorded(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{items: map[schema.GroupVersionResource][]*unstructured.Unstructured{}}
	fi := &fakeCRDInstaller{failNext: context.Canceled}
	r, err := New(Config{Workdir: dir, Client: fc, CRDInstaller: fi})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	state, err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error from failing additive phase")
	}
	if state.Phases.ApplyCRDsAdditive != PhaseFailed {
		t.Errorf("expected additive=failed, got %q", state.Phases.ApplyCRDsAdditive)
	}
	// later phases never ran
	if state.Phases.DeleteOldCRDs == PhaseDone || state.Phases.ApplyCRDsFinal == PhaseDone {
		t.Errorf("later phases ran after failure: %#v", state.Phases)
	}
}
