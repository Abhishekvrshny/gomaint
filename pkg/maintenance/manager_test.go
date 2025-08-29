package maintenance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/eventsource"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// Mock event source for testing
type mockEventSource struct {
	startErr          error
	stopErr           error
	setMaintenanceErr error
	getMaintenanceErr error
	eventHandler      eventsource.EventHandler
	started           bool
	stopped           bool
	healthy           bool
	maintenance       bool
	name              string
	sourceType        string
	mu                sync.Mutex
}

func (m *mockEventSource) Start(ctx context.Context, handler eventsource.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startErr != nil {
		return m.startErr
	}

	m.eventHandler = handler
	m.started = true
	return nil
}

func (m *mockEventSource) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopErr != nil {
		return m.stopErr
	}

	m.stopped = true
	return nil
}

func (m *mockEventSource) Name() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.name == "" {
		return "mock-event-source"
	}
	return m.name
}

func (m *mockEventSource) Type() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sourceType == "" {
		return "mock"
	}
	return m.sourceType
}

func (m *mockEventSource) IsHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

func (m *mockEventSource) SetMaintenance(ctx context.Context, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.setMaintenanceErr != nil {
		return m.setMaintenanceErr
	}

	m.maintenance = enabled
	return nil
}

func (m *mockEventSource) GetMaintenance(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getMaintenanceErr != nil {
		return false, m.getMaintenanceErr
	}

	return m.maintenance, nil
}

func (m *mockEventSource) triggerEvent(event eventsource.MaintenanceEvent) error {
	m.mu.Lock()
	handler := m.eventHandler
	m.mu.Unlock()

	if handler != nil {
		return handler(event)
	}
	return nil
}

// Mock handler for testing
type mockHandler struct {
	name        string
	healthy     bool
	startErr    error
	endErr      error
	state       handlers.HandlerState
	maintenance bool
	startCalled int
	endCalled   int
	mu          sync.Mutex
}

func (m *mockHandler) Name() string {
	return m.name
}

func (m *mockHandler) IsHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

func (m *mockHandler) State() handlers.HandlerState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mockHandler) OnMaintenanceStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startCalled++
	m.maintenance = true
	m.state = handlers.StateMaintenance

	return m.startErr
}

func (m *mockHandler) OnMaintenanceEnd(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endCalled++
	m.maintenance = false
	m.state = handlers.StateNormal

	return m.endErr
}

func (m *mockHandler) getCallCounts() (start int, end int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled, m.endCalled
}

// Mock logger for testing
type mockLogger struct {
	logs []string
	mu   sync.Mutex
}

func (m *mockLogger) Info(args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "INFO: "+fmt.Sprint(args...))
}

func (m *mockLogger) Infof(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "INFO: "+fmt.Sprintf(format, args...))
}

func (m *mockLogger) Warn(args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "WARN: "+fmt.Sprint(args...))
}

func (m *mockLogger) Warnf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "WARN: "+fmt.Sprintf(format, args...))
}

func (m *mockLogger) Error(args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "ERROR: "+fmt.Sprint(args...))
}

func (m *mockLogger) Errorf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "ERROR: "+fmt.Sprintf(format, args...))
}

func (m *mockLogger) getLogs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.logs...)
}

func TestNewManager(t *testing.T) {
	eventSource := &mockEventSource{}

	t.Run("with valid drain timeout", func(t *testing.T) {
		manager := NewManager(eventSource, 45*time.Second)

		if manager == nil {
			t.Fatal("NewManager returned nil")
		}

		if manager.eventSource != eventSource {
			t.Error("Event source not set correctly")
		}

		if manager.drainTimeout != 45*time.Second {
			t.Errorf("Expected drain timeout 45s, got %v", manager.drainTimeout)
		}

		if manager.handlers == nil {
			t.Error("Handlers map not initialized")
		}

		if manager.logger == nil {
			t.Error("Logger not initialized")
		}
	})

	t.Run("with zero drain timeout", func(t *testing.T) {
		manager := NewManager(eventSource, 0)

		if manager.drainTimeout != 30*time.Second {
			t.Errorf("Expected default drain timeout 30s, got %v", manager.drainTimeout)
		}
	})

	t.Run("with negative drain timeout", func(t *testing.T) {
		manager := NewManager(eventSource, -5*time.Second)

		if manager.drainTimeout != 30*time.Second {
			t.Errorf("Expected default drain timeout 30s, got %v", manager.drainTimeout)
		}
	})
}

func TestManager_RegisterHandler(t *testing.T) {
	eventSource := &mockEventSource{}
	manager := NewManager(eventSource, 30*time.Second)

	t.Run("register valid handler", func(t *testing.T) {
		handler := &mockHandler{name: "test-handler", healthy: true}

		err := manager.RegisterHandler(handler)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Verify handler is registered
		registered, exists := manager.GetHandler("test-handler")
		if !exists {
			t.Error("Handler not found after registration")
		}

		if registered != handler {
			t.Error("Retrieved handler doesn't match registered handler")
		}
	})

	t.Run("register nil handler", func(t *testing.T) {
		err := manager.RegisterHandler(nil)
		if err == nil {
			t.Error("Expected error when registering nil handler")
		}

		if err.Error() != "handler cannot be nil" {
			t.Errorf("Expected 'handler cannot be nil' error, got %v", err)
		}
	})

	t.Run("register duplicate handler", func(t *testing.T) {
		handler1 := &mockHandler{name: "duplicate-handler", healthy: true}
		handler2 := &mockHandler{name: "duplicate-handler", healthy: true}

		err1 := manager.RegisterHandler(handler1)
		if err1 != nil {
			t.Errorf("Expected no error on first registration, got %v", err1)
		}

		err2 := manager.RegisterHandler(handler2)
		if err2 == nil {
			t.Error("Expected error when registering duplicate handler")
		}

		expectedMsg := "handler with name 'duplicate-handler' already registered"
		if err2.Error() != expectedMsg {
			t.Errorf("Expected '%s' error, got %v", expectedMsg, err2)
		}
	})
}

func TestManager_GetHandler(t *testing.T) {
	eventSource := &mockEventSource{}
	manager := NewManager(eventSource, 30*time.Second)

	handler := &mockHandler{name: "test-handler", healthy: true}
	manager.RegisterHandler(handler)

	t.Run("get existing handler", func(t *testing.T) {
		retrieved, exists := manager.GetHandler("test-handler")
		if !exists {
			t.Error("Expected handler to exist")
		}

		if retrieved != handler {
			t.Error("Retrieved handler doesn't match registered handler")
		}
	})

	t.Run("get non-existing handler", func(t *testing.T) {
		_, exists := manager.GetHandler("non-existing")
		if exists {
			t.Error("Expected handler not to exist")
		}
	})
}

func TestManager_Start(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		ctx := context.Background()
		err := manager.Start(ctx)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if !eventSource.started {
			t.Error("Event source should be started")
		}
	})

	t.Run("start with event source error", func(t *testing.T) {
		eventSource := &mockEventSource{startErr: errors.New("start failed")}
		manager := NewManager(eventSource, 30*time.Second)

		ctx := context.Background()
		err := manager.Start(ctx)

		if err == nil {
			t.Error("Expected error when event source fails to start")
		}

		if !errors.Is(err, eventSource.startErr) {
			t.Errorf("Expected wrapped start error, got %v", err)
		}
	})

	t.Run("start without event source", func(t *testing.T) {
		manager := NewManager(nil, 30*time.Second)

		ctx := context.Background()
		err := manager.Start(ctx)

		if err == nil {
			t.Error("Expected error when event source is nil")
		}

		expectedMsg := "event source not configured"
		if err.Error() != expectedMsg {
			t.Errorf("Expected '%s' error, got %v", expectedMsg, err)
		}
	})

	t.Run("start already started manager", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		ctx := context.Background()

		// Start first time
		err1 := manager.Start(ctx)
		if err1 != nil {
			t.Errorf("Expected no error on first start, got %v", err1)
		}

		// Start second time
		err2 := manager.Start(ctx)
		if err2 == nil {
			t.Error("Expected error when starting already started manager")
		}

		expectedMsg := "manager already started"
		if err2.Error() != expectedMsg {
			t.Errorf("Expected '%s' error, got %v", expectedMsg, err2)
		}
	})
}

func TestManager_Stop(t *testing.T) {
	t.Run("successful stop", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		ctx := context.Background()
		manager.Start(ctx)

		err := manager.Stop()
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if !eventSource.stopped {
			t.Error("Event source should be stopped")
		}
	})

	t.Run("stop not started manager", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		err := manager.Stop()
		if err != nil {
			t.Errorf("Expected no error when stopping not started manager, got %v", err)
		}
	})

	t.Run("stop with event source error", func(t *testing.T) {
		eventSource := &mockEventSource{stopErr: errors.New("stop failed")}
		manager := NewManager(eventSource, 30*time.Second)

		ctx := context.Background()
		manager.Start(ctx)

		err := manager.Stop()
		if err == nil {
			t.Error("Expected error when event source fails to stop")
		}
	})
}

func TestManager_MaintenanceMode(t *testing.T) {
	t.Run("enter maintenance mode", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		handler1 := &mockHandler{name: "handler1", healthy: true}
		handler2 := &mockHandler{name: "handler2", healthy: true}

		manager.RegisterHandler(handler1)
		manager.RegisterHandler(handler2)

		ctx := context.Background()
		manager.Start(ctx)

		// Trigger maintenance enabled event
		event := eventsource.MaintenanceEvent{
			Type:      eventsource.MaintenanceEnabled,
			Timestamp: time.Now(),
		}

		err := eventSource.triggerEvent(event)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Verify maintenance mode is enabled
		if !manager.IsInMaintenance() {
			t.Error("Expected manager to be in maintenance mode")
		}

		// Verify handlers were called
		start1, _ := handler1.getCallCounts()
		start2, _ := handler2.getCallCounts()

		if start1 != 1 {
			t.Errorf("Expected handler1 OnMaintenanceStart called once, got %d", start1)
		}

		if start2 != 1 {
			t.Errorf("Expected handler2 OnMaintenanceStart called once, got %d", start2)
		}
	})

	t.Run("exit maintenance mode", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		handler := &mockHandler{name: "handler", healthy: true}
		manager.RegisterHandler(handler)

		ctx := context.Background()
		manager.Start(ctx)

		// Enter maintenance mode first
		enableEvent := eventsource.MaintenanceEvent{
			Type:      eventsource.MaintenanceEnabled,
			Timestamp: time.Now(),
		}
		eventSource.triggerEvent(enableEvent)

		// Exit maintenance mode
		disableEvent := eventsource.MaintenanceEvent{
			Type:      eventsource.MaintenanceDisabled,
			Timestamp: time.Now(),
		}

		err := eventSource.triggerEvent(disableEvent)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Verify maintenance mode is disabled
		if manager.IsInMaintenance() {
			t.Error("Expected manager not to be in maintenance mode")
		}

		// Verify handlers were called
		start, end := handler.getCallCounts()

		if start != 1 {
			t.Errorf("Expected OnMaintenanceStart called once, got %d", start)
		}

		if end != 1 {
			t.Errorf("Expected OnMaintenanceEnd called once, got %d", end)
		}
	})

	t.Run("handler error during maintenance start", func(t *testing.T) {
		eventSource := &mockEventSource{}
		manager := NewManager(eventSource, 30*time.Second)

		handler := &mockHandler{
			name:     "error-handler",
			healthy:  true,
			startErr: errors.New("start failed"),
		}
		manager.RegisterHandler(handler)

		ctx := context.Background()
		manager.Start(ctx)

		// Trigger maintenance enabled event
		event := eventsource.MaintenanceEvent{
			Type:      eventsource.MaintenanceEnabled,
			Timestamp: time.Now(),
		}

		err := eventSource.triggerEvent(event)
		if err == nil {
			t.Error("Expected error when handler fails")
		}

		// Manager should still be in maintenance mode even if handler fails
		if !manager.IsInMaintenance() {
			t.Error("Expected manager to be in maintenance mode even with handler error")
		}
	})
}

func TestManager_GetHandlerHealth(t *testing.T) {
	eventSource := &mockEventSource{}
	manager := NewManager(eventSource, 30*time.Second)

	handler1 := &mockHandler{name: "healthy-handler", healthy: true}
	handler2 := &mockHandler{name: "unhealthy-handler", healthy: false}

	manager.RegisterHandler(handler1)
	manager.RegisterHandler(handler2)

	health := manager.GetHandlerHealth()

	if len(health) != 2 {
		t.Errorf("Expected 2 handlers in health map, got %d", len(health))
	}

	if !health["healthy-handler"] {
		t.Error("Expected healthy-handler to be healthy")
	}

	if health["unhealthy-handler"] {
		t.Error("Expected unhealthy-handler to be unhealthy")
	}
}

func TestManager_SetLogger(t *testing.T) {
	eventSource := &mockEventSource{}
	manager := NewManager(eventSource, 30*time.Second)

	mockLog := &mockLogger{}
	manager.SetLogger(mockLog)

	// Test that the logger is used
	handler := &mockHandler{name: "test-handler", healthy: true}
	manager.RegisterHandler(handler)

	logs := mockLog.getLogs()
	if len(logs) == 0 {
		t.Error("Expected log messages after registering handler")
	}

	found := false
	for _, log := range logs {
		if log == "INFO: registered handler 'test-handler'" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected registration log message, got logs: %v", logs)
	}
}

func TestManager_SetLogger_Nil(t *testing.T) {
	eventSource := &mockEventSource{}
	manager := NewManager(eventSource, 30*time.Second)

	originalLogger := manager.logger
	manager.SetLogger(nil)

	// Logger should remain unchanged when setting nil
	if manager.logger != originalLogger {
		t.Error("Logger should not change when setting nil logger")
	}
}

func TestManager_Concurrency(t *testing.T) {
	eventSource := &mockEventSource{}
	manager := NewManager(eventSource, 30*time.Second)

	ctx := context.Background()
	manager.Start(ctx)

	// Test concurrent handler registration and health checks
	var wg sync.WaitGroup

	// Register handlers concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			handler := &mockHandler{
				name:    fmt.Sprintf("handler-%d", id),
				healthy: id%2 == 0, // Every other handler is healthy
			}
			manager.RegisterHandler(handler)
		}(i)
	}

	// Check health concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				manager.GetHandlerHealth()
				manager.IsInMaintenance()
			}
		}()
	}

	wg.Wait()

	// Verify all handlers were registered
	health := manager.GetHandlerHealth()
	if len(health) != 10 {
		t.Errorf("Expected 10 handlers, got %d", len(health))
	}
}
