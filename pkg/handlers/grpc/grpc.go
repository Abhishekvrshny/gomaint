package grpc

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// Handler handles gRPC server maintenance mode
type Handler struct {
	*handlers.BaseHandler
	server        *grpc.Server
	listener      net.Listener
	healthServer  *health.Server
	drainTimeout  time.Duration
	inMaintenance int32 // atomic boolean
	wg            sync.WaitGroup
}

// NewGRPCHandler creates a new gRPC handler
func NewGRPCHandler(listener net.Listener, drainTimeout time.Duration) *Handler {
	server := grpc.NewServer(
		grpc.UnaryInterceptor(nil),  // Will be set in wrapUnaryInterceptor
		grpc.StreamInterceptor(nil), // Will be set in wrapStreamInterceptor
	)

	// Create health server
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)

	// Register reflection service for debugging
	reflection.Register(server)

	h := &Handler{
		BaseHandler:  handlers.NewBaseHandler("grpc"),
		server:       server,
		listener:     listener,
		healthServer: healthServer,
		drainTimeout: drainTimeout,
	}

	// Set up interceptors after handler is created
	h.setupInterceptors()

	// Set initial health status
	h.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	return h
}


// GetServer returns the gRPC server instance for service registration
func (h *Handler) GetServer() *grpc.Server {
	return h.server
}

// setupInterceptors configures the gRPC interceptors for maintenance mode handling
func (h *Handler) setupInterceptors() {
	// Create new server with interceptors
	newServer := grpc.NewServer(
		grpc.UnaryInterceptor(h.wrapUnaryInterceptor),
		grpc.StreamInterceptor(h.wrapStreamInterceptor),
	)

	// Re-register health server
	grpc_health_v1.RegisterHealthServer(newServer, h.healthServer)
	reflection.Register(newServer)

	h.server = newServer
}

// OnMaintenanceStart puts the gRPC server into maintenance mode
func (h *Handler) OnMaintenanceStart(ctx context.Context) error {
	atomic.StoreInt32(&h.inMaintenance, 1)
	h.SetState(handlers.StateMaintenance)

	// Update health status to not serving
	h.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Wait for active requests to complete or timeout
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(h.drainTimeout):
		return handlers.ErrDrainTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OnMaintenanceEnd takes the gRPC server out of maintenance mode
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	atomic.StoreInt32(&h.inMaintenance, 0)
	h.SetState(handlers.StateNormal)

	// Update health status to serving
	h.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	return nil
}

// wrapUnaryInterceptor wraps unary RPC calls to implement maintenance mode logic
func (h *Handler) wrapUnaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	// Skip maintenance mode logic for health checks
	if info.FullMethod == "/grpc.health.v1.Health/Check" ||
		info.FullMethod == "/grpc.health.v1.Health/Watch" {
		return handler(ctx, req)
	}

	// Check if in maintenance mode
	if atomic.LoadInt32(&h.inMaintenance) == 1 {
		return nil, handlers.ErrMaintenanceMode
	}

	// Track active requests for graceful draining
	h.wg.Add(1)
	defer h.wg.Done()

	// Call original handler
	return handler(ctx, req)
}

// wrapStreamInterceptor wraps streaming RPC calls to implement maintenance mode logic
func (h *Handler) wrapStreamInterceptor(
	srv interface{},
	stream grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	// Skip maintenance mode logic for health checks
	if info.FullMethod == "/grpc.health.v1.Health/Watch" {
		return handler(srv, stream)
	}

	// Check if in maintenance mode
	if atomic.LoadInt32(&h.inMaintenance) == 1 {
		return handlers.ErrMaintenanceMode
	}

	// Track active streams for graceful draining
	h.wg.Add(1)
	defer h.wg.Done()

	// Call original handler
	return handler(srv, stream)
}

// Start starts the gRPC server
func (h *Handler) Start(ctx context.Context) error {
	go func() {
		if err := h.server.Serve(h.listener); err != nil {
			h.Logger().Errorf("gRPC server serve error: %v", err)
		}
	}()

	h.Logger().Infof("gRPC server started on %s", h.listener.Addr().String())
	return nil
}

// Stop gracefully stops the gRPC server
func (h *Handler) Stop(ctx context.Context) error {
	if h.server == nil {
		return nil
	}

	// Set health status to not serving
	h.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Use graceful stop with context timeout
	stopped := make(chan struct{})
	go func() {
		h.server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		h.Logger().Info("gRPC server stopped gracefully")
		return nil
	case <-ctx.Done():
		h.Logger().Warn("gRPC server graceful stop timeout, forcing stop")
		h.server.Stop()
		return ctx.Err()
	}
}

