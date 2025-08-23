// Package gomaint provides a maintenance library for Go services
// that can gracefully handle maintenance mode.
package gomaint

import (
	"context"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/eventsource"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
	"github.com/abhishekvarshney/gomaint/pkg/maintenance"
)

// Manager is the maintenance manager
type Manager = maintenance.Manager

// EventSource interface for maintenance event sources  
type EventSource = eventsource.EventSource

// Handler interface for maintenance handlers
type Handler = handlers.Handler

// EtcdConfig holds etcd-specific configuration
type EtcdConfig = eventsource.EtcdConfig

// NewManager creates a new maintenance manager with an event source
func NewManager(eventSource EventSource, drainTimeout time.Duration) *Manager {
	return maintenance.NewManager(eventSource, drainTimeout)
}

// NewEtcdEventSource creates a new etcd event source
func NewEtcdEventSource(config *EtcdConfig) (EventSource, error) {
	return eventsource.NewEtcdEventSource(config)
}

// NewEtcdConfig creates a new etcd configuration
func NewEtcdConfig(name string, endpoints []string, keyPath string) *EtcdConfig {
	return eventsource.NewEtcdConfig(name, endpoints, keyPath)
}

// StartWithEtcd is a convenience function to create and start a maintenance manager with etcd
func StartWithEtcd(ctx context.Context, endpoints []string, keyPath string, drainTimeout time.Duration, handlers ...Handler) (*Manager, error) {
	// Create etcd configuration
	etcdConfig := NewEtcdConfig("etcd", endpoints, keyPath)
	
	// Create etcd event source
	eventSource, err := NewEtcdEventSource(etcdConfig)
	if err != nil {
		return nil, err
	}
	
	// Create maintenance manager
	mgr := NewManager(eventSource, drainTimeout)
	
	// Register all handlers
	for _, handler := range handlers {
		if err := mgr.RegisterHandler(handler); err != nil {
			return nil, err
		}
	}
	
	// Start the manager
	if err := mgr.Start(ctx); err != nil {
		return nil, err
	}
	
	return mgr, nil
}

// HandlerState represents the current state of a handler
type HandlerState = handlers.HandlerState

// Handler states
const (
	StateNormal      = handlers.StateNormal
	StateMaintenance = handlers.StateMaintenance
	StateError       = handlers.StateError
)
