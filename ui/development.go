package ui

import (
	"net/http"
	"strings"

	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Mock data for development mode
func getMockHardware() []tinkerbell.Hardware {
	return []tinkerbell.Hardware{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Hardware",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "server-01",
				Namespace: "default",
				Labels: map[string]string{
					"environment": "production",
					"rack":        "rack-01",
				},
			},
			Status: tinkerbell.HardwareStatus{
				State: tinkerbell.HardwareReady,
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Hardware",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "server-02",
				Namespace: "default",
				Labels: map[string]string{
					"environment": "staging",
					"rack":        "rack-02",
				},
			},
			Status: tinkerbell.HardwareStatus{
				State: tinkerbell.HardwareError,
			},
		},
	}
}

func getMockTemplates() []tinkerbell.Template {
	templateData := `version: "0.1"
name: ubuntu-install
global_timeout: 1800
tasks:
  - name: "os-installation"
    worker: "{{.device_1}}"
    volumes:
      - /dev:/dev
      - /dev/console:/dev/console
      - /lib/firmware:/lib/firmware:ro
    actions:
      - name: "stream-ubuntu-image"
        image: quay.io/tinkerbell-actions/image2disk:v1.0.0
        timeout: 600
        environment:
          DEST_DISK: /dev/sda
          IMG_URL: "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"
          COMPRESSED: true`

	return []tinkerbell.Template{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Template",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ubuntu-install",
				Namespace: "default",
				Labels: map[string]string{
					"os": "ubuntu",
				},
			},
			Spec: tinkerbell.TemplateSpec{
				Data: &templateData,
			},
			Status: tinkerbell.TemplateStatus{
				State: tinkerbell.TemplateReady,
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Template",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "centos-install",
				Namespace: "default",
			},
			Status: tinkerbell.TemplateStatus{
				State: tinkerbell.TemplateError,
			},
		},
	}
}

func getMockWorkflows() []tinkerbell.Workflow {
	return []tinkerbell.Workflow{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Workflow",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "provision-server-01",
				Namespace: "default",
			},
			Spec: tinkerbell.WorkflowSpec{
				TemplateRef: "ubuntu-install",
				HardwareRef: "server-01",
			},
			Status: tinkerbell.WorkflowStatus{
				State:             tinkerbell.WorkflowStateRunning,
				TemplateRendering: tinkerbell.TemplateRenderingSuccessful,
				CurrentState: &tinkerbell.CurrentState{
					TaskName:   "os-installation",
					ActionName: "stream-ubuntu-image",
					AgentID:    "agent-01",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "tinkerbell.org/v1alpha1",
				Kind:       "Workflow",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "provision-server-02",
				Namespace: "default",
			},
			Spec: tinkerbell.WorkflowSpec{
				TemplateRef: "centos-install",
				HardwareRef: "server-02",
			},
			Status: tinkerbell.WorkflowStatus{
				State:             tinkerbell.WorkflowStateSuccess,
				TemplateRendering: tinkerbell.TemplateRenderingSuccessful,
			},
		},
	}
}

// Development mode handlers that use mock data
func setupDevelopmentHandlers() http.Handler {
	mux := http.NewServeMux()

	// Register static file handler
	RegisterStatic(mux)

	// Register page handlers
	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/hardware", developmentHardwareListHandler)
	mux.HandleFunc("/hardware/", developmentHardwareDetailHandler)
	mux.HandleFunc("/templates", developmentTemplateListHandler)
	mux.HandleFunc("/templates/", developmentTemplateDetailHandler)
	mux.HandleFunc("/workflows", developmentWorkflowListHandler)
	mux.HandleFunc("/workflows/", developmentWorkflowDetailHandler)

	return mux
}

func developmentHardwareListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	HardwarePage(getMockHardware()).Render(r.Context(), w)
}

func developmentHardwareDetailHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/hardware/")
	hardwareName := strings.Split(path, "/")[0]

	// Find hardware by name
	mockHardware := getMockHardware()
	for _, hw := range mockHardware {
		if hw.Name == hardwareName {
			w.Header().Set("Content-Type", "text/html")
			HardwareDetailPage(hw).Render(r.Context(), w)
			return
		}
	}

	http.NotFound(w, r)
}

func developmentTemplateListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	TemplatePage(getMockTemplates()).Render(r.Context(), w)
}

func developmentTemplateDetailHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/templates/")
	templateName := strings.Split(path, "/")[0]

	// Find template by name
	mockTemplates := getMockTemplates()
	for _, tmpl := range mockTemplates {
		if tmpl.Name == templateName {
			w.Header().Set("Content-Type", "text/html")
			TemplateDetailPage(tmpl).Render(r.Context(), w)
			return
		}
	}

	http.NotFound(w, r)
}

func developmentWorkflowListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	WorkflowPage(getMockWorkflows()).Render(r.Context(), w)
}

func developmentWorkflowDetailHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/workflows/")
	workflowName := strings.Split(path, "/")[0]

	// Find workflow by name
	mockWorkflows := getMockWorkflows()
	for _, wf := range mockWorkflows {
		if wf.Name == workflowName {
			w.Header().Set("Content-Type", "text/html")
			WorkflowDetailPage(wf).Render(r.Context(), w)
			return
		}
	}

	http.NotFound(w, r)
}
