package kafka

import (
	"context"
	"time"
)

// Message represents a generic Kafka message that abstracts away library-specific implementations
type Message interface {
	// Topic returns the topic name
	Topic() string

	// Partition returns the partition number
	Partition() int32

	// Offset returns the message offset
	Offset() int64

	// Key returns the message key
	Key() []byte

	// Value returns the message value/body
	Value() []byte

	// Headers returns the message headers as key-value pairs
	Headers() map[string][]byte

	// Timestamp returns when the message was produced
	Timestamp() time.Time
}

// Consumer represents a generic Kafka consumer that abstracts away library-specific implementations
type Consumer interface {
	// Start begins consuming messages (non-blocking)
	Start(ctx context.Context) error

	// Stop stops consuming messages gracefully
	Stop() error

	// IsHealthy returns true if the consumer is connected and healthy
	IsHealthy() bool

	// ActiveMessages returns the count of messages currently being processed
	// This is used for graceful draining during maintenance mode
	ActiveMessages() int64
}

// MessageProcessor defines the interface for processing Kafka messages
// This interface is library-agnostic and focuses on business logic
type MessageProcessor interface {
	// ProcessMessage processes a single message
	// The implementation should focus on business logic, not Kafka-specific concerns
	ProcessMessage(ctx context.Context, message Message) error
}

// MessageCommitter is an optional interface that consumers can implement
// to provide custom message commit behavior
type MessageCommitter interface {
	// Commit marks the message as processed
	// Some libraries handle this automatically, others require explicit commits
	Commit(ctx context.Context, message Message) error
}
