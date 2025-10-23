package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	fmt.Println("SSW Log Capturer Go - Minimal Version Starting...")
	fmt.Println("Version: v1.0.0-minimal")
	fmt.Println("Build Date:", time.Now().Format("2006-01-02 15:04:05"))

	// Start HTTP server for health checks
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"status": "healthy",
			"version": "v1.0.0-minimal",
			"timestamp": "` + time.Now().Format(time.RFC3339) + `",
			"uptime": "` + time.Since(startTime).String() + `",
			"message": "Minimal version - monitoring and processing temporarily disabled for deployment testing"
		}`))
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"processed": 0,
			"failed": 0,
			"version": "minimal",
			"note": "Statistics temporarily disabled for deployment testing"
		}`))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
			<h1>SSW Log Capturer Go - Minimal Version</h1>
			<p>Status: Running (Minimal Mode)</p>
			<p>This is a temporary minimal version for deployment testing.</p>
			<ul>
				<li><a href="/health">Health Check</a></li>
				<li><a href="/stats">Statistics</a></li>
			</ul>
		`))
	})

	startTime = time.Now()

	go func() {
		fmt.Println("Starting HTTP server on :8080...")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	fmt.Println("Application started successfully in minimal mode")
	fmt.Println("HTTP server running on http://localhost:8080")
	fmt.Println("Health check available at http://localhost:8080/health")

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("Shutting down gracefully...")
	time.Sleep(1 * time.Second)
	fmt.Println("Shutdown complete")
}

var startTime time.Time