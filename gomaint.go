// Package gomaint provides a maintenance library for Go services
// that can gracefully handle maintenance mode.
package gomaint

import (
	"context"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/config"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
	"github.com/abhishekvarshney/gomaint/pkg/manager"
)

// Manager is the main maintenance manager
type Manager = manager.Manager

// Handler interface for maintenance handlers
type Handler = handlers.Handler

// Config holds the configuration for the maintenance manager
type Config = config.Config

// NewManager creates a new maintenance manager with the given configuration
func NewManager(cfg *Config) *Manager {
	return manager.NewManager(cfg)
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return config.DefaultConfig()
}

// NewConfig creates a new configuration with the specified parameters
func NewConfig(etcdEndpoints []string, keyPath string, drainTimeout time.Duration) *Config {
	cfg := DefaultConfig()
	cfg.EtcdEndpoints = etcdEndpoints
	cfg.KeyPath = keyPath
	cfg.DrainTimeout = drainTimeout
	return cfg
}

// Start is a convenience function to create and start a maintenance manager
func Start(ctx context.Context, cfg *Config, handlers ...Handler) (*Manager, error) {
	mgr := NewManager(cfg)

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
