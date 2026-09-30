package kube

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestManagerUsesBackendCache(t *testing.T) {
	cfg := &rest.Config{Host: "http://127.0.0.1:1"}
	b, err := NewBackend(Backend{ClientConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := controllerruntime.NewManager(cfg, controllerruntime.Options{
		NewCache:               b.NewCache,
		Metrics:                server.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		t.Fatal(err)
	}

	shared, ok := mgr.GetCache().(sharedCache)
	if !ok || shared.Cache != b.cluster.GetCache() {
		t.Fatalf("manager cache = %T, want the backend's cache", mgr.GetCache())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := shared.Start(ctx); err != nil {
		t.Fatalf("Start = %v, want it to wait for ctx and return nil", err)
	}
}
