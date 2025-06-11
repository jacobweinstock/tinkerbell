package ui

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func newClient(kubeconfig, namespace string) (*kubernetes.Clientset, dynamic.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return clientset, dynClient, nil
}

func setupRouter(client *kubernetes.Clientset, dynClient dynamic.Interface, ns string) http.Handler {
	// Initialize global variables in server.go
	kubeClient = client
	dynamicClient = dynClient
	namespace = ns

	mux := http.NewServeMux()

	// Register static file handler
	RegisterStatic(mux)

	// Register page handlers
	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/hardware", hardwareListHandler)
	mux.HandleFunc("/hardware/", hardwareDetailHandler)
	mux.HandleFunc("/templates", templateListHandler)
	mux.HandleFunc("/templates/", templateDetailHandler)
	mux.HandleFunc("/workflows", workflowListHandler)
	mux.HandleFunc("/workflows/", workflowDetailHandler)

	return mux
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	HomePage().Render(r.Context(), w)
}

func Main() {
	var (
		port        = flag.Int("port", 8080, "Port to listen on")
		kubeconfig  = flag.String("kubeconfig", "", "Path to kubeconfig file")
		namespace   = flag.String("namespace", "default", "Kubernetes namespace")
		development = flag.Bool("development", false, "Enable development mode")
	)
	flag.Parse()

	// Set up logger
	logOpts := &slog.HandlerOptions{}
	if *development {
		logOpts.Level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, logOpts))
	slog.SetDefault(logger)

	var handler http.Handler

	if *development {
		logger.Info("running in development mode with mock data")
		handler = setupDevelopmentHandlers()
	} else {
		// Initialize clients
		client, dynClient, err := newClient(*kubeconfig, *namespace)
		if err != nil {
			logger.Error("failed to create client", "error", err)
			os.Exit(1)
		}
		handler = setupRouter(client, dynClient, *namespace)
	}

	// Create server
	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("starting server", "address", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger.Info("shutting down server")
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
	}
}
