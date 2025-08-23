package maintenance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/eventsource"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// Manager coordinates maintenance mode across multiple handlers using a single event source
type Manager struct {
	eventSource   eventsource.EventSource
	handlers      map[string]handlers.Handler
	handlersMux   sync.RWMutex
	inMaintenance bool
	maintenanceMux sync.RWMutex
	drainTimeout  time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	started       bool
	startedMux    sync.RWMutex
}

// NewManager creates a new maintenance manager with the specified event source
func NewManager(eventSource eventsource.EventSource, drainTimeout time.Duration) *Manager {
	if drainTimeout <= 0 {
		drainTimeout = 30 * time.Second
	}
	
	return &Manager{
		eventSource:  eventSource,
		handlers:     make(map[string]handlers.Handler),
		drainTimeout: drainTimeout,
	}
}

// RegisterHandler registers a handler with the manager
func (m *Manager) RegisterHandler(handler handlers.Handler) error {
	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}
	
	m.handlersMux.Lock()
	defer m.handlersMux.Unlock()
	
	name := handler.Name()
	if _, exists := m.handlers[name]; exists {
		return fmt.Errorf("handler with name '%s' already registered", name)
	}
	
	m.handlers[name] = handler
	return nil
}

// UnregisterHandler removes a handler from the manager
func (m *Manager) UnregisterHandler(name string) error {
	m.handlersMux.Lock()
	defer m.handlersMux.Unlock()
	
	if _, exists := m.handlers[name]; !exists {
		return fmt.Errorf("handler with name '%s' not found", name)
	}
	
	delete(m.handlers, name)
	return nil
}

// GetHandler returns a handler by name
func (m *Manager) GetHandler(name string) (handlers.Handler, bool) {
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()
	
	handler, exists := m.handlers[name]
	return handler, exists
}

// ListHandlers returns a list of all registered handler names
func (m *Manager) ListHandlers() []string {
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()
	
	names := make([]string, 0, len(m.handlers))
	for name := range m.handlers {
		names = append(names, name)
	}
	return names
}

// Start begins monitoring for maintenance state changes
func (m *Manager) Start(ctx context.Context) error {
	m.startedMux.Lock()
	defer m.startedMux.Unlock()
	
	if m.started {
		return fmt.Errorf("manager already started")
	}
	
	if m.eventSource == nil {
		return fmt.Errorf("event source not configured")
	}
	
	m.ctx, m.cancel = context.WithCancel(ctx)
	
	// Start the event source with our handler
	if err := m.eventSource.Start(m.ctx, m.handleMaintenanceEvent); err != nil {
		return fmt.Errorf("failed to start event source: %w", err)
	}
	
	m.started = true
	return nil
}

// Stop stops the manager and all handlers
func (m *Manager) Stop() error {
	m.startedMux.Lock()
	defer m.startedMux.Unlock()
	
	if !m.started {
		return nil
	}
	
	var lastErr error
	
	// Cancel context first
	if m.cancel != nil {
		m.cancel()
	}
	
	// Stop the event source
	if m.eventSource != nil {
		if err := m.eventSource.Stop(); err != nil {
			lastErr = fmt.Errorf("failed to stop event source: %w", err)
		}
	}
	
	// If currently in maintenance, try to gracefully exit maintenance mode
	if m.IsInMaintenance() {
		ctx, cancel := context.WithTimeout(context.Background(), m.drainTimeout)
		defer cancel()
		
		if err := m.exitMaintenanceMode(ctx); err != nil {
			lastErr = fmt.Errorf("failed to exit maintenance mode: %w", err)
		}
	}
	
	m.started = false
	return lastErr
}

// IsInMaintenance returns true if the service is currently in maintenance mode
func (m *Manager) IsInMaintenance() bool {
	m.maintenanceMux.RLock()
	defer m.maintenanceMux.RUnlock()
	return m.inMaintenance
}

// SetMaintenance manually sets the maintenance mode (updates the event source)
func (m *Manager) SetMaintenance(ctx context.Context, enabled bool) error {
	if m.eventSource == nil {
		return fmt.Errorf("event source not configured")
	}
	
	return m.eventSource.SetMaintenance(ctx, enabled)
}

// GetMaintenance gets the current maintenance mode from the event source
func (m *Manager) GetMaintenance(ctx context.Context) (bool, error) {
	if m.eventSource == nil {
		return false, fmt.Errorf("event source not configured")
	}
	
	return m.eventSource.GetMaintenance(ctx)
}

// IsHealthy returns true if the event source and all handlers are healthy
func (m *Manager) IsHealthy() bool {
	// Check event source health
	if m.eventSource != nil && !m.eventSource.IsHealthy() {
		return false
	}
	
	// Check all handlers
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()
	
	for _, handler := range m.handlers {
		if !handler.IsHealthy() {
			return false
		}
	}
	
	return true
}

// GetEventSource returns the event source for advanced usage
func (m *Manager) GetEventSource() eventsource.EventSource {
	return m.eventSource
}

// GetHandlerHealth returns the health status of all handlers
func (m *Manager) GetHandlerHealth() map[string]bool {
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()
	
	health := make(map[string]bool)
	for name, handler := range m.handlers {
		health[name] = handler.IsHealthy()
	}
	return health
}

// WaitForMaintenance waits for the service to enter or exit maintenance mode
func (m *Manager) WaitForMaintenance(ctx context.Context, enabled bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if m.IsInMaintenance() == enabled {
				return nil
			}
		}
	}
}

// handleMaintenanceEvent handles maintenance events from the event source
func (m *Manager) handleMaintenanceEvent(event eventsource.MaintenanceEvent) error {
	enabled := event.Type == eventsource.MaintenanceEnabled
	
	m.maintenanceMux.Lock()
	currentState := m.inMaintenance
	m.inMaintenance = enabled
	m.maintenanceMux.Unlock()
	
	// No state change, nothing to do
	if currentState == enabled {
		return nil
	}
	
	ctx, cancel := context.WithTimeout(m.ctx, m.drainTimeout)
	defer cancel()
	
	if enabled {
		return m.enterMaintenanceMode(ctx)
	} else {
		return m.exitMaintenanceMode(ctx)
	}
}

// enterMaintenanceMode puts all handlers into maintenance mode
func (m *Manager) enterMaintenanceMode(ctx context.Context) error {
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()
	
	var errors []error
	
	for name, handler := range m.handlers {
		if err := handler.OnMaintenanceStart(ctx); err != nil {
			errors = append(errors, fmt.Errorf("handler '%s': %w", name, err))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("failed to enter maintenance mode for some handlers: %v", errors)
	}
	
	return nil
}

// exitMaintenanceMode takes all handlers out of maintenance mode
func (m *Manager) exitMaintenanceMode(ctx context.Context) error {
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()
	
	var errors []error
	
	for name, handler := range m.handlers {
		if err := handler.OnMaintenanceEnd(ctx); err != nil {
			errors = append(errors, fmt.Errorf("handler '%s': %w", name, err))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("failed to exit maintenance mode for some handlers: %v", errors)
	}
	
	return nil
}