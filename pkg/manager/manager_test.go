package manager

import (
	"context"
	"testing"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/config"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// MockHandler for testing
type MockHandler struct {
	*handlers.BaseHandler
	startCalled bool
	endCalled   bool
}

func NewMockHandler(name string) *MockHandler {
	return &MockHandler{
		BaseHandler: handlers.NewBaseHandler(name),
	}
}

func (m *MockHandler) OnMaintenanceStart(ctx context.Context) error {
	m.startCalled = true
	m.SetState(handlers.StateMaintenance)
	return nil
}

func (m *MockHandler) OnMaintenanceEnd(ctx context.Context) error {
	m.endCalled = true
	m.SetState(handlers.StateNormal)
	return nil
}

func TestNewManager(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := NewManager(cfg)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.config != cfg {
		t.Error("Manager config not set correctly")
	}
}

func TestRegisterHandler(t *testing.T) {
	mgr := NewManager(nil)
	handler := NewMockHandler("test")

	err := mgr.RegisterHandler(handler)
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Test duplicate registration
	err = mgr.RegisterHandler(handler)
	if err == nil {
		t.Error("Expected error for duplicate handler registration")
	}

	// Test nil handler
	err = mgr.RegisterHandler(nil)
	if err == nil {
		t.Error("Expected error for nil handler")
	}
}

func TestUnregisterHandler(t *testing.T) {
	mgr := NewManager(nil)
	handler := NewMockHandler("test")

	// Register first
	err := mgr.RegisterHandler(handler)
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Unregister
	err = mgr.UnregisterHandler("test")
	if err != nil {
		t.Fatalf("Failed to unregister handler: %v", err)
	}

	// Test unregistering non-existent handler
	err = mgr.UnregisterHandler("nonexistent")
	if err == nil {
		t.Error("Expected error for unregistering non-existent handler")
	}
}

func TestGetHandler(t *testing.T) {
	mgr := NewManager(nil)
	handler := NewMockHandler("test")

	// Test getting non-existent handler
	_, exists := mgr.GetHandler("test")
	if exists {
		t.Error("Expected handler to not exist")
	}

	// Register and test getting existing handler
	mgr.RegisterHandler(handler)
	retrieved, exists := mgr.GetHandler("test")
	if !exists {
		t.Error("Expected handler to exist")
	}
	if retrieved != handler {
		t.Error("Retrieved handler is not the same as registered")
	}
}

func TestListHandlers(t *testing.T) {
	mgr := NewManager(nil)

	// Test empty list
	handlers := mgr.ListHandlers()
	if len(handlers) != 0 {
		t.Error("Expected empty handler list")
	}

	// Add handlers
	handler1 := NewMockHandler("test1")
	handler2 := NewMockHandler("test2")

	mgr.RegisterHandler(handler1)
	mgr.RegisterHandler(handler2)

	handlers = mgr.ListHandlers()
	if len(handlers) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(handlers))
	}

	// Check if both handlers are in the list
	found1, found2 := false, false
	for _, name := range handlers {
		if name == "test1" {
			found1 = true
		}
		if name == "test2" {
			found2 = true
		}
	}

	if !found1 || !found2 {
		t.Error("Not all registered handlers found in list")
	}
}

func TestIsInMaintenance(t *testing.T) {
	mgr := NewManager(nil)

	// Initially should not be in maintenance
	if mgr.IsInMaintenance() {
		t.Error("Manager should not be in maintenance initially")
	}
}

func TestHealthCheck(t *testing.T) {
	mgr := NewManager(nil)
	handler := NewMockHandler("test")

	mgr.RegisterHandler(handler)

	health := mgr.HealthCheck()
	if len(health) != 1 {
		t.Errorf("Expected 1 health check result, got %d", len(health))
	}

	if !health["test"] {
		t.Error("Handler should be healthy initially")
	}
}

func TestWaitForMaintenanceMode(t *testing.T) {
	mgr := NewManager(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return immediately since already in normal mode
	err := mgr.WaitForMaintenanceMode(ctx, false, time.Second)
	if err != nil {
		t.Errorf("WaitForMaintenanceMode failed: %v", err)
	}

	// Should timeout waiting for maintenance mode
	err = mgr.WaitForMaintenanceMode(ctx, true, time.Second)
	if err == nil {
		t.Error("Expected timeout error")
	}
}
