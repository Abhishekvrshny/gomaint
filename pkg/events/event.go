package events

import (
	"time"
)

// EventType represents the type of event
type EventType string

const (
	// EventTypeMaintenanceChange represents a maintenance mode state change
	EventTypeMaintenanceChange EventType = "maintenance_change"
	// EventTypeConfigChange represents a configuration change
	EventTypeConfigChange EventType = "config_change"
	// EventTypeHealthChange represents a health status change
	EventTypeHealthChange EventType = "health_change"
)

// Event represents a state change event from any source
type Event struct {
	// Source is the name of the event source (e.g., "etcd", "file", "webhook")
	Source string `json:"source"`

	// Type is the type of event
	Type EventType `json:"type"`

	// Key is the key/path that changed
	Key string `json:"key"`

	// Value is the new value (can be any type)
	Value interface{} `json:"value"`

	// PreviousValue is the previous value (can be nil for new keys)
	PreviousValue interface{} `json:"previous_value,omitempty"`

	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Metadata contains source-specific metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// EventCallback is the function signature for event callbacks
type EventCallback func(event Event) error

// EventFilter is a function that determines if an event should be processed
type EventFilter func(event Event) bool

// Subscription represents an event subscription
type Subscription struct {
	ID       string
	Type     EventType
	Filter   EventFilter
	Callback EventCallback
}
