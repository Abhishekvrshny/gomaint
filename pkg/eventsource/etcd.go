package eventsource

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdConfig holds etcd-specific configuration
type EtcdConfig struct {
	Config

	// Endpoints is the list of etcd endpoints
	Endpoints []string `json:"endpoints"`

	// KeyPath is the etcd key to watch for maintenance state
	KeyPath string `json:"key_path"`

	// Timeout for etcd operations
	Timeout time.Duration `json:"timeout"`

	// Authentication
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// TLS Configuration
	TLS      bool   `json:"tls,omitempty"`
	CertFile string `json:"cert_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
	CAFile   string `json:"ca_file,omitempty"`
}

// NewEtcdConfig creates a new etcd configuration
func NewEtcdConfig(name string, endpoints []string, keyPath string) *EtcdConfig {
	return &EtcdConfig{
		Config: Config{
			Name: name,
			Type: "etcd",
		},
		Endpoints: endpoints,
		KeyPath:   keyPath,
		Timeout:   5 * time.Second,
	}
}

// EtcdEventSource implements EventSource for etcd
type EtcdEventSource struct {
	config  *EtcdConfig
	client  *clientv3.Client
	handler EventHandler
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mutex   sync.RWMutex
	logger  logger.Logger
}

// NewEtcdEventSource creates a new etcd event source
func NewEtcdEventSource(config *EtcdConfig) (*EtcdEventSource, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if len(config.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one etcd endpoint must be specified")
	}

	if config.KeyPath == "" {
		return nil, fmt.Errorf("key path must be specified")
	}

	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}

	return &EtcdEventSource{
		config:  config,
		healthy: false,
		logger:  logger.NewDefaultLogger(),
	}, nil
}

// Start begins monitoring for maintenance events
func (e *EtcdEventSource) Start(ctx context.Context, handler EventHandler) error {
	if handler == nil {
		return fmt.Errorf("event handler cannot be nil")
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.client != nil {
		return fmt.Errorf("event source already started")
	}

	// Create etcd client
	clientConfig := clientv3.Config{
		Endpoints:   e.config.Endpoints,
		DialTimeout: e.config.Timeout,
		Username:    e.config.Username,
		Password:    e.config.Password,
	}

	// Configure TLS if enabled
	if e.config.TLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
		}

		if e.config.CertFile != "" && e.config.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(e.config.CertFile, e.config.KeyFile)
			if err != nil {
				return fmt.Errorf("failed to load client certificates: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		clientConfig.TLS = tlsConfig
	}

	client, err := clientv3.New(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}

	e.client = client
	e.handler = handler
	e.ctx, e.cancel = context.WithCancel(ctx)

	e.logger.Infof("etcd event source connected to %v", e.config.Endpoints)

	// Check initial state
	if err := e.checkInitialState(); err != nil {
		e.cleanup()
		return fmt.Errorf("failed to check initial state: %w", err)
	}

	// Start watching for changes
	go e.watchForChanges()

	e.healthy = true
	e.logger.Infof("etcd event source started, watching key: %s", e.config.KeyPath)
	return nil
}

// Stop stops monitoring and cleans up resources
func (e *EtcdEventSource) Stop() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.cleanup()
	return nil
}

// cleanup performs internal cleanup (must be called with mutex held)
func (e *EtcdEventSource) cleanup() {
	e.healthy = false

	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}

	if e.client != nil {
		e.client.Close()
		e.client = nil
	}

	e.handler = nil
}

// Name returns the name of this event source
func (e *EtcdEventSource) Name() string {
	return e.config.Name
}

// Type returns the type of event source
func (e *EtcdEventSource) Type() string {
	return e.config.Type
}

// IsHealthy returns true if the event source is operational
func (e *EtcdEventSource) IsHealthy() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.healthy
}

// SetLogger sets a custom logger for the event source
func (e *EtcdEventSource) SetLogger(l logger.Logger) {
	if l != nil {
		e.logger = l
	}
}

// SetMaintenance sets the maintenance state in etcd
func (e *EtcdEventSource) SetMaintenance(ctx context.Context, enabled bool) error {
	e.mutex.RLock()
	client := e.client
	e.mutex.RUnlock()

	if client == nil {
		return fmt.Errorf("event source not started")
	}

	ctx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	value := "false"
	if enabled {
		value = "true"
	}

	_, err := client.Put(ctx, e.config.KeyPath, value)
	if err != nil {
		return fmt.Errorf("failed to set maintenance state: %w", err)
	}

	return nil
}

// GetMaintenance gets the current maintenance state from etcd
func (e *EtcdEventSource) GetMaintenance(ctx context.Context) (bool, error) {
	e.mutex.RLock()
	client := e.client
	e.mutex.RUnlock()

	if client == nil {
		return false, fmt.Errorf("event source not started")
	}

	ctx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	resp, err := client.Get(ctx, e.config.KeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to get maintenance state: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return false, nil // Key doesn't exist, not in maintenance
	}

	return e.parseMaintenanceValue(string(resp.Kvs[0].Value)), nil
}

// checkInitialState checks the initial maintenance state
func (e *EtcdEventSource) checkInitialState() error {
	ctx, cancel := context.WithTimeout(e.ctx, e.config.Timeout)
	defer cancel()

	resp, err := e.client.Get(ctx, e.config.KeyPath)
	if err != nil {
		return fmt.Errorf("failed to get initial key value: %w", err)
	}

	// Determine initial state
	enabled := false
	if len(resp.Kvs) > 0 {
		enabled = e.parseMaintenanceValue(string(resp.Kvs[0].Value))
	}

	// Create and send initial event
	eventType := MaintenanceDisabled
	if enabled {
		eventType = MaintenanceEnabled
	}

	event := MaintenanceEvent{
		Type:      eventType,
		Key:       e.config.KeyPath,
		Timestamp: time.Now(),
		Source:    e.config.Name,
		Metadata: map[string]interface{}{
			"initial": true,
		},
	}

	return e.handler(event)
}

// watchForChanges watches the etcd key for changes
func (e *EtcdEventSource) watchForChanges() {
	watchChan := e.client.Watch(e.ctx, e.config.KeyPath)

	for {
		select {
		case <-e.ctx.Done():
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				// Mark as unhealthy but continue trying
				e.mutex.Lock()
				e.healthy = false
				e.mutex.Unlock()

				e.logger.Errorf("etcd watch error: %v, retrying in 1s", watchResp.Err())
				// Simple backoff - in production might want exponential backoff
				time.Sleep(time.Second)
				continue
			}

			e.mutex.Lock()
			e.healthy = true
			e.mutex.Unlock()

			for _, etcdEvent := range watchResp.Events {
				if err := e.processEtcdEvent(etcdEvent); err != nil {
					e.logger.Errorf("failed to process etcd event: %v", err)
					continue
				}
			}
		}
	}
}

// processEtcdEvent processes a single etcd event
func (e *EtcdEventSource) processEtcdEvent(etcdEvent *clientv3.Event) error {
	var eventType EventType
	var enabled bool

	switch etcdEvent.Type {
	case clientv3.EventTypePut:
		enabled = e.parseMaintenanceValue(string(etcdEvent.Kv.Value))
	case clientv3.EventTypeDelete:
		enabled = false // Key deleted means not in maintenance
	default:
		return nil // Unknown event type, ignore
	}

	if enabled {
		eventType = MaintenanceEnabled
	} else {
		eventType = MaintenanceDisabled
	}

	event := MaintenanceEvent{
		Type:      eventType,
		Key:       e.config.KeyPath,
		Timestamp: time.Now(),
		Source:    e.config.Name,
		Metadata: map[string]interface{}{
			"etcd_event_type": etcdEvent.Type.String(),
			"etcd_revision":   etcdEvent.Kv.ModRevision,
		},
	}

	return e.handler(event)
}

// parseMaintenanceValue parses the etcd value to determine maintenance state
func (e *EtcdEventSource) parseMaintenanceValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "true", "1", "yes", "on", "enabled", "maintenance":
		return true
	default:
		return false
	}
}
