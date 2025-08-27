package sqs

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/handlers"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// SQSClient interface allows for easier testing and mocking
type SQSClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

// MessageProcessor defines the interface for processing SQS messages
type MessageProcessor interface {
	ProcessMessage(ctx context.Context, message types.Message) error
}

// Config holds the configuration for the SQS handler
type Config struct {
	QueueURL                 string
	MaxNumberOfMessages      int32
	WaitTimeSeconds          int32
	VisibilityTimeoutSeconds int32
	DrainTimeout             time.Duration
	PollInterval             time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig(queueURL string) *Config {
	return &Config{
		QueueURL:                 queueURL,
		MaxNumberOfMessages:      10,
		WaitTimeSeconds:          20,
		VisibilityTimeoutSeconds: 30,
		DrainTimeout:             30 * time.Second,
		PollInterval:             1 * time.Second,
	}
}

// Handler implements the Handler interface for SQS message processing
type Handler struct {
	*handlers.BaseHandler
	client    SQSClient
	config    *Config
	processor MessageProcessor
	logger    *log.Logger

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

// Stats holds statistics for the SQS handler
type Stats struct {
	MessagesReceived  int64
	MessagesProcessed int64
	MessagesFailed    int64
	MessagesInFlight  int64
	LastMessageTime   time.Time
	ProcessingErrors  []string
	mu                sync.RWMutex
}

// NewSQSHandler creates a new SQS handler
func NewSQSHandler(client SQSClient, config *Config, processor MessageProcessor, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.Default()
	}

	return &Handler{
		BaseHandler: handlers.NewBaseHandler("sqs"),
		client:      client,
		config:      config,
		processor:   processor,
		logger:      logger,
		stopChan:    make(chan struct{}),
	}
}

// Start begins message processing (non-blocking)
func (h *Handler) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isRunning {
		h.logger.Println("SQS Handler: Already running, skipping start")
		return nil
	}

	h.logger.Printf("SQS Handler: Starting message processing for queue: %s", h.config.QueueURL)

	// Create a new context for the handler that's independent of the passed context
	h.ctx, h.cancel = context.WithCancel(context.Background())

	// Create a new stop channel if needed
	if h.stopChan == nil {
		h.stopChan = make(chan struct{})
	}

	// Start the message processing goroutine
	h.wg.Add(1)
	h.isRunning = true
	go h.messageLoop(h.ctx)

	return nil
}

// Stop stops message processing
func (h *Handler) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.isRunning {
		h.logger.Println("SQS Handler: Not running, skipping stop")
		return nil
	}

	h.logger.Println("SQS Handler: Stopping message processing")

	// Cancel the handler context
	if h.cancel != nil {
		h.cancel()
	}

	close(h.stopChan)
	h.isRunning = false

	// Wait for message loop to finish (unlock mutex first)
	h.mu.Unlock()
	h.wg.Wait()
	h.mu.Lock()

	// Reset stop channel for potential restart
	h.stopChan = nil
	h.ctx = nil
	h.cancel = nil

	h.logger.Println("SQS Handler: Message processing stopped")
	return nil
}

// OnMaintenanceStart handles maintenance mode activation
func (h *Handler) OnMaintenanceStart(ctx context.Context) error {
	h.logger.Println("SQS Handler: Entering maintenance mode - draining messages")
	atomic.StoreInt32(&h.inMaintenance, 1)
	h.SetState(handlers.StateMaintenance)

	// Stop the handler to prevent new messages from being received
	if err := h.Stop(); err != nil {
		h.logger.Printf("SQS Handler: Error stopping during maintenance: %v", err)
	}

	// Wait for in-flight messages to complete or timeout
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		h.logger.Println("SQS Handler: All messages drained successfully")
		return nil
	case <-time.After(h.config.DrainTimeout):
		inFlight := atomic.LoadInt64(&h.stats.MessagesInFlight)
		h.logger.Printf("SQS Handler: Drain timeout reached with %d messages still in flight", inFlight)
		return fmt.Errorf("timeout waiting for %d messages to drain after %v", inFlight, h.config.DrainTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OnMaintenanceEnd handles maintenance mode deactivation
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	h.logger.Println("SQS Handler: Exiting maintenance mode - resuming message processing")
	atomic.StoreInt32(&h.inMaintenance, 0)
	h.SetState(handlers.StateNormal)

	// Restart the handler if it was running
	h.mu.Lock()
	wasRunning := h.isRunning
	h.mu.Unlock()

	if !wasRunning {
		// Start the handler with a fresh context
		if err := h.Start(ctx); err != nil {
			h.logger.Printf("SQS Handler: Failed to restart after maintenance: %v", err)
			return err
		}
		h.logger.Println("SQS Handler: Message processing resumed after maintenance")
	} else {
		h.logger.Println("SQS Handler: Message processing will continue automatically")
	}

	return nil
}

// IsHealthy performs SQS health check
func (h *Handler) IsHealthy() bool {
	// First check the base handler state
	if !h.BaseHandler.IsHealthy() {
		return false
	}

	// Check SQS connectivity by getting queue attributes
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(h.config.QueueURL),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
		},
	})

	if err != nil {
		h.logger.Printf("SQS Handler: Health check failed: %v", err)
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
		"queue_url":          h.config.QueueURL,
		"in_maintenance":     atomic.LoadInt32(&h.inMaintenance) == 1,
		"messages_received":  h.stats.MessagesReceived,
		"messages_processed": h.stats.MessagesProcessed,
		"messages_failed":    h.stats.MessagesFailed,
		"messages_in_flight": h.stats.MessagesInFlight,
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

// messageLoop is the main message processing loop
func (h *Handler) messageLoop(ctx context.Context) {
	defer h.wg.Done()
	defer func() {
		h.mu.Lock()
		h.isRunning = false
		h.mu.Unlock()
	}()

	ticker := time.NewTicker(h.config.PollInterval)
	defer ticker.Stop()

	h.logger.Println("SQS Handler: Message loop started")

	for {
		select {
		case <-ctx.Done():
			h.logger.Println("SQS Handler: Context cancelled, stopping message loop")
			return
		case <-h.stopChan:
			h.logger.Println("SQS Handler: Stop signal received, stopping message loop")
			return
		case <-ticker.C:
			// Skip receiving new messages if in maintenance mode
			if atomic.LoadInt32(&h.inMaintenance) == 1 {
				// Don't log every poll interval to avoid spam
				continue
			}

			// Receive messages from SQS
			if err := h.receiveAndProcessMessages(ctx); err != nil {
				h.logger.Printf("SQS Handler: Error receiving messages: %v", err)
				h.addProcessingError(err.Error())
			}
		}
	}
}

// receiveAndProcessMessages receives and processes messages from SQS
func (h *Handler) receiveAndProcessMessages(ctx context.Context) error {
	// Receive messages
	result, err := h.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(h.config.QueueURL),
		MaxNumberOfMessages: h.config.MaxNumberOfMessages,
		WaitTimeSeconds:     h.config.WaitTimeSeconds,
		VisibilityTimeout:   h.config.VisibilityTimeoutSeconds,
	})

	if err != nil {
		return fmt.Errorf("failed to receive messages: %w", err)
	}

	// Process each message concurrently
	for _, message := range result.Messages {
		// Track message reception
		atomic.AddInt64(&h.stats.MessagesReceived, 1)
		atomic.AddInt64(&h.stats.MessagesInFlight, 1)

		h.stats.mu.Lock()
		h.stats.LastMessageTime = time.Now()
		h.stats.mu.Unlock()

		// Process message in goroutine
		h.wg.Add(1)
		go h.processMessage(ctx, message)
	}

	return nil
}

// processMessage processes a single message
func (h *Handler) processMessage(ctx context.Context, message types.Message) {
	defer h.wg.Done()
	defer atomic.AddInt64(&h.stats.MessagesInFlight, -1)

	// Process the message
	if err := h.processor.ProcessMessage(ctx, message); err != nil {
		h.logger.Printf("SQS Handler: Failed to process message %s: %v",
			aws.ToString(message.MessageId), err)
		atomic.AddInt64(&h.stats.MessagesFailed, 1)
		h.addProcessingError(fmt.Sprintf("MessageId %s: %v",
			aws.ToString(message.MessageId), err))
		return
	}

	// Delete message from queue on successful processing
	_, err := h.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(h.config.QueueURL),
		ReceiptHandle: message.ReceiptHandle,
	})

	if err != nil {
		h.logger.Printf("SQS Handler: Failed to delete message %s: %v",
			aws.ToString(message.MessageId), err)
		h.addProcessingError(fmt.Sprintf("Delete failed for MessageId %s: %v",
			aws.ToString(message.MessageId), err))
		return
	}

	atomic.AddInt64(&h.stats.MessagesProcessed, 1)
	h.logger.Printf("SQS Handler: Successfully processed message %s",
		aws.ToString(message.MessageId))
}

// addProcessingError adds an error to the processing errors list (keeps last 10)
func (h *Handler) addProcessingError(errorMsg string) {
	h.stats.mu.Lock()
	defer h.stats.mu.Unlock()

	h.stats.ProcessingErrors = append(h.stats.ProcessingErrors,
		fmt.Sprintf("%s: %s", time.Now().Format(time.RFC3339), errorMsg))

	// Keep only last 10 errors
	if len(h.stats.ProcessingErrors) > 10 {
		h.stats.ProcessingErrors = h.stats.ProcessingErrors[1:]
	}
}
