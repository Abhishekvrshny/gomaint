package kafka

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// Config holds the configuration for the Kafka handler
type Config struct {
	DrainTimeout time.Duration
}

// Handler implements the Handler interface for Kafka message processing
type Handler struct {
	*handlers.BaseHandler
	consumer Consumer
	config   *Config
	logger   *log.Logger

	// State management
	inMaintenance int32 // atomic boolean
	stopChan      chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
	isRunning     bool
	ctx           context.Context
	cancel        context.CancelFunc

	// Statistics
	stats Stats
}

// Stats holds statistics for the Kafka handler
type Stats struct {
	MessagesReceived  int64
	MessagesProcessed int64
	MessagesFailed    int64
	MessagesInFlight  int64
	LastMessageTime   time.Time
	ProcessingErrors  []string
	mu                sync.RWMutex
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		DrainTimeout: 30 * time.Second,
	}
}

// NewKafkaHandler creates a new Kafka handler
func NewKafkaHandler(consumer Consumer, config *Config, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.Default()
	}

	if config == nil {
		config = DefaultConfig()
	}

	return &Handler{
		BaseHandler: handlers.NewBaseHandler("kafka"),
		consumer:    consumer,
		config:      config,
		logger:      logger,
		stopChan:    make(chan struct{}),
	}
}

// Start begins message processing (non-blocking)
func (h *Handler) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isRunning {
		h.logger.Println("Kafka Handler: Already running, skipping start")
		return nil
	}

	h.logger.Println("Kafka Handler: Starting message processing")

	// Create a new context for the handler that's independent of the passed context
	h.ctx, h.cancel = context.WithCancel(context.Background())

	// Create a new stop channel if needed
	if h.stopChan == nil {
		h.stopChan = make(chan struct{})
	}

	// Start the consumer
	if err := h.consumer.Start(h.ctx); err != nil {
		return fmt.Errorf("failed to start consumer: %w", err)
	}

	h.isRunning = true

	return nil
}

// Stop stops message processing
func (h *Handler) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.isRunning {
		h.logger.Println("Kafka Handler: Not running, skipping stop")
		return nil
	}

	h.logger.Println("Kafka Handler: Stopping message processing")

	// Stop the consumer
	if err := h.consumer.Stop(); err != nil {
		h.logger.Printf("Kafka Handler: Error stopping consumer: %v", err)
	}

	// Cancel the handler context
	if h.cancel != nil {
		h.cancel()
	}

	close(h.stopChan)
	h.isRunning = false

	// Reset stop channel for potential restart
	h.stopChan = nil
	h.ctx = nil
	h.cancel = nil

	h.logger.Println("Kafka Handler: Message processing stopped")
	return nil
}

// OnMaintenanceStart handles maintenance mode activation
func (h *Handler) OnMaintenanceStart(ctx context.Context) error {
	h.logger.Println("Kafka Handler: Entering maintenance mode - draining messages")
	atomic.StoreInt32(&h.inMaintenance, 1)
	h.SetState(handlers.StateMaintenance)

	// Stop the handler to prevent new messages from being received
	if err := h.Stop(); err != nil {
		h.logger.Printf("Kafka Handler: Error stopping during maintenance: %v", err)
	}

	// Wait for active messages to complete or timeout
	done := make(chan struct{})
	go func() {
		for {
			if h.consumer.ActiveMessages() == 0 {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-done:
		h.logger.Println("Kafka Handler: All messages drained successfully")
		return nil
	case <-time.After(h.config.DrainTimeout):
		activeCount := h.consumer.ActiveMessages()
		h.logger.Printf("Kafka Handler: Drain timeout reached with %d messages still active", activeCount)
		return fmt.Errorf("timeout waiting for %d messages to drain after %v", activeCount, h.config.DrainTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OnMaintenanceEnd handles maintenance mode deactivation
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	h.logger.Println("Kafka Handler: Exiting maintenance mode - resuming message processing")
	atomic.StoreInt32(&h.inMaintenance, 0)
	h.SetState(handlers.StateNormal)

	// Start the handler with a fresh context
	if err := h.Start(ctx); err != nil {
		h.logger.Printf("Kafka Handler: Failed to restart after maintenance: %v", err)
		return err
	}
	h.logger.Println("Kafka Handler: Message processing resumed after maintenance")

	return nil
}

// IsHealthy performs Kafka health check
func (h *Handler) IsHealthy() bool {
	// First check the base handler state
	if !h.BaseHandler.IsHealthy() {
		return false
	}

	// Check consumer health
	if h.consumer == nil || !h.consumer.IsHealthy() {
		h.logger.Println("Kafka Handler: Health check failed - consumer is not healthy")
		return false
	}

	return true
}

// GetStats returns handler statistics
func (h *Handler) GetStats() map[string]interface{} {
	h.stats.mu.RLock()
	defer h.stats.mu.RUnlock()

	stats := map[string]interface{}{
		"handler_name":       h.Name(),
		"handler_state":      h.State().String(),
		"in_maintenance":     atomic.LoadInt32(&h.inMaintenance) == 1,
		"messages_received":  h.stats.MessagesReceived,
		"messages_processed": h.stats.MessagesProcessed,
		"messages_failed":    h.stats.MessagesFailed,
		"messages_in_flight": h.stats.MessagesInFlight,
		"active_messages":    h.consumer.ActiveMessages(),
		"last_message_time":  h.stats.LastMessageTime,
		"drain_timeout":      h.config.DrainTimeout.String(),
	}

	// Include recent errors if any
	if len(h.stats.ProcessingErrors) > 0 {
		// Show last 5 errors
		errorCount := len(h.stats.ProcessingErrors)
		startIdx := 0
		if errorCount > 5 {
			startIdx = errorCount - 5
		}
		stats["recent_errors"] = h.stats.ProcessingErrors[startIdx:]
	}

	return stats
}
