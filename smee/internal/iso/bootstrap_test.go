package iso

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
	"github.com/tinkerbell/tinkerbell/smee/internal/bootstrap"
)

const (
	testISOSize  = 64 * 1024
	testSlotOff  = 16 * 1024
	testSlotCap  = 8 * 1024
	testMAC      = "de:ed:be:ef:fe:ed"
	testISOName  = "captainos.iso"
	unusedMagic  = "this-magic-string-is-not-in-the-test-iso"
	testISOPath  = "/iso/" + testMAC + "/" + testISOName
	sourceISOURL = "/" + testISOName
)

// newSourceISO writes a synthetic ISO, and optionally its layout metadata, and
// serves them with range support.
func newSourceISO(t *testing.T, capacity int64, withLayout bool) (srcURL string, src []byte) {
	t.Helper()
	dir := t.TempDir()

	src = make([]byte, testISOSize)
	for i := range src {
		src[i] = byte(i%251) + 1 // never zero, so slot padding is distinguishable
	}
	if err := os.WriteFile(filepath.Join(dir, testISOName), src, 0o600); err != nil {
		t.Fatal(err)
	}

	if withLayout {
		l := bootstrap.Layout{Version: bootstrap.LayoutVersion}
		l.ISO.Size = testISOSize
		l.Slot.Path = "/boot/bootstrap.cpio"
		l.Slot.Offset = testSlotOff
		l.Slot.Capacity = capacity
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, testISOName+bootstrap.LayoutSuffix), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)

	return srv.URL + sourceISOURL, src
}

// bootstrapBackend carries the user defined files under test, separate from
// mockBackend so the kernel cmdline fixtures stay untouched.
type bootstrapBackend struct{}

func (bootstrapBackend) FilterHardware(_ context.Context, opts data.HardwareFilter) (*tinkerbell.Hardware, error) {
	if opts.ByMACAddress == "" {
		return nil, fmt.Errorf("hardware not found")
	}
	return &tinkerbell.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(testCreationTime),
		},
		Spec: tinkerbell.HardwareSpec{
			Interfaces: []tinkerbell.Interface{{
				DHCP:    &tinkerbell.DHCP{MAC: opts.ByMACAddress, VLANID: "400"},
				Netboot: &tinkerbell.Netboot{},
			}},
			Metadata: &tinkerbell.HardwareMetadata{
				Facility:  &tinkerbell.MetadataFacility{FacilityCode: "test"},
				OSIEFiles: testBootstrapFiles,
			},
		},
	}, nil
}

var testBootstrapFiles = []tinkerbell.OSIEFile{
	{Path: "network.json", Contents: `{"version":1,"hostname":"test-machine"}`},
	{Path: "/etc/motd", Contents: "welcome\n"},
}

// testCreationTime stands in for the Hardware's creation timestamp, which is
// what the delivered files are stamped with.
var testCreationTime = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func newBootstrapHandler(t *testing.T, srcURL string) http.HandlerFunc {
	t.Helper()
	h := &Handler{
		Logger:  logr.Discard(),
		Backend: bootstrapBackend{},
		Patch: Patch{
			MagicString: unusedMagic,
			SourceISO:   srcURL,
		},
	}
	hf, err := h.HandlerFunc()
	if err != nil {
		t.Fatal(err)
	}
	return hf
}

func serveISO(t *testing.T, hf http.HandlerFunc, rangeHeader string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, testISOPath, nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	w := httptest.NewRecorder()
	hf.ServeHTTP(w, req)
	return w.Result()
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// wantPersonalized returns the image the handler is expected to produce.
func wantPersonalized(t *testing.T, src []byte) ([]byte, []byte) {
	t.Helper()
	archive := mustArchive(t)
	want := bytes.Clone(src)
	copy(want[testSlotOff:], archive)
	clear(want[testSlotOff+int64(len(archive)) : testSlotOff+testSlotCap])
	return want, archive
}

func mustArchive(t *testing.T) []byte {
	t.Helper()
	files := make([]bootstrap.File, 0, len(testBootstrapFiles))
	for _, f := range testBootstrapFiles {
		files = append(files, bootstrap.File{
			Name: bootstrap.Dir + "/" + strings.TrimPrefix(f.Path, "/"),
			Data: []byte(f.Contents),
		})
	}
	a, err := bootstrap.Archive(files, testCreationTime)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestBootstrapSlotFullResponse(t *testing.T) {
	srcURL, src := newSourceISO(t, testSlotCap, true)
	hf := newBootstrapHandler(t, srcURL)

	resp := serveISO(t, hf, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	got := readBody(t, resp)

	want, archive := wantPersonalized(t, src)
	if len(got) != len(src) {
		t.Fatalf("personalized ISO is %d bytes, source is %d; the size must not change", len(got), len(src))
	}
	if !bytes.Equal(got[:testSlotOff], src[:testSlotOff]) {
		t.Error("bytes before the slot were modified")
	}
	if !bytes.Equal(got[testSlotOff+testSlotCap:], src[testSlotOff+testSlotCap:]) {
		t.Error("bytes after the slot were modified")
	}
	if !bytes.Equal(got[testSlotOff:testSlotOff+int64(len(archive))], archive) {
		t.Error("the generated archive was not written to the slot")
	}
	padding := got[testSlotOff+int64(len(archive)) : testSlotOff+testSlotCap]
	if !bytes.Equal(padding, make([]byte, len(padding))) {
		t.Error("the remainder of the slot is not zero padded")
	}
	if !bytes.Equal(got, want) {
		t.Error("personalized ISO does not match the expected image")
	}
}

func TestBootstrapSlotRangeRequests(t *testing.T) {
	srcURL, src := newSourceISO(t, testSlotCap, true)
	hf := newBootstrapHandler(t, srcURL)
	want, archive := wantPersonalized(t, src)
	archiveEnd := testSlotOff + int64(len(archive))

	tests := map[string]struct{ start, end int64 }{
		"entirely before the slot":        {0, testSlotOff - 1},
		"crossing the slot start":         {testSlotOff - 100, testSlotOff + 100},
		"entirely inside the archive":     {testSlotOff + 10, testSlotOff + 40},
		"crossing the archive end":        {archiveEnd - 10, archiveEnd + 10},
		"entirely inside the padding":     {archiveEnd + 16, archiveEnd + 64},
		"crossing the slot end":           {testSlotOff + testSlotCap - 50, testSlotOff + testSlotCap + 50},
		"crossing both slot boundaries":   {testSlotOff - 1, testSlotOff + testSlotCap},
		"entirely after the slot":         {testSlotOff + testSlotCap, testISOSize - 1},
		"single byte at the slot start":   {testSlotOff, testSlotOff},
		"single byte at the slot end":     {testSlotOff + testSlotCap - 1, testSlotOff + testSlotCap - 1},
		"open ended from inside the slot": {testSlotOff + 5, testISOSize - 1},
		"suffix range":                    {testISOSize - 512, testISOSize - 1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			resp := serveISO(t, hf, fmt.Sprintf("bytes=%d-%d", tt.start, tt.end))
			if resp.StatusCode != http.StatusPartialContent {
				t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusPartialContent)
			}
			got := readBody(t, resp)
			if diff := cmp.Diff(want[tt.start:tt.end+1], got); diff != "" {
				t.Fatalf("range [%d,%d] does not match the full personalized image (-want +got):\n%s", tt.start, tt.end, diff)
			}
		})
	}
}

// TestBootstrapSlotReassembly mirrors how a BMC consumes virtual media: many
// small, independent range requests that must add up to one coherent image.
func TestBootstrapSlotReassembly(t *testing.T) {
	srcURL, src := newSourceISO(t, testSlotCap, true)
	hf := newBootstrapHandler(t, srcURL)
	want, _ := wantPersonalized(t, src)

	const chunk = 1333 // deliberately not a power of two
	got := make([]byte, testISOSize)
	for start := int64(testISOSize - chunk); start > -chunk; start -= chunk { // served back to front
		s := max(start, 0)
		e := min(s+chunk, int64(testISOSize)) - 1
		resp := serveISO(t, hf, fmt.Sprintf("bytes=%d-%d", s, e))
		copy(got[s:], readBody(t, resp))
	}
	if !bytes.Equal(got, want) {
		t.Fatal("image reassembled from out-of-order ranges does not match the full response")
	}
}

func TestBootstrapSlotAbsentLayout(t *testing.T) {
	srcURL, src := newSourceISO(t, testSlotCap, false)
	hf := newBootstrapHandler(t, srcURL)

	resp := serveISO(t, hf, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !bytes.Equal(readBody(t, resp), src) {
		t.Fatal("an ISO with no published slot must be served unmodified")
	}
}

func TestBootstrapSlotOverCapacity(t *testing.T) {
	srcURL, _ := newSourceISO(t, 32, true)
	hf := newBootstrapHandler(t, srcURL)

	resp := serveISO(t, hf, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d; an oversized archive must fail before image bytes are served", resp.StatusCode, http.StatusInternalServerError)
	}
}

// TestBootstrapUserDefinedFiles confirms the slot carries exactly the files the
// Hardware object defines, under the shared base path.
func TestBootstrapUserDefinedFiles(t *testing.T) {
	srcURL, _ := newSourceISO(t, testSlotCap, true)
	hf := newBootstrapHandler(t, srcURL)

	got := readBody(t, serveISO(t, hf, ""))
	_, archive := wantPersonalized(t, got)
	if !bytes.Contains(got[testSlotOff:testSlotOff+testSlotCap], archive) {
		t.Fatal("the slot does not contain the expected archive")
	}
	for _, f := range testBootstrapFiles {
		name := bootstrap.Dir + "/" + strings.TrimPrefix(f.Path, "/")
		if !bytes.Contains(archive, []byte(name)) {
			t.Errorf("archive does not deliver %s", name)
		}
		if !bytes.Contains(archive, []byte(f.Contents)) {
			t.Errorf("archive does not carry the content of %s", name)
		}
	}
}
