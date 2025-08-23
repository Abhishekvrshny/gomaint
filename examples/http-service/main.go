package main

import (
	"context"
	"fmt"
	"github.com/abhishekvarshney/gomaint"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	httpHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/http"
)

func main() {
	// Create HTTP server
	mux := http.NewServeMux()

	// Add some routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "healthy", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`))
	})

	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Data processed successfully", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`))
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Get etcd endpoints from environment variable or use default
	etcdEndpoints := []string{"localhost:2379"}
	if envEndpoints := os.Getenv("ETCD_ENDPOINTS"); envEndpoints != "" {
		etcdEndpoints = strings.Split(envEndpoints, ",")
	}

	// Create maintenance configuration
	config := gomaint.NewConfig(
		etcdEndpoints,               // etcd endpoints
		"/maintenance/http-service", // etcd key to watch
		30*time.Second,              // drain timeout
	)

	// Create HTTP handler for maintenance
	httpMaintenanceHandler := httpHandler.NewHTTPHandler(server, 30*time.Second)

	// skip maintenance for health check URL
	httpMaintenanceHandler.SkipPaths("/health")

	// Create and start maintenance manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr, err := gomaint.Start(ctx, config, httpMaintenanceHandler)
	if err != nil {
		log.Fatalf("Failed to start maintenance manager: %v", err)
	}
	defer mgr.Stop()

	// Start HTTP server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Print usage information
	fmt.Println("HTTP Service with Maintenance Mode")
	fmt.Println("==================================")
	fmt.Println("Server running on http://localhost:8080")
	fmt.Println("Health check: http://localhost:8080/health")
	fmt.Println("API endpoint: http://localhost:8080/api/data")
	fmt.Println("")
	fmt.Println("To enable maintenance mode:")
	fmt.Println("  etcdctl put /maintenance/http-service true")
	fmt.Println("")
	fmt.Println("To disable maintenance mode:")
	fmt.Println("  etcdctl put /maintenance/http-service false")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop...")

	// Monitor maintenance state changes
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				inMaintenance := mgr.IsInMaintenance()
				health := mgr.HealthCheck()

				log.Printf("Status - Maintenance: %v, Health: %v",
					inMaintenance,
					health["http"])
			}
		}
	}()

	<-sigChan
	log.Println("Shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpMaintenanceHandler.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
