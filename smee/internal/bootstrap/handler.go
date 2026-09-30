package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/pkg/data"
)

// ArchiveName is the file name iPXE fetches from the Handler.
const ArchiveName = "bootstrap.cpio"

// BackendReader is the interface for getting hardware data from a backend.
type BackendReader interface {
	FilterHardware(ctx context.Context, opts data.HardwareFilter) (*tinkerbell.Hardware, error)
}

// Handler serves the per-host configuration archive directly, for iPXE to load
// as an additional initrd.
//
// Unlike the ISO path there is no reserved slot, so the response is just the
// generated archive: no padding, no offsets, and no layout metadata. iPXE
// appends archives rather than patching bytes into an existing image.
type Handler struct {
	Logger  logr.Logger
	Backend BackendReader
}

// HandlerFunc returns a handler for /bootstrap/<mac>/bootstrap.cpio.
func (h *Handler) HandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := h.Logger.WithValues("method", r.Method, "uri", r.RequestURI, "remoteAddr", r.RemoteAddr)

		if base := path.Base(r.URL.Path); base != ArchiveName {
			log.V(1).Info("unsupported bootstrap archive name", "name", base)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ha, err := macFromPath(r.URL.Path)
		if err != nil {
			log.Info("unable to parse mac address in the URL path", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		archive, err := h.archiveFor(r.Context(), ha)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("no hardware record for this machine", "mac", ha.String())
				w.WriteHeader(http.StatusNotFound)
				return
			}
			log.Error(err, "unable to build the bootstrap archive", "mac", ha.String())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Generated in full before any bytes are written, so a failure is an
		// error response rather than a truncated archive the kernel would
		// silently drop.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if _, err := w.Write(archive); err != nil {
			log.Error(err, "unable to write the bootstrap archive", "mac", ha.String())
			return
		}
		log.V(1).Info("served bootstrap archive", "mac", ha.String(), "bytes", len(archive))
	}
}

func (h *Handler) archiveFor(ctx context.Context, mac net.HardwareAddr) ([]byte, error) {
	if h.Backend == nil {
		return nil, fmt.Errorf("no backend configured")
	}
	spec, err := h.Backend.FilterHardware(ctx, data.HardwareFilter{ByMACAddress: mac.String()})
	if err != nil {
		return nil, err
	}

	files, err := Files(spec)
	if err != nil {
		return nil, err
	}

	return Archive(files, ModTime(spec))
}

func macFromPath(urlPath string) (net.HardwareAddr, error) {
	mac := path.Base(path.Dir(urlPath))
	ha, err := net.ParseMAC(mac)
	if err != nil {
		return nil, fmt.Errorf("the second to last element in the URL path must be a valid mac address: %w", err)
	}

	return ha, nil
}
