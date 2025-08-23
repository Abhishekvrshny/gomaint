package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventManager manages multiple event sources and coordinates event distribution
type EventManager struct {
	sources       map[string]EventSource
	subscriptions map[string]*Subscription
	sourcesMux    sync.RWMutex
	subsMux       sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	started       bool
}

// NewEventManager creates a new event manager
func NewEventManager() *EventManager {
	return &EventManager{
		sources:       make(map[string]EventSource),
		subscriptions: make(map[string]*Subscription),
	}
}

// AddSource adds an event source to the manager
func (em *EventManager) AddSource(source EventSource) error {
	if source == nil {
		return fmt.Errorf("source cannot be nil")
	}

	em.sourcesMux.Lock()
	defer em.sourcesMux.Unlock()

	name := source.Name()
	if _, exists := em.sources[name]; exists {
		return fmt.Errorf("source with name '%s' already exists", name)
	}

	em.sources[name] = source
	return nil
}

// RemoveSource removes an event source from the manager
func (em *EventManager) RemoveSource(name string) error {
	em.sourcesMux.Lock()
	defer em.sourcesMux.Unlock()

	source, exists := em.sources[name]
	if !exists {
		return fmt.Errorf("source with name '%s' not found", name)
	}

	// Stop the source if it's running
	if err := source.Stop(); err != nil {
		return fmt.Errorf("failed to stop source '%s': %w", name, err)
	}

	delete(em.sources, name)
	return nil
}

// GetSource returns an event source by name
func (em *EventManager) GetSource(name string) (EventSource, bool) {
	em.sourcesMux.RLock()
	defer em.sourcesMux.RUnlock()

	source, exists := em.sources[name]
	return source, exists
}

// ListSources returns a list of all registered source names
func (em *EventManager) ListSources() []string {
	em.sourcesMux.RLock()
	defer em.sourcesMux.RUnlock()

	names := make([]string, 0, len(em.sources))
	for name := range em.sources {
		names = append(names, name)
	}
	return names
}

// Subscribe creates a subscription for events of a specific type
func (em *EventManager) Subscribe(eventType EventType, callback EventCallback) (string, error) {
	return em.SubscribeWithFilter(eventType, nil, callback)
}

// SubscribeWithFilter creates a subscription with an optional filter
func (em *EventManager) SubscribeWithFilter(eventType EventType, filter EventFilter, callback EventCallback) (string, error) {
	if callback == nil {
		return "", fmt.Errorf("callback cannot be nil")
	}

	subscriptionID := uuid.New().String()
	subscription := &Subscription{
		ID:       subscriptionID,
		Type:     eventType,
		Filter:   filter,
		Callback: callback,
	}

	em.subsMux.Lock()
	em.subscriptions[subscriptionID] = subscription
	em.subsMux.Unlock()

	// Subscribe to all sources for this event type
	em.sourcesMux.RLock()
	for _, source := range em.sources {
		// Create a wrapper callback that includes our filtering and routing logic
		sourceCallback := em.createSourceCallback(subscription)
		source.Subscribe(eventType, sourceCallback)
	}
	em.sourcesMux.RUnlock()

	return subscriptionID, nil
}

// Unsubscribe removes a subscription
func (em *EventManager) Unsubscribe(subscriptionID string) error {
	em.subsMux.Lock()
	_, exists := em.subscriptions[subscriptionID]
	if !exists {
		em.subsMux.Unlock()
		return fmt.Errorf("subscription with ID '%s' not found", subscriptionID)
	}
	delete(em.subscriptions, subscriptionID)
	em.subsMux.Unlock()

	// Unsubscribe from all sources
	em.sourcesMux.RLock()
	for _, source := range em.sources {
		source.Unsubscribe(subscriptionID)
	}
	em.sourcesMux.RUnlock()

	return nil
}

// Start starts all event sources
func (em *EventManager) Start(ctx context.Context) error {
	if em.started {
		return fmt.Errorf("event manager already started")
	}

	em.ctx, em.cancel = context.WithCancel(ctx)

	em.sourcesMux.RLock()
	defer em.sourcesMux.RUnlock()

	var errors []error
	for name, source := range em.sources {
		if err := source.Start(em.ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to start source '%s': %w", name, err))
		}
	}

	if len(errors) > 0 {
		// Stop any sources that were started successfully
		em.stopAllSources()
		return fmt.Errorf("failed to start some sources: %v", errors)
	}

	em.started = true
	return nil
}

// Stop stops all event sources
func (em *EventManager) Stop() error {
	if !em.started {
		return nil
	}

	if em.cancel != nil {
		em.cancel()
	}

	err := em.stopAllSources()
	em.started = false
	return err
}

// stopAllSources stops all registered sources
func (em *EventManager) stopAllSources() error {
	em.sourcesMux.RLock()
	defer em.sourcesMux.RUnlock()

	var lastErr error
	for name, source := range em.sources {
		if err := source.Stop(); err != nil {
			lastErr = fmt.Errorf("failed to stop source '%s': %w", name, err)
		}
	}

	return lastErr
}

// IsHealthy returns true if all sources are healthy
func (em *EventManager) IsHealthy() bool {
	em.sourcesMux.RLock()
	defer em.sourcesMux.RUnlock()

	for _, source := range em.sources {
		if !source.IsHealthy() {
			return false
		}
	}
	return true
}

// GetSourceHealth returns the health status of all sources
func (em *EventManager) GetSourceHealth() map[string]bool {
	em.sourcesMux.RLock()
	defer em.sourcesMux.RUnlock()

	health := make(map[string]bool)
	for name, source := range em.sources {
		health[name] = source.IsHealthy()
	}
	return health
}

// createSourceCallback creates a callback wrapper for a subscription
func (em *EventManager) createSourceCallback(subscription *Subscription) EventCallback {
	return func(event Event) error {
		// Apply filter if present
		if subscription.Filter != nil && !subscription.Filter(event) {
			return nil
		}

		// Call the subscription callback
		return subscription.Callback(event)
	}
}

// PublishEvent manually publishes an event (useful for testing or internal events)
func (em *EventManager) PublishEvent(event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	em.subsMux.RLock()
	defer em.subsMux.RUnlock()

	var errors []error
	for _, subscription := range em.subscriptions {
		if subscription.Type == event.Type {
			// Apply filter if present
			if subscription.Filter != nil && !subscription.Filter(event) {
				continue
			}

			if err := subscription.Callback(event); err != nil {
				errors = append(errors, fmt.Errorf("subscription '%s': %w", subscription.ID, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to deliver event to some subscribers: %v", errors)
	}

	return nil
}
