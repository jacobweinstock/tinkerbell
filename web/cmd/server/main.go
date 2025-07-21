package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/tinkerbell/tinkerbell/web"
)

func main() {
	r := gin.Default()

	// Serve static files (CSS, images, etc.)
	r.Static("/artwork", "./artwork")
	r.Static("/css", "./")

	// Dashboard route
	r.GET("/", func(c *gin.Context) {
		component := web.Dashboard()
		c.Header("Content-Type", "text/html")
		component.Render(c.Request.Context(), c.Writer)
	})

	log.Println("Starting server on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
