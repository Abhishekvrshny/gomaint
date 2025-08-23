package server

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/uuid"
	pb "github.com/abhishekvarshney/gomaint/examples/grpc-service/proto"
)

// UserService implements the UserService gRPC interface
type UserService struct {
	pb.UnimplementedUserServiceServer
	users map[string]*pb.User
	mutex sync.RWMutex
}

// NewUserService creates a new UserService instance
func NewUserService() *UserService {
	service := &UserService{
		users: make(map[string]*pb.User),
	}

	// Add some sample users
	service.seedUsers()
	return service
}

// seedUsers adds some initial users for testing
func (s *UserService) seedUsers() {
	now := timestamppb.Now()
	
	sampleUsers := []*pb.User{
		{
			Id:        uuid.New().String(),
			Name:      "John Doe",
			Email:     "john.doe@example.com",
			CreatedAt: now,
			UpdatedAt: now,
			Active:    true,
		},
		{
			Id:        uuid.New().String(),
			Name:      "Jane Smith",
			Email:     "jane.smith@example.com",
			CreatedAt: now,
			UpdatedAt: now,
			Active:    true,
		},
		{
			Id:        uuid.New().String(),
			Name:      "Bob Johnson",
			Email:     "bob.johnson@example.com",
			CreatedAt: now,
			UpdatedAt: now,
			Active:    false,
		},
	}

	for _, user := range sampleUsers {
		s.users[user.Id] = user
	}
}

// CreateUser creates a new user
func (s *UserService) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check for duplicate email
	for _, user := range s.users {
		if user.Email == req.Email {
			return nil, status.Error(codes.AlreadyExists, "user with this email already exists")
		}
	}

	now := timestamppb.Now()
	user := &pb.User{
		Id:        uuid.New().String(),
		Name:      req.Name,
		Email:     req.Email,
		CreatedAt: now,
		UpdatedAt: now,
		Active:    true,
	}

	s.users[user.Id] = user

	return &pb.CreateUserResponse{
		User:    user,
		Message: fmt.Sprintf("User %s created successfully", user.Name),
	}, nil
}

// GetUser retrieves a user by ID
func (s *UserService) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "user ID is required")
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	user, exists := s.users[req.Id]
	if !exists {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	return &pb.GetUserResponse{
		User: user,
	}, nil
}

// UpdateUser updates an existing user
func (s *UserService) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.UpdateUserResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "user ID is required")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	user, exists := s.users[req.Id]
	if !exists {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	// Update fields if provided
	updated := false
	if req.Name != "" && req.Name != user.Name {
		user.Name = req.Name
		updated = true
	}
	if req.Email != "" && req.Email != user.Email {
		// Check for duplicate email
		for id, existingUser := range s.users {
			if id != req.Id && existingUser.Email == req.Email {
				return nil, status.Error(codes.AlreadyExists, "user with this email already exists")
			}
		}
		user.Email = req.Email
		updated = true
	}
	if user.Active != req.Active {
		user.Active = req.Active
		updated = true
	}

	if updated {
		user.UpdatedAt = timestamppb.Now()
	}

	return &pb.UpdateUserResponse{
		User:    user,
		Message: fmt.Sprintf("User %s updated successfully", user.Name),
	}, nil
}

// DeleteUser deletes a user by ID
func (s *UserService) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*pb.DeleteUserResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "user ID is required")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	user, exists := s.users[req.Id]
	if !exists {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	delete(s.users, req.Id)

	return &pb.DeleteUserResponse{
		Message: fmt.Sprintf("User %s deleted successfully", user.Name),
	}, nil
}

// ListUsers lists users with pagination
func (s *UserService) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Set defaults
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100 // Cap at 100
	}

	// Filter users
	var filteredUsers []*pb.User
	for _, user := range s.users {
		if req.ActiveOnly && !user.Active {
			continue
		}
		filteredUsers = append(filteredUsers, user)
	}

	totalCount := int32(len(filteredUsers))
	
	// Paginate
	startIdx := (page - 1) * pageSize
	endIdx := startIdx + pageSize
	
	if startIdx >= totalCount {
		return &pb.ListUsersResponse{
			Users:      []*pb.User{},
			TotalCount: totalCount,
			Page:       page,
			PageSize:   pageSize,
		}, nil
	}
	
	if endIdx > totalCount {
		endIdx = totalCount
	}
	
	pageUsers := filteredUsers[startIdx:endIdx]

	return &pb.ListUsersResponse{
		Users:      pageUsers,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
	}, nil
}

// StreamUsers streams users in batches (server streaming)
func (s *UserService) StreamUsers(req *pb.StreamUsersRequest, stream pb.UserService_StreamUsersServer) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	batchSize := req.BatchSize
	if batchSize < 1 {
		batchSize = 5 // Default batch size
	}
	if batchSize > 50 {
		batchSize = 50 // Cap at 50
	}

	// Filter users
	var users []*pb.User
	for _, user := range s.users {
		if req.ActiveOnly && !user.Active {
			continue
		}
		users = append(users, user)
	}

	// Stream users in batches
	for i := 0; i < len(users); i += int(batchSize) {
		end := i + int(batchSize)
		if end > len(users) {
			end = len(users)
		}

		batch := users[i:end]
		isLastBatch := end >= len(users)

		response := &pb.StreamUsersResponse{
			Users:       batch,
			IsLastBatch: isLastBatch,
		}

		if err := stream.Send(response); err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("failed to send batch: %v", err))
		}

		// Add a small delay to simulate processing
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// SubscribeToUserEvents subscribes to user events (client streaming)
func (s *UserService) SubscribeToUserEvents(stream pb.UserService_SubscribeToUserEventsServer) error {
	var createdUsers []*pb.User

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			// Client finished sending
			break
		}
		if err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("failed to receive: %v", err))
		}

		// Process the user creation request
		s.mutex.Lock()
		now := timestamppb.Now()
		user := &pb.User{
			Id:        uuid.New().String(),
			Name:      req.Name,
			Email:     req.Email,
			CreatedAt: now,
			UpdatedAt: now,
			Active:    true,
		}
		s.users[user.Id] = user
		createdUsers = append(createdUsers, user)
		s.mutex.Unlock()

		// Log the created user (in a real app, this might publish to an event system)
		fmt.Printf("Created user via streaming: %s (%s)\n", user.Name, user.Email)
	}

	fmt.Printf("Client streaming completed. Created %d users\n", len(createdUsers))
	return stream.SendAndClose(&emptypb.Empty{})
}

// ChatWithUsers demonstrates bidirectional streaming
func (s *UserService) ChatWithUsers(stream pb.UserService_ChatWithUsersServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("failed to receive: %v", err))
		}

		// Process the request and create a user
		s.mutex.Lock()
		now := timestamppb.Now()
		user := &pb.User{
			Id:        uuid.New().String(),
			Name:      req.Name,
			Email:     req.Email,
			CreatedAt: now,
			UpdatedAt: now,
			Active:    true,
		}
		s.users[user.Id] = user
		s.mutex.Unlock()

		// Send response back
		response := &pb.CreateUserResponse{
			User:    user,
			Message: fmt.Sprintf("User %s created via chat stream", user.Name),
		}

		if err := stream.Send(response); err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("failed to send response: %v", err))
		}

		fmt.Printf("Chat stream - Created user: %s (%s)\n", user.Name, user.Email)
	}
}