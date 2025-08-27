package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	// Get server address from environment or use default
	serverAddr := os.Getenv("GRPC_SERVER")
	if serverAddr == "" {
		serverAddr = "localhost:50051"
	}

	// Create connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		ctx,
		serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to gRPC server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Create health client
	healthClient := grpc_health_v1.NewHealthClient(conn)

	// Check health
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "", // Empty string checks overall server health
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
		os.Exit(1)
	}

	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		fmt.Fprintf(os.Stderr, "Server is not serving (status: %s)\n", resp.Status.String())
		os.Exit(1)
	}

	fmt.Println("Health check passed: server is healthy")
	os.Exit(0)
}
