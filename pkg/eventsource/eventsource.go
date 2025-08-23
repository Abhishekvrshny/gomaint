package eventsource

import (
	"context"
	"time"
)

// EventType represents the type of maintenance event
type EventType string

const (
	// MaintenanceEnabled indicates maintenance mode should be enabled
	MaintenanceEnabled EventType = "maintenance_enabled"
	// MaintenanceDisabled indicates maintenance mode should be disabled
	MaintenanceDisabled EventType = "maintenance_disabled"
)

// MaintenanceEvent represents a maintenance state change event
type MaintenanceEvent struct {
	// Type indicates whether maintenance is being enabled or disabled
	Type EventType `json:"type"`
	
	// Key is the source key that triggered this event
	Key string `json:"key"`
	
	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`
	
	// Source identifier for the event source
	Source string `json:"source"`
	
	// Metadata contains additional event information
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// EventHandler is called when a maintenance event occurs
type EventHandler func(event MaintenanceEvent) error

// EventSource defines the interface for maintenance event sources
// An EventSource can only be of one specific type (e.g., etcd, file, etc.)
type EventSource interface {
	// Start begins monitoring for maintenance events
	Start(ctx context.Context, handler EventHandler) error
	
	// Stop stops monitoring and cleans up resources
	Stop() error
	
	// Name returns the name/identifier of this event source
	Name() string
	
	// Type returns the type of event source (e.g., "etcd")
	Type() string
	
	// IsHealthy returns true if the event source is operational
	IsHealthy() bool
	
	// SetMaintenance manually sets the maintenance state (if supported)
	SetMaintenance(ctx context.Context, enabled bool) error
	
	// GetMaintenance gets the current maintenance state
	GetMaintenance(ctx context.Context) (bool, error)
}

// Config represents basic configuration for event sources
type Config struct {
	// Name is the identifier for this event source instance
	Name string
	
	// Type is the type of event source ("etcd", "file", etc.)
	Type string
}