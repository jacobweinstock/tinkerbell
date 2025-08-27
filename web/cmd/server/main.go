package main

import (
	"encoding/json"
	"log"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/web"
)

const (
	DefaultItemsPerPage = 10
	AllNamespace        = "all"
)

// Helper function to create paginated hardware data
func getPaginatedHardware(hardware []web.Hardware, page, itemsPerPage int) web.HardwarePageData {
	totalItems := len(hardware)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if totalItems == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIndex := (page - 1) * itemsPerPage
	endIndex := startIndex + itemsPerPage

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > totalItems {
		endIndex = totalItems
	}
	if startIndex > totalItems {
		startIndex = totalItems
	}

	var paginatedHardware []web.Hardware
	if startIndex < totalItems && startIndex >= 0 && endIndex >= startIndex {
		paginatedHardware = hardware[startIndex:endIndex]
	}

	startItem := 0
	endItem := 0
	if totalItems > 0 {
		startItem = startIndex + 1
		endItem = endIndex
	}

	return web.HardwarePageData{
		Hardware: paginatedHardware,
		Pagination: web.PaginationData{
			CurrentPage:  page,
			TotalPages:   totalPages,
			TotalItems:   totalItems,
			ItemsPerPage: itemsPerPage,
			StartItem:    startItem,
			EndItem:      endItem,
		},
	}
}

func getPaginatedWorkflows(workflows []web.Workflow, page, itemsPerPage int) web.WorkflowPageData {
	totalItems := len(workflows)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if totalItems == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIndex := (page - 1) * itemsPerPage
	endIndex := startIndex + itemsPerPage

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > totalItems {
		endIndex = totalItems
	}
	if startIndex > totalItems {
		startIndex = totalItems
	}

	var paginatedWorkflows []web.Workflow
	if startIndex < totalItems && startIndex >= 0 && endIndex >= startIndex {
		paginatedWorkflows = workflows[startIndex:endIndex]
	}

	startItem := 0
	endItem := 0
	if totalItems > 0 {
		startItem = startIndex + 1
		endItem = endIndex
	}

	return web.WorkflowPageData{
		Workflows: paginatedWorkflows,
		Pagination: web.PaginationData{
			CurrentPage:  page,
			TotalPages:   totalPages,
			TotalItems:   totalItems,
			ItemsPerPage: itemsPerPage,
			StartItem:    startItem,
			EndItem:      endItem,
		},
	}
}

func getPaginatedTemplates(templates []web.Template, page, itemsPerPage int) web.TemplatePageData {
	totalItems := len(templates)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if totalItems == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIndex := (page - 1) * itemsPerPage
	endIndex := startIndex + itemsPerPage

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > totalItems {
		endIndex = totalItems
	}
	if startIndex > totalItems {
		startIndex = totalItems
	}

	var paginatedTemplates []web.Template
	if startIndex < totalItems && startIndex >= 0 && endIndex >= startIndex {
		paginatedTemplates = templates[startIndex:endIndex]
	}

	startItem := 0
	endItem := 0
	if totalItems > 0 {
		startItem = startIndex + 1
		endItem = endIndex
	}

	return web.TemplatePageData{
		Templates: paginatedTemplates,
		Pagination: web.PaginationData{
			CurrentPage:  page,
			TotalPages:   totalPages,
			TotalItems:   totalItems,
			ItemsPerPage: itemsPerPage,
			StartItem:    startItem,
			EndItem:      endItem,
		},
	}
}

func getPaginatedBMCMachines(machines []web.BMCMachine, page, itemsPerPage int) web.BMCMachinePageData {
	totalItems := len(machines)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if totalItems == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIndex := (page - 1) * itemsPerPage
	endIndex := startIndex + itemsPerPage

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > totalItems {
		endIndex = totalItems
	}
	if startIndex > totalItems {
		startIndex = totalItems
	}

	var paginatedMachines []web.BMCMachine
	if startIndex < totalItems && startIndex >= 0 && endIndex >= startIndex {
		paginatedMachines = machines[startIndex:endIndex]
	}

	startItem := 0
	endItem := 0
	if totalItems > 0 {
		startItem = startIndex + 1
		endItem = endIndex
	}

	return web.BMCMachinePageData{
		Machines: paginatedMachines,
		Pagination: web.PaginationData{
			CurrentPage:  page,
			TotalPages:   totalPages,
			TotalItems:   totalItems,
			ItemsPerPage: itemsPerPage,
			StartItem:    startItem,
			EndItem:      endItem,
		},
	}
}

func getPaginatedBMCJobs(jobs []web.BMCJob, page, itemsPerPage int) web.BMCJobPageData {
	totalItems := len(jobs)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if totalItems == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIndex := (page - 1) * itemsPerPage
	endIndex := startIndex + itemsPerPage

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > totalItems {
		endIndex = totalItems
	}
	if startIndex > totalItems {
		startIndex = totalItems
	}

	var paginatedJobs []web.BMCJob
	if startIndex < totalItems && startIndex >= 0 && endIndex >= startIndex {
		paginatedJobs = jobs[startIndex:endIndex]
	}

	startItem := 0
	endItem := 0
	if totalItems > 0 {
		startItem = startIndex + 1
		endItem = endIndex
	}

	return web.BMCJobPageData{
		Jobs: paginatedJobs,
		Pagination: web.PaginationData{
			CurrentPage:  page,
			TotalPages:   totalPages,
			TotalItems:   totalItems,
			ItemsPerPage: itemsPerPage,
			StartItem:    startItem,
			EndItem:      endItem,
		},
	}
}

func getPaginatedBMCTasks(tasks []web.BMCTask, page, itemsPerPage int) web.BMCTaskPageData {
	totalItems := len(tasks)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if totalItems == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	startIndex := (page - 1) * itemsPerPage
	endIndex := startIndex + itemsPerPage

	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > totalItems {
		endIndex = totalItems
	}
	if startIndex > totalItems {
		startIndex = totalItems
	}

	var paginatedTasks []web.BMCTask
	if startIndex < totalItems && startIndex >= 0 && endIndex >= startIndex {
		paginatedTasks = tasks[startIndex:endIndex]
	}

	startItem := 0
	endItem := 0
	if totalItems > 0 {
		startItem = startIndex + 1
		endItem = endIndex
	}

	return web.BMCTaskPageData{
		Tasks: paginatedTasks,
		Pagination: web.PaginationData{
			CurrentPage:  page,
			TotalPages:   totalPages,
			TotalItems:   totalItems,
			ItemsPerPage: itemsPerPage,
			StartItem:    startItem,
			EndItem:      endItem,
		},
	}
}

func main() {
	r := gin.Default()

	// Serve static files (CSS, images, etc.) - handle requests from any path depth
	r.Static("/artwork", "./artwork")
	r.Static("/css", "./css")

	// Also serve static files from BMC subdirectory paths to handle relative path requests
	r.Static("/bmc/artwork", "./artwork")
	r.Static("/bmc/css", "./css")

	// Favicon routes
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Content-Type", "image/svg+xml")
		c.File("./artwork/Tinkerbell-Icon-Dark.svg")
	})
	r.GET("/favicon.svg", func(c *gin.Context) {
		c.Header("Content-Type", "image/svg+xml")
		c.File("./artwork/Tinkerbell-Icon-Dark.svg")
	})

	// Home page route
	r.GET("/", func(c *gin.Context) {
		namespaces := []string{AllNamespace} // Default to "all" namespace
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := []string{AllNamespace}
			n = append(n, strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")...)
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for hardware - use selected namespace or all namespaces
		var hardwareCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			hardwareCmd = []string{"kubectl", "get", "hardware", "-o", "json", "-A"}
		} else {
			hardwareCmd = []string{"kubectl", "get", "-n", selectedNamespace, "hardware", "-o", "json"}
		}

		var hardware []web.Hardware

		// Fetch hardware from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), hardwareCmd[0], hardwareCmd[1:]...).CombinedOutput(); err == nil {
			var hardwareList tinkerbell.HardwareList
			if err := json.Unmarshal(out, &hardwareList); err != nil {
				log.Println("Failed to unmarshal hardware list:", err)
			} else {
				// Convert Kubernetes hardware to web hardware
				for _, hw := range hardwareList.Items {
					webHw := web.Hardware{
						Name:        hw.Name,
						Namespace:   hw.Namespace,
						Description: getHardwareDescription(hw),
						MAC:         getHardwareMAC(hw),
						IPv4Address: getHardwareIP(hw),
						Status:      getHardwareStatus(hw),
						CreatedAt:   hw.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					hardware = append(hardware, webHw)
				}
			}
		} else {
			log.Printf("Failed to fetch hardware: %v", err)
		}

		// Create paginated hardware data
		hardwarePageData := getPaginatedHardware(hardware, page, itemsPerPage)

		component := web.Homepage(namespaces, hardwarePageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// Hardware data endpoint for htmx updates
	r.GET("/hardware", func(c *gin.Context) {
		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for hardware - use selected namespace or all namespaces
		var hardwareCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			hardwareCmd = []string{"kubectl", "get", "-A", "hardware", "-o", "json"}
		} else {
			hardwareCmd = []string{"kubectl", "get", "-n", selectedNamespace, "hardware", "-o", "json"}
		}

		var hardware []web.Hardware

		// Fetch hardware from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), hardwareCmd[0], hardwareCmd[1:]...).CombinedOutput(); err == nil {
			var hardwareList tinkerbell.HardwareList
			if err := json.Unmarshal(out, &hardwareList); err != nil {
				log.Println("Failed to unmarshal hardware list:", err)
				// Fallback to sample data if parsing fails
			} else {
				// Convert Kubernetes hardware to web hardware
				for _, hw := range hardwareList.Items {
					webHw := web.Hardware{
						Name:        hw.Name,
						Namespace:   hw.Namespace,
						Description: getHardwareDescription(hw),
						MAC:         getHardwareMAC(hw),
						IPv4Address: getHardwareIP(hw),
						Status:      getHardwareStatus(hw),
						CreatedAt:   hw.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					hardware = append(hardware, webHw)
				}
			}
		}

		// Create paginated hardware data
		hardwarePageData := getPaginatedHardware(hardware, page, itemsPerPage)

		// Return just the hardware table content
		component := web.HardwareTableContent(hardwarePageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// Workflows page route
	r.GET("/workflows", func(c *gin.Context) {
		namespaces := []string{AllNamespace} // Default to "all" namespace
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := []string{AllNamespace}
			n = append(n, strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")...)
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for workflows
		var workflowCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			workflowCmd = []string{"kubectl", "get", "-A", "workflows", "-o", "json"}
		} else {
			workflowCmd = []string{"kubectl", "get", "-n", selectedNamespace, "workflows", "-o", "json"}
		}

		var workflows []web.Workflow

		// Fetch workflows from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), workflowCmd[0], workflowCmd[1:]...).CombinedOutput(); err == nil {
			var workflowList tinkerbell.WorkflowList
			if err := json.Unmarshal(out, &workflowList); err != nil {
				log.Println("Failed to unmarshal workflow list:", err)
			} else {
				// Convert Kubernetes workflows to web workflows
				for _, wf := range workflowList.Items {
					webWf := web.Workflow{
						Name:        wf.Name,
						Namespace:   wf.Namespace,
						TemplateRef: wf.Spec.TemplateRef,
						State:       string(wf.Status.State),
						Task:        wf.Status.CurrentState.TaskName,
						Action:      wf.Status.CurrentState.ActionName,
						CreatedAt:   wf.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					workflows = append(workflows, webWf)
				}
			}
		}

		// Create paginated workflow data
		workflowPageData := getPaginatedWorkflows(workflows, page, itemsPerPage)

		component := web.WorkflowPage(namespaces, workflowPageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// Templates page route
	r.GET("/templates", func(c *gin.Context) {
		namespaces := []string{AllNamespace} // Default to "all" namespace
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := []string{AllNamespace}
			n = append(n, strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")...)
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for templates
		var templateCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			templateCmd = []string{"kubectl", "get", "-A", "templates", "-o", "json"}
		} else {
			templateCmd = []string{"kubectl", "get", "-n", selectedNamespace, "templates", "-o", "json"}
		}

		var templates []web.Template

		// Fetch templates from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), templateCmd[0], templateCmd[1:]...).CombinedOutput(); err == nil {
			var templateList tinkerbell.TemplateList
			if err := json.Unmarshal(out, &templateList); err != nil {
				log.Println("Failed to unmarshal template list:", err)
			} else {
				// Convert Kubernetes templates to web templates
				for _, tpl := range templateList.Items {
					data := ""
					if tpl.Spec.Data != nil {
						data = *tpl.Spec.Data
					}
					webTpl := web.Template{
						Name:      tpl.Name,
						Namespace: tpl.Namespace,
						State:     string(tpl.Status.State),
						Data:      data,
						CreatedAt: tpl.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					templates = append(templates, webTpl)
				}
			}
		}

		// Create paginated template data
		templatePageData := getPaginatedTemplates(templates, page, itemsPerPage)

		component := web.TemplatePage(namespaces, templatePageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// BMC Machines page route
	r.GET("/bmc/machines", func(c *gin.Context) {
		namespaces := []string{AllNamespace} // Default to "all" namespace
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := []string{AllNamespace}
			n = append(n, strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")...)
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for BMC machines
		var machineCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			machineCmd = []string{"kubectl", "get", "-A", "machines", "-o", "json"}
		} else {
			machineCmd = []string{"kubectl", "get", "-n", selectedNamespace, "machines", "-o", "json"}
		}

		var machines []web.BMCMachine

		// Fetch BMC machines from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), machineCmd[0], machineCmd[1:]...).CombinedOutput(); err == nil {
			var machineList bmc.MachineList
			if err := json.Unmarshal(out, &machineList); err != nil {
				log.Println("Failed to unmarshal machine list:", err)
			} else {
				// Convert Kubernetes BMC machines to web BMC machines
				for _, machine := range machineList.Items {
					contactable := "Unknown"
					for _, condition := range machine.Status.Conditions {
						if condition.Type == bmc.Contactable {
							contactable = string(condition.Status)
							break
						}
					}

					webMachine := web.BMCMachine{
						Name:        machine.Name,
						Namespace:   machine.Namespace,
						PowerState:  string(machine.Status.Power),
						Contactable: contactable,
						Endpoint:    machine.Spec.Connection.Host,
						CreatedAt:   machine.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					machines = append(machines, webMachine)
				}
			}
		}

		// Create paginated BMC machine data
		machinePageData := getPaginatedBMCMachines(machines, page, itemsPerPage)

		component := web.BMCMachinePage(namespaces, machinePageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// BMC Jobs page route
	r.GET("/bmc/jobs", func(c *gin.Context) {
		namespaces := []string{AllNamespace} // Default to "all" namespace
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := []string{AllNamespace}
			n = append(n, strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")...)
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for BMC jobs
		var jobCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			jobCmd = []string{"kubectl", "get", "-A", "jobs.bmc.tinkerbell.org", "-o", "json"}
		} else {
			jobCmd = []string{"kubectl", "get", "-n", selectedNamespace, "jobs.bmc.tinkerbell.org", "-o", "json"}
		}

		var jobs []web.BMCJob

		// Fetch BMC jobs from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), jobCmd[0], jobCmd[1:]...).CombinedOutput(); err == nil {
			var jobList bmc.JobList
			if err := json.Unmarshal(out, &jobList); err != nil {
				log.Println("Failed to unmarshal job list:", err)
			} else {
				// Convert Kubernetes BMC jobs to web BMC jobs
				for _, job := range jobList.Items {
					status := "Unknown"
					completedAt := ""
					for _, condition := range job.Status.Conditions {
						if condition.Type == bmc.JobCompleted && condition.Status == bmc.ConditionTrue {
							status = "Completed"
							break
						} else if condition.Type == bmc.JobFailed && condition.Status == bmc.ConditionTrue {
							status = "Failed"
							break
						} else if condition.Type == bmc.JobRunning && condition.Status == bmc.ConditionTrue {
							status = "Running"
							break
						}
					}

					webJob := web.BMCJob{
						Name:        job.Name,
						Namespace:   job.Namespace,
						MachineRef:  job.Spec.MachineRef.Namespace + "/" + job.Spec.MachineRef.Name,
						Status:      status,
						CompletedAt: completedAt,
						CreatedAt:   job.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					jobs = append(jobs, webJob)
				}
			}
		}

		// Create paginated BMC job data
		jobPageData := getPaginatedBMCJobs(jobs, page, itemsPerPage)

		component := web.BMCJobPage(namespaces, jobPageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// BMC Tasks page route
	r.GET("/bmc/tasks", func(c *gin.Context) {
		namespaces := []string{AllNamespace} // Default to "all" namespace
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := []string{AllNamespace}
			n = append(n, strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")...)
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for BMC tasks
		var taskCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			taskCmd = []string{"kubectl", "get", "-A", "tasks.bmc.tinkerbell.org", "-o", "json"}
		} else {
			taskCmd = []string{"kubectl", "get", "-n", selectedNamespace, "tasks.bmc.tinkerbell.org", "-o", "json"}
		}

		var tasks []web.BMCTask

		// Fetch BMC tasks from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), taskCmd[0], taskCmd[1:]...).CombinedOutput(); err == nil {
			var taskList bmc.TaskList
			if err := json.Unmarshal(out, &taskList); err != nil {
				log.Println("Failed to unmarshal task list:", err)
			} else {
				// Convert Kubernetes BMC tasks to web BMC tasks
				for _, task := range taskList.Items {
					status := "Unknown"
					completedAt := ""
					for _, condition := range task.Status.Conditions {
						if condition.Type == bmc.TaskCompleted && condition.Status == bmc.ConditionTrue {
							status = "Completed"
							if task.Status.CompletionTime != nil {
								completedAt = task.Status.CompletionTime.Format("2006-01-02 15:04:05")
							}
							break
						} else if condition.Type == bmc.TaskFailed && condition.Status == bmc.ConditionTrue {
							status = "Failed"
							break
						}
					}

					taskType := "Unknown"
					if task.Spec.Task.PowerAction != nil {
						taskType = "Power"
					}

					webTask := web.BMCTask{
						Name:        task.Name,
						Namespace:   task.Namespace,
						JobRef:      "N/A", // Tasks don't have JobRef in the current API
						TaskType:    taskType,
						Status:      status,
						CompletedAt: completedAt,
						CreatedAt:   task.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					tasks = append(tasks, webTask)
				}
			}
		}

		// Create paginated BMC task data
		taskPageData := getPaginatedBMCTasks(tasks, page, itemsPerPage)

		component := web.BMCTaskPage(namespaces, taskPageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// Workflows endpoint
	r.GET("/workflows-data", func(c *gin.Context) {
		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for workflows
		var workflowCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			workflowCmd = []string{"kubectl", "get", "-A", "workflows", "-o", "json"}
		} else {
			workflowCmd = []string{"kubectl", "get", "-n", selectedNamespace, "workflows", "-o", "json"}
		}

		var workflows []web.Workflow

		// Fetch workflows from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), workflowCmd[0], workflowCmd[1:]...).CombinedOutput(); err == nil {
			var workflowList tinkerbell.WorkflowList
			if err := json.Unmarshal(out, &workflowList); err != nil {
				log.Println("Failed to unmarshal workflow list:", err)
			} else {
				// Convert Kubernetes workflows to web workflows
				for _, wf := range workflowList.Items {
					webWf := web.Workflow{
						Name:        wf.Name,
						Namespace:   wf.Namespace,
						TemplateRef: wf.Spec.TemplateRef,
						State:       string(wf.Status.State),
						Task:        wf.Status.CurrentState.TaskName,
						Action:      wf.Status.CurrentState.ActionName,
						CreatedAt:   wf.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					workflows = append(workflows, webWf)
				}
			}
		}

		// Create paginated workflow data
		workflowPageData := getPaginatedWorkflows(workflows, page, itemsPerPage)

		// Return workflow table content
		component := web.WorkflowTableContent(workflowPageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// Templates endpoint
	r.GET("/templates-data", func(c *gin.Context) {
		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for templates
		var templateCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			templateCmd = []string{"kubectl", "get", "-A", "templates", "-o", "json"}
		} else {
			templateCmd = []string{"kubectl", "get", "-n", selectedNamespace, "templates", "-o", "json"}
		}

		var templates []web.Template

		// Fetch templates from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), templateCmd[0], templateCmd[1:]...).CombinedOutput(); err == nil {
			var templateList tinkerbell.TemplateList
			if err := json.Unmarshal(out, &templateList); err != nil {
				log.Println("Failed to unmarshal template list:", err)
			} else {
				// Convert Kubernetes templates to web templates
				for _, tpl := range templateList.Items {
					data := ""
					if tpl.Spec.Data != nil {
						data = *tpl.Spec.Data
					}
					webTpl := web.Template{
						Name:      tpl.Name,
						Namespace: tpl.Namespace,
						State:     string(tpl.Status.State),
						Data:      data,
						CreatedAt: tpl.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					templates = append(templates, webTpl)
				}
			}
		}

		// Create paginated template data
		templatePageData := getPaginatedTemplates(templates, page, itemsPerPage)

		// Return template table content
		component := web.TemplateTableContent(templatePageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// BMC Machines endpoint
	r.GET("/bmc/machines-data", func(c *gin.Context) {
		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for BMC machines
		var machineCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			machineCmd = []string{"kubectl", "get", "-A", "machines", "-o", "json"}
		} else {
			machineCmd = []string{"kubectl", "get", "-n", selectedNamespace, "machines", "-o", "json"}
		}

		var machines []web.BMCMachine

		// Fetch BMC machines from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), machineCmd[0], machineCmd[1:]...).CombinedOutput(); err == nil {
			var machineList bmc.MachineList
			if err := json.Unmarshal(out, &machineList); err != nil {
				log.Println("Failed to unmarshal machine list:", err)
			} else {
				// Convert Kubernetes BMC machines to web BMC machines
				for _, machine := range machineList.Items {
					contactable := "Unknown"
					for _, condition := range machine.Status.Conditions {
						if condition.Type == bmc.Contactable {
							contactable = string(condition.Status)
							break
						}
					}

					webMachine := web.BMCMachine{
						Name:        machine.Name,
						Namespace:   machine.Namespace,
						PowerState:  string(machine.Status.Power),
						Contactable: contactable,
						Endpoint:    machine.Spec.Connection.Host,
						CreatedAt:   machine.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					machines = append(machines, webMachine)
				}
			}
		}

		// Create paginated BMC machine data
		machinePageData := getPaginatedBMCMachines(machines, page, itemsPerPage)

		// Return BMC machine table content
		component := web.BMCMachineTableContent(machinePageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// BMC Jobs endpoint
	r.GET("/bmc/jobs-data", func(c *gin.Context) {
		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for BMC jobs
		var jobCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			jobCmd = []string{"kubectl", "get", "-A", "jobs.bmc.tinkerbell.org", "-o", "json"}
		} else {
			jobCmd = []string{"kubectl", "get", "-n", selectedNamespace, "jobs.bmc.tinkerbell.org", "-o", "json"}
		}

		var jobs []web.BMCJob

		// Fetch BMC jobs from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), jobCmd[0], jobCmd[1:]...).CombinedOutput(); err == nil {
			var jobList bmc.JobList
			if err := json.Unmarshal(out, &jobList); err != nil {
				log.Println("Failed to unmarshal job list:", err)
			} else {
				// Convert Kubernetes BMC jobs to web BMC jobs
				for _, job := range jobList.Items {
					status := "Unknown"
					completedAt := ""
					for _, condition := range job.Status.Conditions {
						if condition.Type == bmc.JobCompleted && condition.Status == bmc.ConditionTrue {
							status = "Completed"
							break
						} else if condition.Type == bmc.JobFailed && condition.Status == bmc.ConditionTrue {
							status = "Failed"
							break
						} else if condition.Type == bmc.JobRunning && condition.Status == bmc.ConditionTrue {
							status = "Running"
							break
						}
					}

					webJob := web.BMCJob{
						Name:        job.Name,
						Namespace:   job.Namespace,
						MachineRef:  job.Spec.MachineRef.Namespace + "/" + job.Spec.MachineRef.Name,
						Status:      status,
						CompletedAt: completedAt,
						CreatedAt:   job.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					jobs = append(jobs, webJob)
				}
			}
		}

		// Create paginated BMC job data
		jobPageData := getPaginatedBMCJobs(jobs, page, itemsPerPage)

		// Return BMC job table content
		component := web.BMCJobTableContent(jobPageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	// BMC Tasks endpoint
	r.GET("/bmc/tasks-data", func(c *gin.Context) {
		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Get pagination parameters
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			page = 1
		}

		itemsPerPageStr := c.DefaultQuery("per_page", strconv.Itoa(DefaultItemsPerPage))
		itemsPerPage, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || itemsPerPage < 1 {
			itemsPerPage = DefaultItemsPerPage
		}

		// Build kubectl command for BMC tasks
		var taskCmd []string
		if selectedNamespace == "" || selectedNamespace == AllNamespace {
			taskCmd = []string{"kubectl", "get", "-A", "tasks.bmc.tinkerbell.org", "-o", "json"}
		} else {
			taskCmd = []string{"kubectl", "get", "-n", selectedNamespace, "tasks.bmc.tinkerbell.org", "-o", "json"}
		}

		var tasks []web.BMCTask

		// Fetch BMC tasks from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), taskCmd[0], taskCmd[1:]...).CombinedOutput(); err == nil {
			var taskList bmc.TaskList
			if err := json.Unmarshal(out, &taskList); err != nil {
				log.Println("Failed to unmarshal task list:", err)
			} else {
				// Convert Kubernetes BMC tasks to web BMC tasks
				for _, task := range taskList.Items {
					status := "Unknown"
					completedAt := ""
					for _, condition := range task.Status.Conditions {
						if condition.Type == bmc.TaskCompleted && condition.Status == bmc.ConditionTrue {
							status = "Completed"
							if task.Status.CompletionTime != nil {
								completedAt = task.Status.CompletionTime.Format("2006-01-02 15:04:05")
							}
							break
						} else if condition.Type == bmc.TaskFailed && condition.Status == bmc.ConditionTrue {
							status = "Failed"
							break
						}
					}

					taskType := "Unknown"
					if task.Spec.Task.PowerAction != nil {
						taskType = "Power"
					}

					webTask := web.BMCTask{
						Name:        task.Name,
						Namespace:   task.Namespace,
						JobRef:      "N/A", // Tasks don't have JobRef in the current API
						TaskType:    taskType,
						Status:      status,
						CompletedAt: completedAt,
						CreatedAt:   task.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
					}
					tasks = append(tasks, webTask)
				}
			}
		}

		// Create paginated BMC task data
		taskPageData := getPaginatedBMCTasks(tasks, page, itemsPerPage)

		// Return BMC task table content
		component := web.BMCTaskTableContent(taskPageData)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	log.Println("Starting server on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// Helper function to extract hardware description from hardware spec
func getHardwareDescription(hw tinkerbell.Hardware) string {
	// Try to get description from annotations or metadata
	if hw.Annotations != nil {
		if desc, exists := hw.Annotations["description"]; exists {
			return desc
		}
	}
	// Fallback to hardware name or generic description
	return "Hardware Resource"
}

// Helper function to extract MAC address from hardware interfaces
func getHardwareMAC(hw tinkerbell.Hardware) string {
	if len(hw.Spec.Interfaces) > 0 && hw.Spec.Interfaces[0].DHCP != nil {
		return hw.Spec.Interfaces[0].DHCP.MAC
	}
	return "N/A"
}

// Helper function to extract IP address from hardware interfaces
func getHardwareIP(hw tinkerbell.Hardware) string {
	if len(hw.Spec.Interfaces) > 0 && hw.Spec.Interfaces[0].DHCP != nil && hw.Spec.Interfaces[0].DHCP.IP != nil {
		return hw.Spec.Interfaces[0].DHCP.IP.Address
	}
	return "N/A"
}

// Helper function to determine hardware status
func getHardwareStatus(hw tinkerbell.Hardware) string {
	// Check if hardware has any conditions or status indicators
	// This is a simplified implementation - you might want to check actual hardware status
	if hw.Status.State == "provisioning" {
		return "Provisioning"
	}
	if hw.Status.State == "failed" {
		return "Offline"
	}
	// Default to online if no specific status is set
	return "Online"
}
