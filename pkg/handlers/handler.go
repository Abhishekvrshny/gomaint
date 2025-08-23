package handlers

import (
	"context"
)

// Handler defines the interface that all maintenance handlers must implement
type Handler interface {
	// OnMaintenanceStart is called when the service enters maintenance mode
	OnMaintenanceStart(ctx context.Context) error

	// OnMaintenanceEnd is called when the service exits maintenance mode
	OnMaintenanceEnd(ctx context.Context) error

	// Name returns the name of the handler for identification
	Name() string

	// IsHealthy returns true if the handler is in a healthy state
	IsHealthy() bool
}

// HandlerState represents the current state of a handler
type HandlerState int

const (
	StateNormal HandlerState = iota
	StateMaintenance
	StateError
)

func (s HandlerState) String() string {
	switch s {
	case StateNormal:
		return "normal"
	case StateMaintenance:
		return "maintenance"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// BaseHandler provides common functionality for handlers
type BaseHandler struct {
	name  string
	state HandlerState
}

// NewBaseHandler creates a new base handler
func NewBaseHandler(name string) *BaseHandler {
	return &BaseHandler{
		name:  name,
		state: StateNormal,
	}
}

// Name returns the handler name
func (h *BaseHandler) Name() string {
	return h.name
}

// State returns the current handler state
func (h *BaseHandler) State() HandlerState {
	return h.state
}

// SetState sets the handler state
func (h *BaseHandler) SetState(state HandlerState) {
	h.state = state
}

// IsHealthy returns true if the handler is not in error state
func (h *BaseHandler) IsHealthy() bool {
	return h.state != StateError
}
