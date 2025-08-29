package http

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/handlers"
	"github.com/abhishekvarshney/gomaint/pkg/set"
)

// Handler handles HTTP server maintenance mode
type Handler struct {
	*handlers.BaseHandler
	server          *http.Server
	skipPaths       *set.Set
	originalHandler http.Handler
	drainTimeout    time.Duration
	inMaintenance   int32 // atomic boolean
	wg              sync.WaitGroup
}

// NewHTTPHandler creates a new HTTP handler
func NewHTTPHandler(server *http.Server, drainTimeout time.Duration) *Handler {
	h := &Handler{
		BaseHandler:     handlers.NewBaseHandler("http"),
		server:          server,
		skipPaths:       set.NewSet(),
		originalHandler: server.Handler,
		drainTimeout:    drainTimeout,
	}

	// Wrap the original handler to track requests
	server.Handler = h.wrapHandler(server.Handler)

	return h
}

// SkipPaths adds paths that should skip maintenance mode logic
func (h *Handler) SkipPaths(paths ...interface{}) {
	h.skipPaths.Add(paths...)
}

// OnMaintenanceStart puts the HTTP server into maintenance mode
func (h *Handler) OnMaintenanceStart(ctx context.Context) error {
	atomic.StoreInt32(&h.inMaintenance, 1)
	h.SetState(handlers.StateMaintenance)

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
		return fmt.Errorf("timeout waiting for requests to drain")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OnMaintenanceEnd takes the HTTP server out of maintenance mode
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	atomic.StoreInt32(&h.inMaintenance, 0)
	h.SetState(handlers.StateNormal)
	return nil
}

// wrapHandler wraps the original handler to implement maintenance mode logic
func (h *Handler) wrapHandler(original http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip maintenance mode logic for health check requests
		if h.skipPaths.Contains(r.URL.Path) {
			if original != nil {
				original.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
			return
		}

		// Check if in maintenance mode
		if atomic.LoadInt32(&h.inMaintenance) == 1 {
			h.writeMaintenanceResponse(w, r)
			return
		}

		// Track active requests for graceful draining
		h.wg.Add(1)
		defer h.wg.Done()

		// Call original handler
		if original != nil {
			original.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
}

// writeMaintenanceResponse writes a maintenance mode response
func (h *Handler) writeMaintenanceResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Set Retry-After based on drain timeout (converted to seconds)
	retryAfter := int(h.drainTimeout.Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	w.WriteHeader(http.StatusServiceUnavailable)

	response := `{
		"error": "Service Unavailable",
		"message": "Service is currently under maintenance. Please try again later.",
		"code": 503
	}`

	w.Write([]byte(response))
}

// IsInMaintenance returns true if the handler is in maintenance mode
func (h *Handler) IsInMaintenance() bool {
	return atomic.LoadInt32(&h.inMaintenance) == 1
}

// GetActiveRequestCount returns the number of active requests
func (h *Handler) GetActiveRequestCount() int {
	// This is a simple approximation - in production you might want a more accurate counter
	return int(atomic.LoadInt32(&h.inMaintenance))
}

// GetStats returns handler statistics
func (h *Handler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"handler_name":   h.Name(),
		"handler_state":  h.State().String(),
		"in_maintenance": atomic.LoadInt32(&h.inMaintenance) == 1,
		"drain_timeout":  h.drainTimeout.String(),
		"server_addr":    h.server.Addr,
	}
}

// Shutdown gracefully shuts down the HTTP server
func (h *Handler) Shutdown(ctx context.Context) error {
	if h.server == nil {
		return nil
	}

	return h.server.Shutdown(ctx)
}
