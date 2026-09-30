package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
)

// The three declarations below are the contract with the CaptainOS build. Each
// has a counterpart in captain/captain/iso.py that must agree:
//
//	LayoutSuffix  <-> BOOTSTRAP_LAYOUT_SUFFIX
//	LayoutVersion <-> BOOTSTRAP_LAYOUT_VERSION
//	Layout fields <-> the dict written by _write_bootstrap_layout
//
// Nothing else is shared. The slot's offset and capacity are deliberately not
// constants: they are measured per build and published here, because they move
// between releases, architectures and flavors.

// LayoutSuffix is appended to a source ISO's URL path to locate its layout
// metadata, e.g. "captainos.iso" -> "captainos.iso.layout.json".
const LayoutSuffix = ".layout.json"

// LayoutVersion is the only layout schema version understood here. An ISO
// published with a newer schema is treated as having no slot rather than
// patched on guesses about fields this build does not know.
const LayoutVersion = 1

// maxLayoutBytes bounds how much of an unknown upstream response is read.
const maxLayoutBytes = 64 << 10

// ErrNoLayout reports that the source ISO publishes no usable layout metadata,
// so it has no reserved slot and must not be patched at a guessed location.
// A response that is not valid layout metadata counts as none: the artifact is
// unsupported for this delivery mode, which is not a reason to refuse to serve
// it. A transport or server failure is reported as an error instead, because
// an ISO whose slot exists but was not personalized would boot misconfigured.
var ErrNoLayout = errors.New("source ISO publishes no bootstrap layout metadata")

// Layout is the metadata CaptainOS publishes next to each ISO. It binds the
// reserved slot's location to one exact immutable artifact; offsets are not
// constant across releases, architectures, or flavors.
type Layout struct {
	Version int `json:"version"`
	ISO     struct {
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	} `json:"iso"`
	Slot struct {
		Path     string `json:"path"`
		Offset   int64  `json:"offset"`
		Capacity int64  `json:"capacity"`
	} `json:"slot"`
}

func (l *Layout) validate() error {
	switch {
	case l.Version != LayoutVersion:
		return fmt.Errorf("unsupported bootstrap layout version %d, want %d", l.Version, LayoutVersion)
	case l.Slot.Path == "":
		return errors.New("bootstrap layout has no slot path")
	case l.Slot.Offset <= 0:
		return fmt.Errorf("bootstrap layout has invalid slot offset %d", l.Slot.Offset)
	case l.Slot.Capacity <= 0:
		return fmt.Errorf("bootstrap layout has invalid slot capacity %d", l.Slot.Capacity)
	case l.ISO.Size > 0 && l.Slot.Offset+l.Slot.Capacity > l.ISO.Size:
		return fmt.Errorf("bootstrap slot [%d,%d) lies outside the %d byte ISO", l.Slot.Offset, l.Slot.Offset+l.Slot.Capacity, l.ISO.Size)
	}
	return nil
}

// LayoutURL returns the location of the layout metadata for isoURL.
func LayoutURL(isoURL *url.URL) *url.URL {
	u := *isoURL
	u.Path += LayoutSuffix
	u.RawPath = ""
	return &u
}

// Fetcher retrieves and caches layout metadata per source ISO URL.
//
// A BMC issues thousands of range requests per mount, so both successful
// lookups and "no slot published" answers are cached. Transport failures are
// not cached so a transient error does not disable personalization for the
// process lifetime.
type Fetcher struct {
	Client *http.Client

	mu      sync.RWMutex
	entries map[string]layoutResult
}

type layoutResult struct {
	layout *Layout
	err    error
}

// Get returns the layout for isoURL, or ErrNoLayout if none is published.
func (f *Fetcher) Get(ctx context.Context, isoURL *url.URL) (*Layout, error) {
	key := isoURL.String()
	f.mu.RLock()
	got, ok := f.entries[key]
	f.mu.RUnlock()
	if ok {
		return got.layout, got.err
	}

	l, err := f.fetch(ctx, LayoutURL(isoURL))
	if err != nil && !errors.Is(err, ErrNoLayout) {
		return nil, err
	}

	f.mu.Lock()
	if f.entries == nil {
		f.entries = make(map[string]layoutResult)
	}
	f.entries[key] = layoutResult{layout: l, err: err}
	f.mu.Unlock()

	return l, err
}

func (f *Fetcher) fetch(ctx context.Context, u *url.URL) (*Layout, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching bootstrap layout %s: %w", u, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusForbidden:
		return nil, ErrNoLayout
	default:
		return nil, fmt.Errorf("fetching bootstrap layout %s: unexpected status %s", u, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLayoutBytes))
	if err != nil {
		return nil, fmt.Errorf("reading bootstrap layout %s: %w", u, err)
	}
	l := &Layout{}
	if err := json.Unmarshal(body, l); err != nil {
		return nil, fmt.Errorf("parsing bootstrap layout %s: %w: %w", u, ErrNoLayout, err)
	}
	if err := l.validate(); err != nil {
		return nil, fmt.Errorf("invalid bootstrap layout %s: %w: %w", u, ErrNoLayout, err)
	}
	return l, nil
}
