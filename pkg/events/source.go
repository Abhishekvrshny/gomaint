package events

import (
	"context"
)

// EventSource represents a source of state change events
type EventSource interface {
	// Start begins monitoring for events
	Start(ctx context.Context) error

	// Stop stops monitoring and cleans up resources
	Stop() error

	// Subscribe adds a subscription for specific event types
	Subscribe(eventType EventType, callback EventCallback) error

	// Unsubscribe removes a subscription
	Unsubscribe(subscriptionID string) error

	// Name returns the name of the event source
	Name() string

	// IsHealthy returns true if the source is healthy and operational
	IsHealthy() bool

	// GetLastEvent returns the last event received (if any)
	GetLastEvent() *Event
}

// EventSourceConfig represents configuration for an event source
type EventSourceConfig struct {
	// Name is the unique name for this source
	Name string `json:"name" yaml:"name"`

	// Type is the type of source (e.g., "etcd", "file", "webhook")
	Type string `json:"type" yaml:"type"`

	// Enabled indicates if this source should be active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Config contains source-specific configuration
	Config map[string]interface{} `json:"config" yaml:"config"`
}
