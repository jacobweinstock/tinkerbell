package main

import (
	"encoding/json"
	"log"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/web"
)

func main() {
	r := gin.Default()

	// Serve static files (CSS, images, etc.)
	r.Static("/artwork", "./artwork")
	r.Static("/css", "./css")

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
		namespaces := []string{"one", "two", "three"}
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")
			namespaces = n
		}

		// Get selected namespace from query parameter
		selectedNamespace := c.Query("namespace")

		// Build kubectl command for hardware - use selected namespace or all namespaces
		var hardwareCmd []string
		if selectedNamespace != "" {
			hardwareCmd = []string{"kubectl", "get", "-n", selectedNamespace, "hardware", "-o", "json"}
		} else {
			hardwareCmd = []string{"kubectl", "get", "-A", "hardware", "-o", "json"}
		}

		var hardware []web.Hardware

		// Fetch hardware from Kubernetes
		if out, err := exec.CommandContext(c.Request.Context(), hardwareCmd[0], hardwareCmd[1:]...).CombinedOutput(); err == nil {
			var hardwareList tinkerbell.HardwareList
			if err := json.Unmarshal(out, &hardwareList); err != nil {
				log.Println("Failed to unmarshal hardware list:", err)
				// Fallback to sample data if parsing fails
				hardware = getSampleHardware()
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
			// Fallback to sample data if kubectl fails
			hardware = getSampleHardware()
		}

		component := web.Homepage(namespaces, hardware)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	log.Println("Starting server on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// Helper function to get sample hardware data as fallback
func getSampleHardware() []web.Hardware {
	return []web.Hardware{
		{
			Name:        "worker-node-01",
			Namespace:   "tinkerbell",
			Description: "Dell PowerEdge R630",
			MAC:         "aa:bb:cc:dd:ee:01",
			IPv4Address: "192.168.1.101",
			Status:      "Online",
			CreatedAt:   "2025-07-21 15:04:05",
		},
		{
			Name:        "worker-node-02",
			Namespace:   "tinkerbell",
			Description: "HP ProLiant DL360",
			MAC:         "aa:bb:cc:dd:ee:02",
			IPv4Address: "192.168.1.102",
			Status:      "Provisioning",
			CreatedAt:   "2025-07-21 15:04:05",
		},
		{
			Name:        "control-plane-01",
			Namespace:   "tinkerbell",
			Description: "Supermicro SYS-6029P",
			MAC:         "aa:bb:cc:dd:ee:03",
			IPv4Address: "192.168.1.100",
			Status:      "Offline",
			CreatedAt:   "2025-07-21 15:04:05",
		},
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
