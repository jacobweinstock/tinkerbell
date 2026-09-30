// Package hardware runs the controllers that report on Hardware objects.
package hardware

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/hardware/internal/controller"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Config configures the Hardware controller.
type Config struct {
	Client                  *rest.Config
	EnableLeaderElection    bool
	LeaderElectionNamespace string
	// Renders reports Hardware render outcomes.
	Renders controller.RenderReporter
}

// NewConfig returns a Config with defaults.
func NewConfig() *Config {
	return &Config{EnableLeaderElection: true}
}

// Start runs the Hardware controller until ctx is done.
func (c *Config) Start(ctx context.Context, log logr.Logger) error {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := tinkerbell.AddToScheme(scheme); err != nil {
		return err
	}

	mgr, err := controllerruntime.NewManager(c.Client, controllerruntime.Options{
		Scheme:                  scheme,
		Logger:                  log,
		LeaderElection:          c.EnableLeaderElection,
		LeaderElectionID:        "hardware-controller.tinkerbell.org",
		LeaderElectionNamespace: c.LeaderElectionNamespace,
		// "0" disables the manager's own servers; metrics and health are served by the main HTTP server.
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/metrics/server#Options
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Options
		Metrics:                server.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		return err
	}
	if err := controller.NewRenderedReconciler(mgr.GetClient(), c.Renders).SetupWithManager(mgr); err != nil {
		return err
	}

	return mgr.Start(ctx)
}
