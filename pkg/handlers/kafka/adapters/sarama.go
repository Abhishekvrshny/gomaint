package adapters

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"github.com/abhishekvarshney/gomaint/pkg/handlers/kafka"
)

// SaramaMessage wraps a sarama.ConsumerMessage to implement the generic Message interface
type SaramaMessage struct {
	msg *sarama.ConsumerMessage
}

// Topic returns the topic name
func (sm *SaramaMessage) Topic() string {
	return sm.msg.Topic
}

// Partition returns the partition number
func (sm *SaramaMessage) Partition() int32 {
	return sm.msg.Partition
}

// Offset returns the message offset
func (sm *SaramaMessage) Offset() int64 {
	return sm.msg.Offset
}

// Key returns the message key
func (sm *SaramaMessage) Key() []byte {
	return sm.msg.Key
}

// Value returns the message value/body
func (sm *SaramaMessage) Value() []byte {
	return sm.msg.Value
}

// Headers returns the message headers as key-value pairs
func (sm *SaramaMessage) Headers() map[string][]byte {
	headers := make(map[string][]byte)
	for _, header := range sm.msg.Headers {
		headers[string(header.Key)] = header.Value
	}
	return headers
}

// Timestamp returns when the message was produced
func (sm *SaramaMessage) Timestamp() time.Time {
	return sm.msg.Timestamp
}

// SaramaAdapter wraps a sarama.ConsumerGroup to implement the generic Consumer interface
type SaramaAdapter struct {
	consumerGroup sarama.ConsumerGroup
	topics        []string
	processor     kafka.MessageProcessor
	
	// State management
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	stopChan      chan struct{}
	isRunning     bool
	mu            sync.RWMutex
	
	// Message tracking for graceful draining
	activeMessages int64
}

// NewSaramaAdapter creates a new Sarama adapter
func NewSaramaAdapter(consumerGroup sarama.ConsumerGroup, topics []string, processor kafka.MessageProcessor) *SaramaAdapter {
	return &SaramaAdapter{
		consumerGroup: consumerGroup,
		topics:        topics,
		processor:     processor,
		stopChan:      make(chan struct{}),
	}
}

// Start begins consuming messages (non-blocking)
func (sa *SaramaAdapter) Start(ctx context.Context) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	
	if sa.isRunning {
		return nil // Already running
	}
	
	sa.ctx, sa.cancel = context.WithCancel(ctx)
	sa.isRunning = true
	
	// Start consumer group handler
	sa.wg.Add(1)
	go sa.consumeLoop()
	
	return nil
}

// Stop stops consuming messages gracefully
func (sa *SaramaAdapter) Stop() error {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	
	if !sa.isRunning {
		return nil // Already stopped
	}
	
	// Cancel context and close stop channel
	if sa.cancel != nil {
		sa.cancel()
	}
	close(sa.stopChan)
	sa.isRunning = false
	
	// Wait for consumer loop to finish
	sa.mu.Unlock()
	sa.wg.Wait()
	sa.mu.Lock()
	
	return nil
}

// IsHealthy returns true if the consumer is connected and healthy
func (sa *SaramaAdapter) IsHealthy() bool {
	if sa.consumerGroup == nil {
		return false
	}
	
	// Check if consumer group is closed
	select {
	case <-sa.consumerGroup.Errors():
		return false
	default:
		return true
	}
}

// ActiveMessages returns the count of messages currently being processed
func (sa *SaramaAdapter) ActiveMessages() int64 {
	return atomic.LoadInt64(&sa.activeMessages)
}

// consumeLoop is the main consumer loop
func (sa *SaramaAdapter) consumeLoop() {
	defer sa.wg.Done()
	
	handler := &saramaConsumerHandler{
		adapter: sa,
	}
	
	for {
		select {
		case <-sa.ctx.Done():
			return
		case <-sa.stopChan:
			return
		case err := <-sa.consumerGroup.Errors():
			if err != nil {
				// Log error but continue (in a real implementation, you might want to handle this differently)
				continue
			}
		default:
			// Consume messages from topics
			if err := sa.consumerGroup.Consume(sa.ctx, sa.topics, handler); err != nil {
				// Handle error (in a real implementation, you might want to retry with backoff)
				continue
			}
		}
	}
}

// saramaConsumerHandler implements sarama.ConsumerGroupHandler for the adapter
type saramaConsumerHandler struct {
	adapter *SaramaAdapter
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (h *saramaConsumerHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (h *saramaConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages()
func (h *saramaConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message := <-claim.Messages():
			if message == nil {
				return nil
			}
			
			// Track active message
			atomic.AddInt64(&h.adapter.activeMessages, 1)
			
			// Process message in goroutine
			h.adapter.wg.Add(1)
			go h.processMessage(session, message)
			
		case <-session.Context().Done():
			return nil
		}
	}
}

// processMessage processes a single message
func (h *saramaConsumerHandler) processMessage(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage) {
	defer h.adapter.wg.Done()
	defer atomic.AddInt64(&h.adapter.activeMessages, -1)
	
	// Convert to generic message
	genericMessage := &SaramaMessage{msg: msg}
	
	// Process the message using the generic processor
	if err := h.adapter.processor.ProcessMessage(session.Context(), genericMessage); err != nil {
		// Handle error (in a real implementation, you might want to retry or send to DLQ)
		return
	}
	
	// Mark message as processed
	session.MarkMessage(msg, "")
	
	// Commit the offset (you might want to batch commits for better performance)
	session.Commit()
}

// SaramaConsumerGroupBuilder helps create a properly configured sarama.ConsumerGroup
type SaramaConsumerGroupBuilder struct {
	brokers       []string
	consumerGroup string
	config        *sarama.Config
}

// NewSaramaConsumerGroupBuilder creates a new builder for sarama.ConsumerGroup
func NewSaramaConsumerGroupBuilder(brokers []string, consumerGroup string) *SaramaConsumerGroupBuilder {
	config := sarama.NewConfig()
	config.Version = sarama.V2_8_0_0
	config.Consumer.Group.Session.Timeout = 30 * time.Second
	config.Consumer.Group.Heartbeat.Interval = 3 * time.Second
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	config.Consumer.Return.Errors = true
	config.Consumer.Offsets.AutoCommit.Enable = false // Manual commit for better control
	
	return &SaramaConsumerGroupBuilder{
		brokers:       brokers,
		consumerGroup: consumerGroup,
		config:        config,
	}
}


// WithOffsetInitial sets the initial offset strategy
func (b *SaramaConsumerGroupBuilder) WithOffsetInitial(offset int64) *SaramaConsumerGroupBuilder {
	b.config.Consumer.Offsets.Initial = offset
	return b
}

// WithSessionTimeout sets the session timeout
func (b *SaramaConsumerGroupBuilder) WithSessionTimeout(timeout time.Duration) *SaramaConsumerGroupBuilder {
	b.config.Consumer.Group.Session.Timeout = timeout
	return b
}

// Build creates the sarama.ConsumerGroup
func (b *SaramaConsumerGroupBuilder) Build() (sarama.ConsumerGroup, error) {
	client, err := sarama.NewClient(b.brokers, b.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}
	
	consumerGroup, err := sarama.NewConsumerGroupFromClient(b.consumerGroup, client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}
	
	return consumerGroup, nil
}