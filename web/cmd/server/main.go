package main

import (
	"encoding/json"
	"log"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"github.com/tinkerbell/tinkerbell/web"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

		hw := tinkerbell.Hardware{
			ObjectMeta: v1.ObjectMeta{
				Name: "virtual",
			},
			Spec: tinkerbell.HardwareSpec{
				Interfaces: []tinkerbell.Interface{
					{
						DHCP: &tinkerbell.DHCP{},
					},
				},
			},
		}
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "-n", "tinkerbell", "hardware", "-o", "json", "virtual").CombinedOutput(); err == nil {
			if err := json.Unmarshal(out, &hw); err != nil {
				log.Println("Failed to unmarshal hardware:", err)
			}
		}

		// Sample hardware data - this could be fetched from Kubernetes API or other sources
		hardware := []web.Hardware{
			{
				Name:        hw.Name,
				Namespace:   hw.GetNamespace(),
				Description: "Virtual Hardware",
				MAC:         hw.Spec.Interfaces[0].DHCP.MAC,
				IPv4Address: hw.Spec.Interfaces[0].DHCP.IP.Address,
				Status:      "Online",
				CreatedAt:   hw.GetCreationTimestamp().Format("2006-01-02 15:04:05"),
			}, /*
				{
					Name:        "worker-node-01",
					Description: "Dell PowerEdge R630",
					MAC:         "aa:bb:cc:dd:ee:01",
					IPv4Address: "192.168.1.101",
					Status:      "Online",
					CreatedAt:   "2025-07-22T03:08:25Z",
				},
				{
					Name:        "worker-node-02",
					Description: "HP ProLiant DL360",
					MAC:         "aa:bb:cc:dd:ee:02",
					IPv4Address: "192.168.1.102",
					Status:      "Provisioning",
					CreatedAt:   "2025-07-22T03:08:25Z",
				},
				{
					Name:        "control-plane-01",
					Description: "Supermicro SYS-6029P",
					MAC:         "aa:bb:cc:dd:ee:03",
					IPv4Address: "192.168.1.100",
					Status:      "Offline",
					CreatedAt:   "2025-07-22T03:08:25Z",
				},*/
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
