package main

import (
	"log"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
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

	// Dashboard route
	r.GET("/", func(c *gin.Context) {
		namespaces := []string{"one", "two", "three"}
		if out, err := exec.CommandContext(c.Request.Context(), "kubectl", "get", "ns", "-o", "jsonpath='{.items[*].metadata.name}'").CombinedOutput(); err == nil {
			// convert out ([]byte) to []string
			n := strings.Split(strings.ReplaceAll(string(out), "'", ""), " ")
			namespaces = n
		}

		component := web.Dashboard(namespaces)
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	log.Println("Starting server on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
