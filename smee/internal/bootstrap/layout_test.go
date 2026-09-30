package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestLayoutURL(t *testing.T) {
	tests := map[string]string{
		"http://example.com/captainos.iso":      "http://example.com/captainos.iso.layout.json",
		"https://example.com/a/b.iso?sig=abc":   "https://example.com/a/b.iso.layout.json?sig=abc",
		"http://example.com/with%20space.iso":   "http://example.com/with%20space.iso.layout.json",
		"http://example.com:8080/x/captain.iso": "http://example.com:8080/x/captain.iso.layout.json",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			u, err := url.Parse(in)
			if err != nil {
				t.Fatal(err)
			}
			if got := LayoutURL(u).String(); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestFetcherGet(t *testing.T) {
	const valid = `{"version":1,"iso":{"size":104857600,"sha256":"abc"},"slot":{"path":"/boot/bootstrap.cpio","offset":2097152,"capacity":4194304}}`

	tests := map[string]struct {
		body       string
		status     int
		wantErr    error
		wantOffset int64
		wantAny    bool
	}{
		"valid":                  {body: valid, status: http.StatusOK, wantOffset: 2097152},
		"not published":          {status: http.StatusNotFound, wantErr: ErrNoLayout},
		"forbidden":              {status: http.StatusForbidden, wantErr: ErrNoLayout},
		"server error":           {status: http.StatusInternalServerError, wantAny: true},
		"not json":               {body: "<html>", status: http.StatusOK, wantErr: ErrNoLayout},
		"unsupported version":    {body: `{"version":2,"slot":{"path":"x","offset":1,"capacity":1}}`, status: http.StatusOK, wantErr: ErrNoLayout},
		"zero capacity":          {body: `{"version":1,"slot":{"path":"x","offset":1,"capacity":0}}`, status: http.StatusOK, wantErr: ErrNoLayout},
		"zero offset":            {body: `{"version":1,"slot":{"path":"x","offset":0,"capacity":1}}`, status: http.StatusOK, wantErr: ErrNoLayout},
		"slot outside the image": {body: `{"version":1,"iso":{"size":10},"slot":{"path":"x","offset":8,"capacity":8}}`, status: http.StatusOK, wantErr: ErrNoLayout},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/captainos.iso"+LayoutSuffix {
					t.Errorf("unexpected layout path %q", r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			u, err := url.Parse(srv.URL + "/captainos.iso")
			if err != nil {
				t.Fatal(err)
			}
			f := &Fetcher{}
			got, err := f.Get(context.Background(), u)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got error %v, want %v", err, tt.wantErr)
				}
			case tt.wantAny:
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if got.Slot.Offset != tt.wantOffset {
					t.Fatalf("got offset %d, want %d", got.Slot.Offset, tt.wantOffset)
				}
			}
		})
	}
}

// TestFetcherCaches guards against re-fetching per range request; a BMC makes
// thousands of them per mount.
func TestFetcherCaches(t *testing.T) {
	for name, status := range map[string]int{"published": http.StatusOK, "not published": http.StatusNotFound} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"version":1,"slot":{"path":"/boot/bootstrap.cpio","offset":2048,"capacity":4096}}`))
			}))
			defer srv.Close()

			u, _ := url.Parse(srv.URL + "/captainos.iso")
			f := &Fetcher{}
			for range 5 {
				_, _ = f.Get(context.Background(), u)
			}
			if hits.Load() != 1 {
				t.Fatalf("fetched layout %d times, want 1", hits.Load())
			}
		})
	}
}

// TestFetcherDoesNotCacheTransportErrors keeps a transient failure from
// disabling personalization for the process lifetime.
func TestFetcherDoesNotCacheTransportErrors(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL + "/captainos.iso")
	f := &Fetcher{}
	for range 3 {
		if _, err := f.Get(context.Background(), u); err == nil {
			t.Fatal("expected an error")
		}
	}
	if hits.Load() != 3 {
		t.Fatalf("fetched layout %d times, want 3", hits.Load())
	}
}
