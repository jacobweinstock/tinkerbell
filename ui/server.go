// Package ui provides the web UI for Tinkerbell.
package ui

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

		// Update hardware with comprehensive form data
		if err := updateHardwareFromForm(existingHardware, r); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update hardware fields: %v", err), http.StatusBadRequest)
			return
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

// updateHardwareFromForm updates a Hardware object with data from the HTTP form
func updateHardwareFromForm(hw *tinkerbell.Hardware, r *http.Request) error {
	// Update basic fields
	if name := r.FormValue("name"); name != "" {
		hw.Name = name
	}

	if tinkVersionStr := r.FormValue("tinkVersion"); tinkVersionStr != "" {
		if tinkVersion, err := strconv.ParseInt(tinkVersionStr, 10, 64); err == nil {
			hw.Spec.TinkVersion = tinkVersion
		}
	}

	if userData := r.FormValue("userData"); userData != "" {
		hw.Spec.UserData = &userData
	} else {
		hw.Spec.UserData = nil
	}

	if vendorData := r.FormValue("vendorData"); vendorData != "" {
		hw.Spec.VendorData = &vendorData
	} else {
		hw.Spec.VendorData = nil
	}

	// Update BMC Reference
	bmcName := r.FormValue("bmcName")
	bmcKind := r.FormValue("bmcKind")
	bmcAPIGroup := r.FormValue("bmcAPIGroup")

	if bmcName != "" || bmcKind != "" || bmcAPIGroup != "" {
		if hw.Spec.BMCRef == nil {
			hw.Spec.BMCRef = &corev1.TypedLocalObjectReference{}
		}
		if bmcName != "" {
			hw.Spec.BMCRef.Name = bmcName
		}
		if bmcKind != "" {
			hw.Spec.BMCRef.Kind = bmcKind
		}
		if bmcAPIGroup != "" {
			hw.Spec.BMCRef.APIGroup = &bmcAPIGroup
		}
	}

	// Update Resources
	hw.Spec.Resources = make(map[string]resource.Quantity)
	for key := range r.Form {
		if strings.HasPrefix(key, "resources[") && strings.HasSuffix(key, "].key") {
			if resourceValues, ok := r.Form[key]; ok && len(resourceValues) > 0 {
				resourceKey := resourceValues[0]
				if resourceKey != "" {
					// Find corresponding value
					valueKey := strings.Replace(key, "].key", "].value", 1)
					if resourceValues, ok := r.Form[valueKey]; ok && len(resourceValues) > 0 {
						if qty, err := resource.ParseQuantity(resourceValues[0]); err == nil {
							hw.Spec.Resources[resourceKey] = qty
						}
					}
				}
			}
		}
	}

	// Update References
	hw.Spec.References = make(map[string]tinkerbell.Reference)
	processedRefs := make(map[string]bool)

	for key := range r.Form {
		if strings.HasPrefix(key, "references[") && strings.Contains(key, "]") {
			// Extract reference name from key like "references[refName].field"
			start := strings.Index(key, "[") + 1
			end := strings.Index(key, "]")
			if start > 0 && end > start {
				refName := key[start:end]

				if !processedRefs[refName] {
					ref := tinkerbell.Reference{}

					// Get all fields for this reference
					if nameValues, ok := r.Form[fmt.Sprintf("references[%s].name", refName)]; ok && len(nameValues) > 0 {
						actualRefName := nameValues[0]
						if actualRefName != "" {
							refName = actualRefName
						}
					}

					if objNames, ok := r.Form[fmt.Sprintf("references[%s].objectName", refName)]; ok && len(objNames) > 0 {
						ref.Name = objNames[0]
					}
					if namespaces, ok := r.Form[fmt.Sprintf("references[%s].namespace", refName)]; ok && len(namespaces) > 0 {
						ref.Namespace = namespaces[0]
					}
					if groups, ok := r.Form[fmt.Sprintf("references[%s].group", refName)]; ok && len(groups) > 0 {
						ref.Group = groups[0]
					}
					if versions, ok := r.Form[fmt.Sprintf("references[%s].version", refName)]; ok && len(versions) > 0 {
						ref.Version = versions[0]
					}
					if resources, ok := r.Form[fmt.Sprintf("references[%s].resource", refName)]; ok && len(resources) > 0 {
						ref.Resource = resources[0]
					}

					if ref.Name != "" || ref.Namespace != "" || ref.Group != "" || ref.Version != "" || ref.Resource != "" {
						hw.Spec.References[refName] = ref
					}
					processedRefs[refName] = true
				}
			}
		}
	}

	// Update Hardware Metadata
	if hasAnyMetadataField(r) {
		if hw.Spec.Metadata == nil {
			hw.Spec.Metadata = &tinkerbell.HardwareMetadata{}
		}

		// Basic metadata fields
		if state := r.FormValue("metadata.state"); state != "" {
			hw.Spec.Metadata.State = state
		}

		if bondingModeStr := r.FormValue("metadata.bondingMode"); bondingModeStr != "" {
			if bondingMode, err := strconv.ParseInt(bondingModeStr, 10, 64); err == nil {
				hw.Spec.Metadata.BondingMode = bondingMode
			}
		}

		// Manufacturer metadata
		manufacturerID := r.FormValue("metadata.manufacturer.id")
		manufacturerSlug := r.FormValue("metadata.manufacturer.slug")
		if manufacturerID != "" || manufacturerSlug != "" {
			if hw.Spec.Metadata.Manufacturer == nil {
				hw.Spec.Metadata.Manufacturer = &tinkerbell.MetadataManufacturer{}
			}
			if manufacturerID != "" {
				hw.Spec.Metadata.Manufacturer.ID = manufacturerID
			}
			if manufacturerSlug != "" {
				hw.Spec.Metadata.Manufacturer.Slug = manufacturerSlug
			}
		}

		// Instance metadata
		instanceID := r.FormValue("metadata.instance.id")
		instanceState := r.FormValue("metadata.instance.state")
		instanceHostname := r.FormValue("metadata.instance.hostname")
		instanceIpxeURL := r.FormValue("metadata.instance.ipxeScriptURL")
		instanceUserdata := r.FormValue("metadata.instance.userdata")
		instancePassword := r.FormValue("metadata.instance.cryptedRootPassword")
		instanceAllowPxe := r.FormValue("metadata.instance.allowPxe") == "true"
		instanceRescue := r.FormValue("metadata.instance.rescue") == "true"
		instanceAlwaysPxe := r.FormValue("metadata.instance.alwaysPxe") == "true"
		instanceNetworkReady := r.FormValue("metadata.instance.networkReady") == "true"

		if instanceID != "" || instanceState != "" || instanceHostname != "" || instanceIpxeURL != "" ||
			instanceUserdata != "" || instancePassword != "" || instanceAllowPxe || instanceRescue ||
			instanceAlwaysPxe || instanceNetworkReady {
			if hw.Spec.Metadata.Instance == nil {
				hw.Spec.Metadata.Instance = &tinkerbell.MetadataInstance{}
			}
			if instanceID != "" {
				hw.Spec.Metadata.Instance.ID = instanceID
			}
			if instanceState != "" {
				hw.Spec.Metadata.Instance.State = instanceState
			}
			if instanceHostname != "" {
				hw.Spec.Metadata.Instance.Hostname = instanceHostname
			}
			if instanceIpxeURL != "" {
				hw.Spec.Metadata.Instance.IpxeScriptURL = instanceIpxeURL
			}
			if instanceUserdata != "" {
				hw.Spec.Metadata.Instance.Userdata = instanceUserdata
			}
			if instancePassword != "" {
				hw.Spec.Metadata.Instance.CryptedRootPassword = instancePassword
			}
			hw.Spec.Metadata.Instance.AllowPxe = instanceAllowPxe
			hw.Spec.Metadata.Instance.Rescue = instanceRescue
			hw.Spec.Metadata.Instance.AlwaysPxe = instanceAlwaysPxe
			hw.Spec.Metadata.Instance.NetworkReady = instanceNetworkReady
		}

		// Operating System metadata
		osSlug := r.FormValue("metadata.instance.operatingSystem.slug")
		osDistro := r.FormValue("metadata.instance.operatingSystem.distro")
		osVersion := r.FormValue("metadata.instance.operatingSystem.version")
		osImageTag := r.FormValue("metadata.instance.operatingSystem.imageTag")
		osOsSlug := r.FormValue("metadata.instance.operatingSystem.osSlug")

		if osSlug != "" || osDistro != "" || osVersion != "" || osImageTag != "" || osOsSlug != "" {
			if hw.Spec.Metadata.Instance == nil {
				hw.Spec.Metadata.Instance = &tinkerbell.MetadataInstance{}
			}
			if hw.Spec.Metadata.Instance.OperatingSystem == nil {
				hw.Spec.Metadata.Instance.OperatingSystem = &tinkerbell.MetadataInstanceOperatingSystem{}
			}
			if osSlug != "" {
				hw.Spec.Metadata.Instance.OperatingSystem.Slug = osSlug
			}
			if osDistro != "" {
				hw.Spec.Metadata.Instance.OperatingSystem.Distro = osDistro
			}
			if osVersion != "" {
				hw.Spec.Metadata.Instance.OperatingSystem.Version = osVersion
			}
			if osImageTag != "" {
				hw.Spec.Metadata.Instance.OperatingSystem.ImageTag = osImageTag
			}
			if osOsSlug != "" {
				hw.Spec.Metadata.Instance.OperatingSystem.OsSlug = osOsSlug
			}
		}

		// Facility metadata
		facilityPlanSlug := r.FormValue("metadata.facility.planSlug")
		facilityPlanVersionSlug := r.FormValue("metadata.facility.planVersionSlug")
		facilityCode := r.FormValue("metadata.facility.facilityCode")

		if facilityPlanSlug != "" || facilityPlanVersionSlug != "" || facilityCode != "" {
			if hw.Spec.Metadata.Facility == nil {
				hw.Spec.Metadata.Facility = &tinkerbell.MetadataFacility{}
			}
			if facilityPlanSlug != "" {
				hw.Spec.Metadata.Facility.PlanSlug = facilityPlanSlug
			}
			if facilityPlanVersionSlug != "" {
				hw.Spec.Metadata.Facility.PlanVersionSlug = facilityPlanVersionSlug
			}
			if facilityCode != "" {
				hw.Spec.Metadata.Facility.FacilityCode = facilityCode
			}
		}
	}

	// Update Interfaces
	hw.Spec.Interfaces = []tinkerbell.Interface{}
	interfaceIndex := 0
	for {
		// Check if this interface exists in the form
		macKey := fmt.Sprintf("interfaces[%d].dhcp.mac", interfaceIndex)
		if r.FormValue(macKey) == "" && !hasAnyInterfaceField(r, interfaceIndex) {
			break
		}

		iface := tinkerbell.Interface{}

		// DHCP Configuration
		dhcp := &tinkerbell.DHCP{}
		hasAnyDHCP := false

		if mac := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.mac", interfaceIndex)); mac != "" {
			dhcp.MAC = mac
			hasAnyDHCP = true
		}
		if hostname := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.hostname", interfaceIndex)); hostname != "" {
			dhcp.Hostname = hostname
			hasAnyDHCP = true
		}
		if arch := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.arch", interfaceIndex)); arch != "" {
			dhcp.Arch = arch
			hasAnyDHCP = true
		}

		// IP Configuration
		ipAddress := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.ip.address", interfaceIndex))
		ipNetmask := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.ip.netmask", interfaceIndex))
		ipGateway := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.ip.gateway", interfaceIndex))

		if ipAddress != "" || ipNetmask != "" || ipGateway != "" {
			dhcp.IP = &tinkerbell.IP{
				Address: ipAddress,
				Netmask: ipNetmask,
				Gateway: ipGateway,
			}
			hasAnyDHCP = true
		}

		if leaseTimeStr := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.leaseTime", interfaceIndex)); leaseTimeStr != "" {
			if leaseTime, err := strconv.ParseInt(leaseTimeStr, 10, 64); err == nil {
				dhcp.LeaseTime = leaseTime
				hasAnyDHCP = true
			}
		}

		if vlanID := r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.vlanID", interfaceIndex)); vlanID != "" {
			dhcp.VLANID = vlanID
			hasAnyDHCP = true
		}

		dhcp.UEFI = r.FormValue(fmt.Sprintf("interfaces[%d].dhcp.uefi", interfaceIndex)) == "true"

		if hasAnyDHCP {
			iface.DHCP = dhcp
		}

		// Netboot Configuration
		netboot := &tinkerbell.Netboot{}
		hasAnyNetboot := false

		if r.FormValue(fmt.Sprintf("interfaces[%d].netboot.allowPXE", interfaceIndex)) == "true" {
			allowPXE := true
			netboot.AllowPXE = &allowPXE
			hasAnyNetboot = true
		}

		if r.FormValue(fmt.Sprintf("interfaces[%d].netboot.allowWorkflow", interfaceIndex)) == "true" {
			allowWorkflow := true
			netboot.AllowWorkflow = &allowWorkflow
			hasAnyNetboot = true
		}

		if ipxeURL := r.FormValue(fmt.Sprintf("interfaces[%d].netboot.ipxe.url", interfaceIndex)); ipxeURL != "" {
			netboot.IPXE = &tinkerbell.IPXE{
				URL: ipxeURL,
			}
			hasAnyNetboot = true
		}

		if hasAnyNetboot {
			iface.Netboot = netboot
		}

		iface.DisableDHCP = r.FormValue(fmt.Sprintf("interfaces[%d].disableDHCP", interfaceIndex)) == "true"

		hw.Spec.Interfaces = append(hw.Spec.Interfaces, iface)
		interfaceIndex++
	}

	// Update Disks
	hw.Spec.Disks = []tinkerbell.Disk{}
	diskIndex := 0
	for {
		deviceKey := fmt.Sprintf("disks[%d].device", diskIndex)
		device := r.FormValue(deviceKey)
		if device == "" {
			break
		}

		hw.Spec.Disks = append(hw.Spec.Disks, tinkerbell.Disk{
			Device: device,
		})
		diskIndex++
	}

	return nil
}

// hasAnyMetadataField checks if any metadata field exists in the form
func hasAnyMetadataField(r *http.Request) bool {
	metadataFields := []string{
		"metadata.state", "metadata.bondingMode",
		"metadata.manufacturer.id", "metadata.manufacturer.slug",
		"metadata.instance.id", "metadata.instance.state", "metadata.instance.hostname",
		"metadata.instance.ipxeScriptURL", "metadata.instance.userdata", "metadata.instance.cryptedRootPassword",
		"metadata.instance.allowPxe", "metadata.instance.rescue", "metadata.instance.alwaysPxe", "metadata.instance.networkReady",
		"metadata.instance.operatingSystem.slug", "metadata.instance.operatingSystem.distro",
		"metadata.instance.operatingSystem.version", "metadata.instance.operatingSystem.imageTag",
		"metadata.instance.operatingSystem.osSlug",
		"metadata.facility.planSlug", "metadata.facility.planVersionSlug", "metadata.facility.facilityCode",
	}

	for _, field := range metadataFields {
		if r.FormValue(field) != "" {
			return true
		}
	}
	return false
}

// hasAnyInterfaceField checks if any field for the given interface index exists in the form
func hasAnyInterfaceField(r *http.Request, index int) bool {
	fields := []string{
		"dhcp.hostname", "dhcp.arch", "dhcp.ip.address", "dhcp.ip.netmask", "dhcp.ip.gateway",
		"dhcp.leaseTime", "dhcp.vlanID", "dhcp.uefi", "netboot.allowPXE", "netboot.allowWorkflow",
		"netboot.ipxe.url", "disableDHCP",
	}

	for _, field := range fields {
		if r.FormValue(fmt.Sprintf("interfaces[%d].%s", index, field)) != "" {
			return true
		}
	}
	return false
}
