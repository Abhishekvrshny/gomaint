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

// Handler interface for maintenance handlers
type Handler = handlers.Handler

// StartWithEtcd creates and starts a maintenance manager with etcd event source
// This is the recommended and primary way to create a maintenance manager.
func StartWithEtcd(ctx context.Context, endpoints []string, keyPath string, drainTimeout time.Duration, handlers ...Handler) (*Manager, error) {
	// Create etcd configuration
	etcdConfig := eventsource.NewEtcdConfig("etcd", endpoints, keyPath)
	
	// Create etcd event source
	eventSource, err := eventsource.NewEtcdEventSource(etcdConfig)
	if err != nil {
		return nil, err
	}
	
	// Create maintenance manager
	mgr := maintenance.NewManager(eventSource, drainTimeout)
	
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