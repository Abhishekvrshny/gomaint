package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	pb "github.com/abhishekvarshney/gomaint/examples/grpc-service/proto"
)

func main() {
	// Connect to gRPC server
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Create clients
	userClient := pb.NewUserServiceClient(conn)
	healthClient := grpc_health_v1.NewHealthClient(conn)

	ctx := context.Background()

	fmt.Println("=== gRPC User Service Client Demo ===")

	// Test health check
	fmt.Println("\n1. Testing Health Check...")
	testHealthCheck(ctx, healthClient)

	// Test unary RPC
	fmt.Println("\n2. Testing Unary RPC (CreateUser)...")
	testCreateUser(ctx, userClient)

	// Test another unary RPC
	fmt.Println("\n3. Testing Unary RPC (ListUsers)...")
	testListUsers(ctx, userClient)

	// Test server streaming
	fmt.Println("\n4. Testing Server Streaming (StreamUsers)...")
	testStreamUsers(ctx, userClient)

	// Test client streaming
	fmt.Println("\n5. Testing Client Streaming (SubscribeToUserEvents)...")
	testSubscribeToUserEvents(ctx, userClient)

	// Test bidirectional streaming
	fmt.Println("\n6. Testing Bidirectional Streaming (ChatWithUsers)...")
	testChatWithUsers(ctx, userClient)

	fmt.Println("\n=== Demo Complete ===")
}

func testHealthCheck(ctx context.Context, client grpc_health_v1.HealthClient) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		log.Printf("Health check failed: %v", err)
		return
	}

	fmt.Printf("Health status: %s\n", resp.Status.String())
}

func testCreateUser(ctx context.Context, client pb.UserServiceClient) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req := &pb.CreateUserRequest{
		Name:  "Alice Cooper",
		Email: "alice.cooper@example.com",
	}

	resp, err := client.CreateUser(ctx, req)
	if err != nil {
		log.Printf("CreateUser failed: %v", err)
		return
	}

	fmt.Printf("Created user: %s (ID: %s)\n", resp.User.Name, resp.User.Id)
	fmt.Printf("Message: %s\n", resp.Message)
}

func testListUsers(ctx context.Context, client pb.UserServiceClient) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req := &pb.ListUsersRequest{
		Page:       1,
		PageSize:   10,
		ActiveOnly: false,
	}

	resp, err := client.ListUsers(ctx, req)
	if err != nil {
		log.Printf("ListUsers failed: %v", err)
		return
	}

	fmt.Printf("Found %d users (page %d, page size %d):\n", resp.TotalCount, resp.Page, resp.PageSize)
	for i, user := range resp.Users {
		status := "active"
		if !user.Active {
			status = "inactive"
		}
		fmt.Printf("  %d. %s (%s) - %s\n", i+1, user.Name, user.Email, status)
	}
}

func testStreamUsers(ctx context.Context, client pb.UserServiceClient) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req := &pb.StreamUsersRequest{
		ActiveOnly: false,
		BatchSize:  2,
	}

	stream, err := client.StreamUsers(ctx, req)
	if err != nil {
		log.Printf("StreamUsers failed: %v", err)
		return
	}

	batchCount := 0
	userCount := 0

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Stream receive error: %v", err)
			break
		}

		batchCount++
		fmt.Printf("Batch %d (%d users):\n", batchCount, len(resp.Users))
		for _, user := range resp.Users {
			userCount++
			status := "active"
			if !user.Active {
				status = "inactive"
			}
			fmt.Printf("  - %s (%s) - %s\n", user.Name, user.Email, status)
		}

		if resp.IsLastBatch {
			fmt.Printf("Stream complete. Received %d users in %d batches.\n", userCount, batchCount)
			break
		}
	}
}

func testSubscribeToUserEvents(ctx context.Context, client pb.UserServiceClient) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := client.SubscribeToUserEvents(ctx)
	if err != nil {
		log.Printf("SubscribeToUserEvents failed: %v", err)
		return
	}

	// Send multiple user creation requests
	users := []*pb.CreateUserRequest{
		{Name: "Stream User 1", Email: "stream1@example.com"},
		{Name: "Stream User 2", Email: "stream2@example.com"},
		{Name: "Stream User 3", Email: "stream3@example.com"},
	}

	fmt.Printf("Sending %d user creation requests...\n", len(users))
	for i, user := range users {
		if err := stream.Send(user); err != nil {
			log.Printf("Failed to send user %d: %v", i+1, err)
			break
		}
		fmt.Printf("  Sent: %s (%s)\n", user.Name, user.Email)
		time.Sleep(500 * time.Millisecond) // Small delay between sends
	}

	// Close the stream and get response
	_, err = stream.CloseAndRecv()
	if err != nil {
		log.Printf("Failed to close stream: %v", err)
		return
	}

	fmt.Println("Client streaming completed successfully!")
}

func testChatWithUsers(ctx context.Context, client pb.UserServiceClient) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	stream, err := client.ChatWithUsers(ctx)
	if err != nil {
		log.Printf("ChatWithUsers failed: %v", err)
		return
	}

	// Channel to receive responses
	respChan := make(chan *pb.CreateUserResponse, 10)
	errChan := make(chan error, 1)

	// Goroutine to receive responses
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				close(respChan)
				return
			}
			if err != nil {
				errChan <- err
				return
			}
			respChan <- resp
		}
	}()

	// Send requests
	chatUsers := []*pb.CreateUserRequest{
		{Name: "Chat User 1", Email: "chat1@example.com"},
		{Name: "Chat User 2", Email: "chat2@example.com"},
		{Name: "Chat User 3", Email: "chat3@example.com"},
	}

	fmt.Printf("Starting chat with %d user creation requests...\n", len(chatUsers))

	for i, user := range chatUsers {
		// Send request
		if err := stream.Send(user); err != nil {
			log.Printf("Failed to send chat user %d: %v", i+1, err)
			break
		}
		fmt.Printf("  Sent: %s (%s)\n", user.Name, user.Email)

		// Wait for response
		select {
		case resp := <-respChan:
			if resp != nil {
				fmt.Printf("  Received: %s (ID: %s) - %s\n",
					resp.User.Name, resp.User.Id, resp.Message)
			}
		case err := <-errChan:
			log.Printf("Error receiving response: %v", err)
			return
		case <-time.After(2 * time.Second):
			log.Printf("Timeout waiting for response to user %d", i+1)
		}

		time.Sleep(500 * time.Millisecond)
	}

	// Close the send side
	if err := stream.CloseSend(); err != nil {
		log.Printf("Failed to close send: %v", err)
	}

	// Wait for any remaining responses
	for resp := range respChan {
		if resp != nil {
			fmt.Printf("  Final response: %s - %s\n", resp.User.Name, resp.Message)
		}
	}

	fmt.Println("Bidirectional streaming completed!")
}
