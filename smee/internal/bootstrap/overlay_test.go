package bootstrap

import (
	"bytes"
	"math/rand"
	"net/http"
	"testing"
)

// buildISO returns a fake source ISO and the personalized image the overlay is
// expected to produce.
func buildISO(t *testing.T, size int, o *Overlay) (src, want []byte) {
	t.Helper()
	src = make([]byte, size)
	for i := range src {
		src[i] = byte(i%251) + 1 // never zero, so slot padding is distinguishable
	}
	want = bytes.Clone(src)
	copy(want[o.Offset:], o.Archive)
	clear(want[o.Offset+int64(len(o.Archive)) : o.Offset+o.Capacity])
	return src, want
}

func TestOverlayApply(t *testing.T) {
	const isoSize = 4096
	o := &Overlay{Offset: 1024, Capacity: 512, Archive: bytes.Repeat([]byte{0xAA}, 100)}
	src, want := buildISO(t, isoSize, o)

	tests := map[string]struct{ start, length int64 }{
		"full image":                  {0, isoSize},
		"entirely before the slot":    {0, 1024},
		"entirely after the slot":     {1536, 512},
		"entirely inside the archive": {1030, 20},
		"entirely inside the padding": {1200, 64},
		"crossing the slot start":     {1000, 100},
		"crossing the archive end":    {1100, 100},
		"crossing the slot end":       {1500, 100},
		"crossing both boundaries":    {1000, 600},
		"single byte at slot start":   {1024, 1},
		"single byte at slot end":     {1535, 1},
		"single byte before the slot": {1023, 1},
		"single byte after the slot":  {1536, 1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			buf := bytes.Clone(src[tt.start : tt.start+tt.length])
			o.Apply(buf, tt.start)
			if !bytes.Equal(buf, want[tt.start:tt.start+tt.length]) {
				t.Fatalf("overlay produced wrong bytes for [%d,%d)", tt.start, tt.start+tt.length)
			}
		})
	}
}

// TestOverlayChunkBoundaries proves the result does not depend on how the
// stream is split, which is what read-buffer and range boundaries amount to.
func TestOverlayChunkBoundaries(t *testing.T) {
	const isoSize = 8192
	o := &Overlay{Offset: 2048, Capacity: 2048, Archive: bytes.Repeat([]byte{0x5A}, 777)}
	src, want := buildISO(t, isoSize, o)

	rnd := rand.New(rand.NewSource(1))
	for range 200 {
		var got []byte
		for off := int64(0); off < isoSize; {
			n := int64(rnd.Intn(300) + 1)
			n = min(n, isoSize-off)
			chunk := bytes.Clone(src[off : off+n])
			o.Apply(chunk, off)
			got = append(got, chunk...)
			off += n
		}
		if !bytes.Equal(got, want) {
			t.Fatal("image reassembled from random chunk sizes does not match the full response")
		}
	}
}

// TestOverlayOutOfOrderRanges reconstructs the image from ranges served in a
// random order, as a BMC does.
func TestOverlayOutOfOrderRanges(t *testing.T) {
	const isoSize = 8192
	const rangeSize = int64(64)
	o := &Overlay{Offset: 3000, Capacity: 1000, Archive: bytes.Repeat([]byte{0x11}, 333)}
	src, want := buildISO(t, isoSize, o)

	starts := make([]int64, 0, isoSize/rangeSize)
	for off := int64(0); off < isoSize; off += rangeSize {
		starts = append(starts, off)
	}
	rnd := rand.New(rand.NewSource(2))
	rnd.Shuffle(len(starts), func(i, j int) { starts[i], starts[j] = starts[j], starts[i] })

	got := make([]byte, isoSize)
	for _, off := range starts {
		end := min(off+rangeSize, int64(isoSize))
		chunk := bytes.Clone(src[off:end])
		o.Apply(chunk, off)
		copy(got[off:], chunk)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("image reassembled from out-of-order ranges does not match the full response")
	}
}

func TestOverlayPrepareResponse(t *testing.T) {
	tests := map[string]struct {
		status      int
		contentRng  string
		wantStart   int64
		wantEnabled bool
		wantErr     bool
	}{
		"full response":              {http.StatusOK, "", 0, true, false},
		"range response":             {http.StatusPartialContent, "bytes 1024-2047/8192", 1024, true, false},
		"suffix range response":      {http.StatusPartialContent, "bytes 8000-8191/8192", 8000, true, false},
		"open ended range response":  {http.StatusPartialContent, "bytes 5-8191/8192", 5, true, false},
		"unknown total size":         {http.StatusPartialContent, "bytes 10-20/*", 10, true, false},
		"multipart has no range":     {http.StatusPartialContent, "", 0, false, true},
		"unsatisfiable range":        {http.StatusRequestedRangeNotSatisfiable, "bytes */8192", 0, false, false},
		"malformed range":            {http.StatusPartialContent, "bytes abc/8192", 0, false, true},
		"non byte range unit":        {http.StatusPartialContent, "items 0-1/2", 0, false, true},
		"not modified is not a body": {http.StatusNotModified, "", 0, false, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			o := &Overlay{Offset: 1, Capacity: 1}
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
			if tt.contentRng != "" {
				resp.Header.Set("Content-Range", tt.contentRng)
			}
			err := o.PrepareResponse(resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got error %v, wantErr %v", err, tt.wantErr)
			}
			if o.Enabled != tt.wantEnabled {
				t.Fatalf("got enabled %v, want %v", o.Enabled, tt.wantEnabled)
			}
			if o.Enabled && o.Start != tt.wantStart {
				t.Fatalf("got start %d, want %d", o.Start, tt.wantStart)
			}
		})
	}
}

func TestOverlayNilIsANoop(t *testing.T) {
	var o *Overlay
	buf := []byte{1, 2, 3}
	o.Apply(buf, 0)
	if !bytes.Equal(buf, []byte{1, 2, 3}) {
		t.Fatal("nil overlay modified the buffer")
	}
	if err := o.PrepareResponse(&http.Response{StatusCode: http.StatusOK}); err != nil {
		t.Fatal(err)
	}
}
