// Package ui provides the web UI for Tinkerbell.
package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Global variables to hold client and namespace
var (
	kubeClient    *kubernetes.Clientset
	dynamicClient dynamic.Interface
	namespace     string
)

// Resource definitions for Tinkerbell CRDs
var (
	hardwareGVR = schema.GroupVersionResource{
		Group:    "tinkerbell.org",
		Version:  "v1alpha1",
		Resource: "hardware",
	}
	templateGVR = schema.GroupVersionResource{
		Group:    "tinkerbell.org",
		Version:  "v1alpha1",
		Resource: "templates",
	}
	workflowGVR = schema.GroupVersionResource{
		Group:    "tinkerbell.org",
		Version:  "v1alpha1",
		Resource: "workflows",
	}
)

func StartServer(addr string, client *kubernetes.Clientset, dynClient dynamic.Interface, ns string) error {
	kubeClient = client
	dynamicClient = dynClient
	namespace = ns

	mux := http.NewServeMux()

	// Register static file handler
	RegisterStatic(mux)

	// Register page handlers
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})
	mux.HandleFunc("/hardware", hardwareListHandler)
	mux.HandleFunc("/hardware/", hardwareDetailHandler)
	mux.HandleFunc("/templates", templateListHandler)
	mux.HandleFunc("/templates/", templateDetailHandler)
	mux.HandleFunc("/workflows", workflowListHandler)
	mux.HandleFunc("/workflows/", workflowDetailHandler)

	return http.ListenAndServe(addr, mux)
}

// Helper functions for Kubernetes operations
func listHardware(ctx context.Context) ([]tinkerbell.Hardware, error) {
	unstructuredList, err := dynamicClient.Resource(hardwareGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list hardware: %w", err)
	}

	var hardwareList []tinkerbell.Hardware
	for _, item := range unstructuredList.Items {
		var hardware tinkerbell.Hardware
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &hardware); err != nil {
			continue // Skip invalid items
		}
		hardwareList = append(hardwareList, hardware)
	}
	return hardwareList, nil
}

func getHardware(ctx context.Context, name string) (*tinkerbell.Hardware, error) {
	unstructured, err := dynamicClient.Resource(hardwareGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get hardware %s: %w", name, err)
	}

	var hardware tinkerbell.Hardware
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &hardware); err != nil {
		return nil, fmt.Errorf("failed to convert hardware %s: %w", name, err)
	}
	return &hardware, nil
}

func listTemplates(ctx context.Context) ([]tinkerbell.Template, error) {
	unstructuredList, err := dynamicClient.Resource(templateGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}

	var templateList []tinkerbell.Template
	for _, item := range unstructuredList.Items {
		var template tinkerbell.Template
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &template); err != nil {
			continue // Skip invalid items
		}
		templateList = append(templateList, template)
	}
	return templateList, nil
}

func getTemplate(ctx context.Context, name string) (*tinkerbell.Template, error) {
	unstructured, err := dynamicClient.Resource(templateGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get template %s: %w", name, err)
	}

	var template tinkerbell.Template
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &template); err != nil {
		return nil, fmt.Errorf("failed to convert template %s: %w", name, err)
	}
	return &template, nil
}

func listWorkflows(ctx context.Context) ([]tinkerbell.Workflow, error) {
	unstructuredList, err := dynamicClient.Resource(workflowGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	var workflowList []tinkerbell.Workflow
	for _, item := range unstructuredList.Items {
		var workflow tinkerbell.Workflow
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &workflow); err != nil {
			continue // Skip invalid items
		}
		workflowList = append(workflowList, workflow)
	}
	return workflowList, nil
}

func getWorkflow(ctx context.Context, name string) (*tinkerbell.Workflow, error) {
	unstructured, err := dynamicClient.Resource(workflowGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %s: %w", name, err)
	}

	var workflow tinkerbell.Workflow
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &workflow); err != nil {
		return nil, fmt.Errorf("failed to convert workflow %s: %w", name, err)
	}
	return &workflow, nil
}

// Handler implementations
func hardwareListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		hardwareList, err := listHardware(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list hardware: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		HardwarePage(hardwareList).Render(r.Context(), w)
	case "POST":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Create new Hardware from form data
		name := r.FormValue("name")
		if name == "" {
			http.Error(w, "Hardware name is required", http.StatusBadRequest)
			return
		}

		hardware := &tinkerbell.Hardware{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Hardware",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: tinkerbell.HardwareSpec{
				// Add spec fields from form as needed
			},
		}

		// Convert to unstructured for creation
		unstructuredHardware, err := runtime.DefaultUnstructuredConverter.ToUnstructured(hardware)
		if err != nil {
			http.Error(w, "Failed to convert hardware", http.StatusInternalServerError)
			return
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(unstructuredHardware)

		_, err = dynamicClient.Resource(hardwareGVR).Namespace(namespace).Create(r.Context(), obj, metav1.CreateOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create hardware: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("HX-Redirect", "/hardware")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func hardwareDetailHandler(w http.ResponseWriter, r *http.Request) {
	// Extract hardware name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/hardware/")
	hardwareName := strings.Split(path, "/")[0]

	switch r.Method {
	case "GET":
		hw, err := getHardware(r.Context(), hardwareName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get hardware: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		HardwareDetailPage(*hw).Render(r.Context(), w)
	case "PUT", "PATCH":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get existing hardware
		existingHardware, err := getHardware(r.Context(), hardwareName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get hardware: %v", err), http.StatusInternalServerError)
			return
		}

		// Update with form data
		name := r.FormValue("name")
		if name != "" {
			existingHardware.Name = name
		}

		unstructuredHardware, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingHardware)
		if err != nil {
			http.Error(w, "Failed to convert hardware", http.StatusInternalServerError)
			return
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(unstructuredHardware)

		_, err = dynamicClient.Resource(hardwareGVR).Namespace(namespace).Update(r.Context(), obj, metav1.UpdateOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to update hardware: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("HX-Redirect", "/hardware/"+hardwareName)
	case "DELETE":
		err := dynamicClient.Resource(hardwareGVR).Namespace(namespace).Delete(r.Context(), hardwareName, metav1.DeleteOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete hardware: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		w.Header().Set("HX-Redirect", "/hardware")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func templateListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		templateList, err := listTemplates(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list templates: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		TemplatePage(templateList).Render(r.Context(), w)
	case "POST":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		name := r.FormValue("name")
		data := r.FormValue("data")
		if name == "" {
			http.Error(w, "Template name is required", http.StatusBadRequest)
			return
		}

		template := &tinkerbell.Template{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Template",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: tinkerbell.TemplateSpec{
				Data: &data,
			},
		}

		unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
		if err != nil {
			http.Error(w, "Failed to convert template", http.StatusInternalServerError)
			return
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(unstructuredTemplate)

		_, err = dynamicClient.Resource(templateGVR).Namespace(namespace).Create(r.Context(), obj, metav1.CreateOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create template: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("HX-Redirect", "/templates")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func templateDetailHandler(w http.ResponseWriter, r *http.Request) {
	// Extract template name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/templates/")
	templateName := strings.Split(path, "/")[0]

	switch r.Method {
	case "GET":
		tmpl, err := getTemplate(r.Context(), templateName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get template: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		TemplateDetailPage(*tmpl).Render(r.Context(), w)
	case "PUT", "PATCH":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get existing template
		existingTemplate, err := getTemplate(r.Context(), templateName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get template: %v", err), http.StatusInternalServerError)
			return
		}

		// Update with form data
		name := r.FormValue("name")
		data := r.FormValue("data")
		if name != "" {
			existingTemplate.Name = name
		}
		if data != "" {
			existingTemplate.Spec.Data = &data
		}

		unstructuredTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingTemplate)
		if err != nil {
			http.Error(w, "Failed to convert template", http.StatusInternalServerError)
			return
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(unstructuredTemplate)

		_, err = dynamicClient.Resource(templateGVR).Namespace(namespace).Update(r.Context(), obj, metav1.UpdateOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to update template: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("HX-Redirect", "/templates/"+templateName)
	case "DELETE":
		err := dynamicClient.Resource(templateGVR).Namespace(namespace).Delete(r.Context(), templateName, metav1.DeleteOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete template: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		w.Header().Set("HX-Redirect", "/templates")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func workflowListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		workflowList, err := listWorkflows(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list workflows: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		WorkflowPage(workflowList).Render(r.Context(), w)
	case "POST":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		name := r.FormValue("name")
		templateRef := r.FormValue("template-ref")
		hardwareRef := r.FormValue("hardware-ref")
		if name == "" {
			http.Error(w, "Workflow name is required", http.StatusBadRequest)
			return
		}

		workflow := &tinkerbell.Workflow{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Workflow",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: tinkerbell.WorkflowSpec{
				TemplateRef: templateRef,
				HardwareRef: hardwareRef,
			},
		}

		unstructuredWorkflow, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workflow)
		if err != nil {
			http.Error(w, "Failed to convert workflow", http.StatusInternalServerError)
			return
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(unstructuredWorkflow)

		_, err = dynamicClient.Resource(workflowGVR).Namespace(namespace).Create(r.Context(), obj, metav1.CreateOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create workflow: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("HX-Redirect", "/workflows")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func workflowDetailHandler(w http.ResponseWriter, r *http.Request) {
	// Extract workflow name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/workflows/")
	workflowName := strings.Split(path, "/")[0]

	switch r.Method {
	case "GET":
		wf, err := getWorkflow(r.Context(), workflowName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get workflow: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		WorkflowDetailPage(*wf).Render(r.Context(), w)
	case "PUT", "PATCH":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get existing workflow
		existingWorkflow, err := getWorkflow(r.Context(), workflowName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get workflow: %v", err), http.StatusInternalServerError)
			return
		}

		// Update with form data
		name := r.FormValue("name")
		templateRef := r.FormValue("template-ref")
		hardwareRef := r.FormValue("hardware-ref")
		if name != "" {
			existingWorkflow.Name = name
		}
		if templateRef != "" {
			existingWorkflow.Spec.TemplateRef = templateRef
		}
		if hardwareRef != "" {
			existingWorkflow.Spec.HardwareRef = hardwareRef
		}

		unstructuredWorkflow, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingWorkflow)
		if err != nil {
			http.Error(w, "Failed to convert workflow", http.StatusInternalServerError)
			return
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(unstructuredWorkflow)

		_, err = dynamicClient.Resource(workflowGVR).Namespace(namespace).Update(r.Context(), obj, metav1.UpdateOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to update workflow: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("HX-Redirect", "/workflows/"+workflowName)
	case "DELETE":
		err := dynamicClient.Resource(workflowGVR).Namespace(namespace).Delete(r.Context(), workflowName, metav1.DeleteOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete workflow: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
		w.Header().Set("HX-Redirect", "/workflows")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
