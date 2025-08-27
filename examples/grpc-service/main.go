package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	pb "github.com/abhishekvarshney/gomaint/examples/grpc-service/proto"
	"github.com/abhishekvarshney/gomaint/examples/grpc-service/server"
	grpchandler "github.com/abhishekvarshney/gomaint/pkg/handlers/grpc"
)

const (
	defaultPort         = "50051"
	defaultEtcdKey      = "/maintenance/grpc-service"
	defaultEtcdEndpoint = "localhost:2379"
	defaultDrainTimeout = 30 * time.Second
)

func main() {
	// Get configuration from environment variables
	port := getEnv("GRPC_PORT", defaultPort)
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", defaultEtcdEndpoint)
	etcdKey := getEnv("ETCD_KEY", defaultEtcdKey)

	// Parse drain timeout
	drainTimeoutStr := getEnv("DRAIN_TIMEOUT", "30s")
	drainTimeout, err := time.ParseDuration(drainTimeoutStr)
	if err != nil {
		log.Fatalf("Invalid DRAIN_TIMEOUT: %v", err)
	}

	// Create TCP listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("gRPC server starting on port %s", port)
	log.Printf("Drain timeout: %v", drainTimeout)
	log.Printf("etcd endpoints: %s", etcdEndpoints)
	log.Printf("etcd key: %s", etcdKey)

	// Create gRPC maintenance handler
	handler := grpchandler.NewGRPCHandler(lis, drainTimeout)

	// Create and register the user service
	userService := server.NewUserService()
	pb.RegisterUserServiceServer(handler.GetServer(), userService)

	// Set up etcd client and watcher
	endpoints := strings.Split(etcdEndpoints, ",")
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create etcd client: %v", err)
	}
	defer etcdClient.Close()

	// Start the server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.Start(ctx); err != nil {
		log.Fatalf("Failed to start gRPC handler: %v", err)
	}

	// Start monitoring etcd for maintenance mode changes
	go monitorMaintenanceMode(ctx, etcdClient, etcdKey, handler)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("gRPC server started successfully")
	log.Println("Monitoring etcd for maintenance mode changes...")
	log.Println("Press Ctrl+C to stop")

	<-sigChan
	log.Println("Shutting down gRPC server...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := handler.Stop(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	} else {
		log.Println("Server stopped gracefully")
	}
}

func monitorMaintenanceMode(ctx context.Context, client *clientv3.Client, key string, handler *grpchandler.Handler) {
	watchChan := client.Watch(ctx, key)

	for {
		select {
		case <-ctx.Done():
			return
		case watchResp := <-watchChan:
			for _, event := range watchResp.Events {
				value := string(event.Kv.Value)
				log.Printf("etcd event: %s = %s", string(event.Kv.Key), value)

				switch strings.ToLower(value) {
				case "true", "1", "yes", "on":
					log.Println("Entering maintenance mode...")
					if err := handler.OnMaintenanceStart(ctx); err != nil {
						log.Printf("Error entering maintenance mode: %v", err)
					} else {
						log.Println("Successfully entered maintenance mode")
					}
				case "false", "0", "no", "off", "":
					log.Println("Exiting maintenance mode...")
					if err := handler.OnMaintenanceEnd(ctx); err != nil {
						log.Printf("Error exiting maintenance mode: %v", err)
					} else {
						log.Println("Successfully exited maintenance mode")
					}
				default:
					log.Printf("Unknown maintenance mode value: %s", value)
				}
			}
		}
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
