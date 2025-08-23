package sources

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/config"
	"github.com/abhishekvarshney/gomaint/pkg/events"
	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdEventSource implements EventSource for ETCD
type EtcdEventSource struct {
	name          string
	client        *clientv3.Client
	config        *config.Config
	subscriptions map[string]*etcdSubscription
	subsMux       sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	lastEvent     *events.Event
	lastEventMux  sync.RWMutex
	healthy       bool
	healthMux     sync.RWMutex
}

type etcdSubscription struct {
	id        string
	eventType events.EventType
	callback  events.EventCallback
}

// NewEtcdEventSource creates a new ETCD event source
func NewEtcdEventSource(name string, cfg *config.Config) (*EtcdEventSource, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	clientConfig := clientv3.Config{
		Endpoints:   cfg.EtcdEndpoints,
		DialTimeout: cfg.EtcdTimeout,
		Username:    cfg.EtcdUsername,
		Password:    cfg.EtcdPassword,
	}

	// Configure TLS if enabled
	if cfg.EtcdTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
		}

		if cfg.EtcdCertFile != "" && cfg.EtcdKeyFile != "" {
			cert, err := tls.LoadX509KeyPair(cfg.EtcdCertFile, cfg.EtcdKeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificates: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		clientConfig.TLS = tlsConfig
	}

	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return &EtcdEventSource{
		name:          name,
		client:        client,
		config:        cfg,
		subscriptions: make(map[string]*etcdSubscription),
		healthy:       true,
	}, nil
}

// Start begins monitoring for events
func (e *EtcdEventSource) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)

	// Get initial value and notify subscribers
	if err := e.checkInitialState(); err != nil {
		e.setHealthy(false)
		return fmt.Errorf("failed to check initial state: %w", err)
	}

	// Start watching for changes
	go e.watchKey()

	e.setHealthy(true)
	return nil
}

// Stop stops monitoring and cleans up resources
func (e *EtcdEventSource) Stop() error {
	if e.cancel != nil {
		e.cancel()
	}

	if e.client != nil {
		return e.client.Close()
	}

	e.setHealthy(false)
	return nil
}

// Subscribe adds a subscription for specific event types
func (e *EtcdEventSource) Subscribe(eventType events.EventType, callback events.EventCallback) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}

	subscriptionID := uuid.New().String()
	subscription := &etcdSubscription{
		id:        subscriptionID,
		eventType: eventType,
		callback:  callback,
	}

	e.subsMux.Lock()
	e.subscriptions[subscriptionID] = subscription
	e.subsMux.Unlock()

	return nil
}

// Unsubscribe removes a subscription
func (e *EtcdEventSource) Unsubscribe(subscriptionID string) error {
	e.subsMux.Lock()
	defer e.subsMux.Unlock()

	if _, exists := e.subscriptions[subscriptionID]; !exists {
		return fmt.Errorf("subscription with ID '%s' not found", subscriptionID)
	}

	delete(e.subscriptions, subscriptionID)
	return nil
}

// Name returns the name of the event source
func (e *EtcdEventSource) Name() string {
	return e.name
}

// IsHealthy returns true if the source is healthy and operational
func (e *EtcdEventSource) IsHealthy() bool {
	e.healthMux.RLock()
	defer e.healthMux.RUnlock()
	return e.healthy
}

// GetLastEvent returns the last event received (if any)
func (e *EtcdEventSource) GetLastEvent() *events.Event {
	e.lastEventMux.RLock()
	defer e.lastEventMux.RUnlock()
	return e.lastEvent
}

// checkInitialState gets the current value of the key and notifies subscribers
func (e *EtcdEventSource) checkInitialState() error {
	ctx, cancel := context.WithTimeout(e.ctx, e.config.EtcdTimeout)
	defer cancel()

	resp, err := e.client.Get(ctx, e.config.KeyPath)
	if err != nil {
		return fmt.Errorf("failed to get initial key value: %w", err)
	}

	// If key doesn't exist, assume normal mode (not in maintenance)
	var value interface{} = false
	var previousValue interface{}
	if len(resp.Kvs) > 0 {
		value = e.parseMaintenanceValue(string(resp.Kvs[0].Value))
	}

	event := events.Event{
		Source:        e.name,
		Type:          events.EventTypeMaintenanceChange,
		Key:           e.config.KeyPath,
		Value:         value,
		PreviousValue: previousValue,
		Timestamp:     time.Now(),
		Metadata: map[string]interface{}{
			"initial": true,
		},
	}

	e.setLastEvent(&event)
	return e.notifySubscribers(event)
}

// watchKey watches for changes to the etcd key
func (e *EtcdEventSource) watchKey() {
	watchChan := e.client.Watch(e.ctx, e.config.KeyPath)

	for {
		select {
		case <-e.ctx.Done():
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				e.setHealthy(false)
				// Log error and continue watching
				// In a real implementation, you might want to implement exponential backoff
				time.Sleep(time.Second)
				continue
			}

			e.setHealthy(true)

			for _, etcdEvent := range watchResp.Events {
				var value interface{}
				var eventType string

				switch etcdEvent.Type {
				case clientv3.EventTypePut:
					value = e.parseMaintenanceValue(string(etcdEvent.Kv.Value))
					eventType = "put"
				case clientv3.EventTypeDelete:
					value = false // Key deleted means not in maintenance
					eventType = "delete"
				}

				event := events.Event{
					Source:    e.name,
					Type:      events.EventTypeMaintenanceChange,
					Key:       e.config.KeyPath,
					Value:     value,
					Timestamp: time.Now(),
					Metadata: map[string]interface{}{
						"etcd_event_type": eventType,
						"etcd_revision":   etcdEvent.Kv.ModRevision,
					},
				}

				e.setLastEvent(&event)
				if err := e.notifySubscribers(event); err != nil {
					// Log error but continue watching
					// In a real implementation, you might want to implement retry logic
					continue
				}
			}
		}
	}
}

// parseMaintenanceValue parses the etcd value to determine maintenance state
func (e *EtcdEventSource) parseMaintenanceValue(value string) bool {
	// Simple parsing: "true", "1", "on", "enabled" means maintenance mode
	// Everything else (including empty) means normal mode
	switch value {
	case "true", "1", "on", "enabled", "maintenance":
		return true
	default:
		return false
	}
}

// notifySubscribers notifies all subscribers of an event
func (e *EtcdEventSource) notifySubscribers(event events.Event) error {
	e.subsMux.RLock()
	defer e.subsMux.RUnlock()

	var errors []error
	for _, subscription := range e.subscriptions {
		if subscription.eventType == event.Type {
			if err := subscription.callback(event); err != nil {
				errors = append(errors, fmt.Errorf("subscription '%s': %w", subscription.id, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to notify some subscribers: %v", errors)
	}

	return nil
}

// setLastEvent sets the last event (thread-safe)
func (e *EtcdEventSource) setLastEvent(event *events.Event) {
	e.lastEventMux.Lock()
	defer e.lastEventMux.Unlock()
	e.lastEvent = event
}

// setHealthy sets the health status (thread-safe)
func (e *EtcdEventSource) setHealthy(healthy bool) {
	e.healthMux.Lock()
	defer e.healthMux.Unlock()
	e.healthy = healthy
}

// SetMaintenanceMode sets the maintenance mode in etcd
func (e *EtcdEventSource) SetMaintenanceMode(ctx context.Context, inMaintenance bool) error {
	ctx, cancel := context.WithTimeout(ctx, e.config.EtcdTimeout)
	defer cancel()

	var value string
	if inMaintenance {
		value = "true"
	} else {
		value = "false"
	}

	_, err := e.client.Put(ctx, e.config.KeyPath, value)
	if err != nil {
		return fmt.Errorf("failed to set maintenance mode: %w", err)
	}

	return nil
}

// GetMaintenanceMode gets the current maintenance mode from etcd
func (e *EtcdEventSource) GetMaintenanceMode(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, e.config.EtcdTimeout)
	defer cancel()

	resp, err := e.client.Get(ctx, e.config.KeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to get maintenance mode: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return false, nil // Key doesn't exist, not in maintenance
	}

	return e.parseMaintenanceValue(string(resp.Kvs[0].Value)), nil
}
