package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Overlay describes the substitution applied to a single proxied response.
//
// The effective ISO is the source ISO with [Offset, Offset+len(Archive))
// replaced by Archive and [Offset+len(Archive), Offset+Capacity) zeroed. The
// ISO's size never changes, so ranges, lengths, and every byte outside the
// slot are untouched.
type Overlay struct {
	// Offset is the slot's absolute byte offset within the ISO.
	Offset int64
	// Capacity is the slot's reserved size.
	Capacity int64
	// Archive is the generated configuration archive. len(Archive) <= Capacity.
	Archive []byte

	// Start is the absolute ISO offset of the first byte of the response body.
	// It is derived from the upstream response, not from the requested range.
	Start int64
	// Enabled reports whether the upstream response is a contiguous byte run
	// that can be overlaid.
	Enabled bool
}

// Apply rewrites the portion of buf that intersects the slot. start is the
// absolute ISO offset of buf[0]. Buffers wholly outside the slot are untouched,
// and buffers that begin or end inside the slot are handled without needing a
// marker to appear in them.
func (o *Overlay) Apply(buf []byte, start int64) {
	if o == nil || len(buf) == 0 {
		return
	}
	lo := max(start, o.Offset)
	hi := min(start+int64(len(buf)), o.Offset+o.Capacity)
	if lo >= hi {
		return
	}
	dst := buf[lo-start : hi-start]
	if rel := lo - o.Offset; rel < int64(len(o.Archive)) {
		n := min(int64(len(dst)), int64(len(o.Archive))-rel)
		copy(dst[:n], o.Archive[rel:rel+n])
		dst = dst[n:]
	}
	// Everything past the archive is the reserved slot's zero padding, which
	// the kernel skips when walking the initramfs archive sequence.
	clear(dst)
}

// PrepareResponse records where the upstream response body starts in the ISO.
//
// A partial response's offset comes from Content-Range because an upstream may
// answer a range request with the full image, or with a different range than
// the one requested. Multipart range responses are not a contiguous run of ISO
// bytes and disable the overlay.
func (o *Overlay) PrepareResponse(resp *http.Response) error {
	if o == nil || resp == nil {
		return nil
	}
	o.Enabled = false
	switch resp.StatusCode {
	case http.StatusOK:
		o.Start = 0
		o.Enabled = true
		return nil
	case http.StatusPartialContent:
		start, err := parseContentRangeStart(resp.Header.Get("Content-Range"))
		if err != nil {
			return err
		}
		o.Start = start
		o.Enabled = true
		return nil
	default:
		return nil
	}
}

func parseContentRangeStart(v string) (int64, error) {
	unit, spec, ok := strings.Cut(strings.TrimSpace(v), " ")
	if !ok || !strings.EqualFold(unit, "bytes") {
		return 0, fmt.Errorf("unsupported Content-Range %q", v)
	}
	rng, _, ok := strings.Cut(strings.TrimSpace(spec), "/")
	if !ok {
		return 0, fmt.Errorf("malformed Content-Range %q", v)
	}
	first, _, ok := strings.Cut(rng, "-")
	if !ok {
		return 0, fmt.Errorf("malformed Content-Range %q", v)
	}
	start, err := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	if err != nil || start < 0 {
		return 0, fmt.Errorf("malformed Content-Range %q", v)
	}
	return start, nil
}

type overlayCtxKey struct{}

// WithOverlay returns ctx carrying o.
func WithOverlay(ctx context.Context, o *Overlay) context.Context {
	return context.WithValue(ctx, overlayCtxKey{}, o)
}

// FromContext returns the overlay stored in ctx, or nil.
func FromContext(ctx context.Context) *Overlay {
	o, _ := ctx.Value(overlayCtxKey{}).(*Overlay)
	return o
}
