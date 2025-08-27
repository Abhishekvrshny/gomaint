package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/abhishekvarshney/gomaint"
	httpHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/http"
)

const (
	defaultPort         = "8080"
	defaultEtcdKey      = "/maintenance/http-service"
	defaultEtcdEndpoint = "localhost:2379"
	defaultDrainTimeout = 30 * time.Second
)

func main() {
	// Get configuration from environment variables
	port := getEnv("HTTP_PORT", defaultPort)
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", defaultEtcdEndpoint)
	etcdKey := getEnv("ETCD_KEY", defaultEtcdKey)

	// Parse drain timeout
	drainTimeoutStr := getEnv("DRAIN_TIMEOUT", "30s")
	drainTimeout, err := time.ParseDuration(drainTimeoutStr)
	if err != nil {
		log.Fatalf("Invalid DRAIN_TIMEOUT: %v", err)
	}

	log.Printf("HTTP server starting on port %s", port)
	log.Printf("Drain timeout: %v", drainTimeout)
	log.Printf("etcd endpoints: %s", etcdEndpoints)
	log.Printf("etcd key: %s", etcdKey)

	// Create HTTP server and routes
	mux := http.NewServeMux()

	// Add routes
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "API is working", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`))
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Create HTTP handler for maintenance mode
	handler := httpHandler.NewHTTPHandler(server, drainTimeout)

	// Skip health check endpoints from maintenance mode
	handler.SkipPaths("/health")

	// Parse etcd endpoints
	endpoints := strings.Split(etcdEndpoints, ",")

	// Start server using the new simplified event source architecture
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr, err := gomaint.StartWithEtcd(ctx, endpoints, etcdKey, drainTimeout, handler)
	if err != nil {
		log.Fatalf("Failed to start maintenance manager: %v", err)
	}
	defer mgr.Stop()

	// Start HTTP server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("HTTP server started successfully")
	log.Println("Monitoring etcd for maintenance mode changes...")
	log.Println("Press Ctrl+C to stop")

	<-sigChan
	log.Println("Shutting down HTTP server...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	} else {
		log.Println("HTTP server stopped gracefully")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
