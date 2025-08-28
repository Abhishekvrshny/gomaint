package adapters

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	kafkaHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/kafka"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// ConfluentMessage wraps a kafka.Message to implement the generic Message interface
type ConfluentMessage struct {
	msg *kafka.Message
}

// Topic returns the topic name
func (cm *ConfluentMessage) Topic() string {
	if cm.msg.TopicPartition.Topic != nil {
		return *cm.msg.TopicPartition.Topic
	}
	return ""
}

// Partition returns the partition number
func (cm *ConfluentMessage) Partition() int32 {
	return cm.msg.TopicPartition.Partition
}

// Offset returns the message offset
func (cm *ConfluentMessage) Offset() int64 {
	return int64(cm.msg.TopicPartition.Offset)
}

// Key returns the message key
func (cm *ConfluentMessage) Key() []byte {
	return cm.msg.Key
}

// Value returns the message value/body
func (cm *ConfluentMessage) Value() []byte {
	return cm.msg.Value
}

// Headers returns the message headers as key-value pairs
func (cm *ConfluentMessage) Headers() map[string][]byte {
	headers := make(map[string][]byte)
	for _, header := range cm.msg.Headers {
		headers[header.Key] = header.Value
	}
	return headers
}

// Timestamp returns when the message was produced
func (cm *ConfluentMessage) Timestamp() time.Time {
	return cm.msg.Timestamp
}

// ConfluentAdapter wraps a kafka.Consumer to implement the generic Consumer interface
type ConfluentAdapter struct {
	consumer  *kafka.Consumer
	topics    []string
	processor kafkaHandler.MessageProcessor
	
	// State management
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	stopChan      chan struct{}
	isRunning     bool
	mu            sync.RWMutex
	
	// Configuration
	pollTimeout time.Duration
	
	// Message tracking for graceful draining
	activeMessages int64
}

// NewConfluentAdapter creates a new Confluent adapter
func NewConfluentAdapter(consumer *kafka.Consumer, topics []string, processor kafkaHandler.MessageProcessor) *ConfluentAdapter {
	return &ConfluentAdapter{
		consumer:    consumer,
		topics:      topics,
		processor:   processor,
		stopChan:    make(chan struct{}),
		pollTimeout: time.Second, // Default 1 second poll timeout
	}
}

// WithPollTimeout sets the polling timeout for the consumer
func (ca *ConfluentAdapter) WithPollTimeout(timeout time.Duration) *ConfluentAdapter {
	ca.pollTimeout = timeout
	return ca
}

// Start begins consuming messages (non-blocking)
func (ca *ConfluentAdapter) Start(ctx context.Context) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	
	if ca.isRunning {
		return nil // Already running
	}
	
	// Subscribe to topics
	if err := ca.consumer.SubscribeTopics(ca.topics, nil); err != nil {
		return err
	}
	
	ca.ctx, ca.cancel = context.WithCancel(ctx)
	ca.isRunning = true
	
	// Start consumer loop
	ca.wg.Add(1)
	go ca.consumeLoop()
	
	return nil
}

// Stop stops consuming messages gracefully
func (ca *ConfluentAdapter) Stop() error {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	
	if !ca.isRunning {
		return nil // Already stopped
	}
	
	// Cancel context and close stop channel
	if ca.cancel != nil {
		ca.cancel()
	}
	close(ca.stopChan)
	ca.isRunning = false
	
	// Wait for consumer loop to finish
	ca.mu.Unlock()
	ca.wg.Wait()
	ca.mu.Lock()
	
	return nil
}

// IsHealthy returns true if the consumer is connected and healthy
func (ca *ConfluentAdapter) IsHealthy() bool {
	if ca.consumer == nil {
		return false
	}
	
	// Try to get metadata to check connectivity
	metadata, err := ca.consumer.GetMetadata(nil, false, int(ca.pollTimeout.Milliseconds()))
	if err != nil {
		return false
	}
	
	return len(metadata.Brokers) > 0
}

// ActiveMessages returns the count of messages currently being processed
func (ca *ConfluentAdapter) ActiveMessages() int64 {
	return atomic.LoadInt64(&ca.activeMessages)
}

// consumeLoop is the main consumer loop
func (ca *ConfluentAdapter) consumeLoop() {
	defer ca.wg.Done()
	
	for {
		select {
		case <-ca.ctx.Done():
			return
		case <-ca.stopChan:
			return
		default:
			// Poll for messages
			msg, err := ca.consumer.ReadMessage(ca.pollTimeout)
			if err != nil {
				// Check if it's a timeout (not an actual error)
				if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
					continue // Timeout is expected, continue polling
				}
				// Handle other errors (in a real implementation, you might want to retry with backoff)
				continue
			}
			
			if msg != nil {
				// Track active message
				atomic.AddInt64(&ca.activeMessages, 1)
				
				// Process message in goroutine
				ca.wg.Add(1)
				go ca.processMessage(msg)
			}
		}
	}
}

// processMessage processes a single message
func (ca *ConfluentAdapter) processMessage(msg *kafka.Message) {
	defer ca.wg.Done()
	defer atomic.AddInt64(&ca.activeMessages, -1)
	
	// Convert to generic message
	genericMessage := &ConfluentMessage{msg: msg}
	
	// Process the message using the generic processor
	if err := ca.processor.ProcessMessage(ca.ctx, genericMessage); err != nil {
		// Handle error (in a real implementation, you might want to retry or send to DLQ)
		return
	}
	
	// Commit the message offset
	if _, err := ca.consumer.CommitMessage(msg); err != nil {
		// Handle commit error (in a real implementation, you might want to retry)
	}
}

// ConfluentConsumerBuilder helps create a properly configured kafka.Consumer
type ConfluentConsumerBuilder struct {
	config kafka.ConfigMap
}

// NewConfluentConsumerBuilder creates a new builder for kafka.Consumer
func NewConfluentConsumerBuilder(brokers, groupID string) *ConfluentConsumerBuilder {
	return &ConfluentConsumerBuilder{
		config: kafka.ConfigMap{
			"bootstrap.servers": brokers,
			"group.id":          groupID,
			"auto.offset.reset": "latest",
			"enable.auto.commit": false, // Manual commit for better control
		},
	}
}


// WithAutoOffsetReset sets the auto.offset.reset strategy
func (b *ConfluentConsumerBuilder) WithAutoOffsetReset(strategy string) *ConfluentConsumerBuilder {
	b.config["auto.offset.reset"] = strategy
	return b
}

// WithSessionTimeout sets the session timeout
func (b *ConfluentConsumerBuilder) WithSessionTimeout(timeout string) *ConfluentConsumerBuilder {
	b.config["session.timeout.ms"] = timeout
	return b
}


// Build creates the kafka.Consumer
func (b *ConfluentConsumerBuilder) Build() (*kafka.Consumer, error) {
	consumer, err := kafka.NewConsumer(&b.config)
	if err != nil {
		return nil, err
	}
	
	return consumer, nil
}