package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/config"
	"github.com/abhishekvarshney/gomaint/pkg/events"
	"github.com/abhishekvarshney/gomaint/pkg/events/sources"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// Manager is the main maintenance manager that coordinates all handlers
type Manager struct {
	config         *config.Config
	eventManager   *events.EventManager
	etcdSource     *sources.EtcdEventSource
	handlers       map[string]handlers.Handler
	handlersMux    sync.RWMutex
	inMaintenance  bool
	maintenanceMux sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	subscriptionID string
}

// NewManager creates a new maintenance manager
func NewManager(cfg *config.Config) *Manager {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	return &Manager{
		config:       cfg,
		eventManager: events.NewEventManager(),
		handlers:     make(map[string]handlers.Handler),
	}
}

// RegisterHandler registers a new handler with the manager
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
	if err := m.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Create ETCD event source
	etcdSource, err := sources.NewEtcdEventSource("etcd", m.config)
	if err != nil {
		return fmt.Errorf("failed to create etcd event source: %w", err)
	}

	m.etcdSource = etcdSource

	// Add ETCD source to event manager
	if err := m.eventManager.AddSource(etcdSource); err != nil {
		return fmt.Errorf("failed to add etcd source: %w", err)
	}

	// Subscribe to maintenance change events
	subscriptionID, err := m.eventManager.Subscribe(events.EventTypeMaintenanceChange, m.onMaintenanceStateChange)
	if err != nil {
		return fmt.Errorf("failed to subscribe to maintenance events: %w", err)
	}

	m.subscriptionID = subscriptionID

	// Start the event manager
	if err := m.eventManager.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start event manager: %w", err)
	}

	return nil
}

// Stop stops the manager and all handlers
func (m *Manager) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	var lastErr error

	// Unsubscribe from events
	if m.subscriptionID != "" {
		if err := m.eventManager.Unsubscribe(m.subscriptionID); err != nil {
			lastErr = err
		}
	}

	// Stop event manager
	if m.eventManager != nil {
		if err := m.eventManager.Stop(); err != nil {
			lastErr = err
		}
	}

	// If currently in maintenance, try to gracefully exit maintenance mode
	if m.IsInMaintenance() {
		ctx, cancel := context.WithTimeout(context.Background(), m.config.DrainTimeout)
		defer cancel()

		if err := m.exitMaintenanceMode(ctx); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// IsInMaintenance returns true if the service is currently in maintenance mode
func (m *Manager) IsInMaintenance() bool {
	m.maintenanceMux.RLock()
	defer m.maintenanceMux.RUnlock()
	return m.inMaintenance
}

// SetMaintenanceMode manually sets the maintenance mode (updates etcd)
func (m *Manager) SetMaintenanceMode(ctx context.Context, inMaintenance bool) error {
	if m.etcdSource == nil {
		return fmt.Errorf("manager not started")
	}

	return m.etcdSource.SetMaintenanceMode(ctx, inMaintenance)
}

// GetMaintenanceMode gets the current maintenance mode from etcd
func (m *Manager) GetMaintenanceMode(ctx context.Context) (bool, error) {
	if m.etcdSource == nil {
		return false, fmt.Errorf("manager not started")
	}

	return m.etcdSource.GetMaintenanceMode(ctx)
}

// GetEventManager returns the event manager for advanced usage
func (m *Manager) GetEventManager() *events.EventManager {
	return m.eventManager
}

// AddEventSource adds an additional event source to the manager
func (m *Manager) AddEventSource(source events.EventSource) error {
	return m.eventManager.AddSource(source)
}

// RemoveEventSource removes an event source from the manager
func (m *Manager) RemoveEventSource(name string) error {
	return m.eventManager.RemoveSource(name)
}

// ListEventSources returns a list of all registered event source names
func (m *Manager) ListEventSources() []string {
	return m.eventManager.ListSources()
}

// GetEventSourceHealth returns the health status of all event sources
func (m *Manager) GetEventSourceHealth() map[string]bool {
	return m.eventManager.GetSourceHealth()
}

// onMaintenanceStateChange is called when a maintenance state change event is received
func (m *Manager) onMaintenanceStateChange(event events.Event) error {
	// Extract the maintenance state from the event
	inMaintenance, ok := event.Value.(bool)
	if !ok {
		return fmt.Errorf("invalid maintenance state value: %v", event.Value)
	}

	m.maintenanceMux.Lock()
	currentState := m.inMaintenance
	m.inMaintenance = inMaintenance
	m.maintenanceMux.Unlock()

	// No change in state
	if currentState == inMaintenance {
		return nil
	}

	ctx, cancel := context.WithTimeout(m.ctx, m.config.DrainTimeout)
	defer cancel()

	if inMaintenance {
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

// HealthCheck returns the health status of all handlers
func (m *Manager) HealthCheck() map[string]bool {
	m.handlersMux.RLock()
	defer m.handlersMux.RUnlock()

	health := make(map[string]bool)
	for name, handler := range m.handlers {
		health[name] = handler.IsHealthy()
	}

	return health
}

// WaitForMaintenanceMode waits for the service to enter or exit maintenance mode
func (m *Manager) WaitForMaintenanceMode(ctx context.Context, inMaintenance bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if m.IsInMaintenance() == inMaintenance {
				return nil
			}
		}
	}
}
