package kafka

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// KafkaClient interface allows for easier testing and mocking
type KafkaClient interface {
	sarama.Client
}

// MessageProcessor defines the interface for processing Kafka messages
type MessageProcessor interface {
	ProcessMessage(ctx context.Context, message *sarama.ConsumerMessage) error
}

// Config holds the configuration for the Kafka handler
type Config struct {
	Brokers           []string
	Topics            []string
	ConsumerGroup     string
	BatchSize         int
	SessionTimeout    time.Duration
	HeartbeatInterval time.Duration
	DrainTimeout      time.Duration
	MaxProcessingTime time.Duration
	// Consumer configuration
	OffsetInitial      int64 // sarama.OffsetOldest or sarama.OffsetNewest
	EnableAutoCommit   bool
	AutoCommitInterval time.Duration
	RetryBackoff       time.Duration
	MaxWorkers         int
}

// DefaultConfig returns a default configuration
func DefaultConfig(brokers []string, topics []string, consumerGroup string) *Config {
	return &Config{
		Brokers:            brokers,
		Topics:             topics,
		ConsumerGroup:      consumerGroup,
		BatchSize:          100,
		SessionTimeout:     30 * time.Second,
		HeartbeatInterval:  3 * time.Second,
		DrainTimeout:       45 * time.Second,
		MaxProcessingTime:  30 * time.Second,
		OffsetInitial:      sarama.OffsetNewest,
		EnableAutoCommit:   false, // Manual commit for better control
		AutoCommitInterval: 1 * time.Second,
		RetryBackoff:       2 * time.Second,
		MaxWorkers:         10,
	}
}

// Handler implements the Handler interface for Kafka message processing
type Handler struct {
	*handlers.BaseHandler
	client        sarama.Client
	consumerGroup sarama.ConsumerGroup
	config        *Config
	processor     MessageProcessor
	logger        *log.Logger

	// State management
	inMaintenance int32 // atomic boolean
	stopChan      chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
	isRunning     bool
	ctx           context.Context
	cancel        context.CancelFunc

	// Worker pool for message processing
	workerPool chan struct{}

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
	ConsumerLag       map[string]map[int32]int64 // topic -> partition -> lag
	mu                sync.RWMutex
}

// ConsumerGroupHandler implements sarama.ConsumerGroupHandler
type ConsumerGroupHandler struct {
	handler *Handler
}

// NewKafkaHandler creates a new Kafka handler
func NewKafkaHandler(brokers []string, config *Config, processor MessageProcessor, logger *log.Logger) (*Handler, error) {
	if logger == nil {
		logger = log.Default()
	}

	if config == nil {
		config = DefaultConfig(brokers, []string{"test-topic"}, "gomaint-consumer-group")
	}

	// Create Kafka client configuration
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Version = sarama.V2_8_0_0
	kafkaConfig.Consumer.Group.Session.Timeout = config.SessionTimeout
	kafkaConfig.Consumer.Group.Heartbeat.Interval = config.HeartbeatInterval
	kafkaConfig.Consumer.Offsets.Initial = config.OffsetInitial
	kafkaConfig.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRoundRobin
	kafkaConfig.Consumer.Return.Errors = true
	kafkaConfig.Consumer.Offsets.AutoCommit.Enable = config.EnableAutoCommit
	kafkaConfig.Consumer.Offsets.AutoCommit.Interval = config.AutoCommitInterval
	kafkaConfig.Consumer.Fetch.Default = 1024 * 1024 // 1MB
	kafkaConfig.Consumer.MaxProcessingTime = config.MaxProcessingTime

	// Create Kafka client
	client, err := sarama.NewClient(config.Brokers, kafkaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	// Create consumer group
	consumerGroup, err := sarama.NewConsumerGroupFromClient(config.ConsumerGroup, client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}

	handler := &Handler{
		BaseHandler:   handlers.NewBaseHandler("kafka"),
		client:        client,
		consumerGroup: consumerGroup,
		config:        config,
		processor:     processor,
		logger:        logger,
		stopChan:      make(chan struct{}),
		workerPool:    make(chan struct{}, config.MaxWorkers),
		stats: Stats{
			ConsumerLag: make(map[string]map[int32]int64),
		},
	}

	return handler, nil
}

// Start begins message processing (non-blocking)
func (h *Handler) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isRunning {
		h.logger.Println("Kafka Handler: Already running, skipping start")
		return nil
	}

	h.logger.Printf("Kafka Handler: Starting message processing for topics: %v", h.config.Topics)

	// Create a new context for the handler that's independent of the passed context
	h.ctx, h.cancel = context.WithCancel(context.Background())

	// Create a new stop channel if needed
	if h.stopChan == nil {
		h.stopChan = make(chan struct{})
	}

	// Start the consumer group
	h.wg.Add(1)
	h.isRunning = true
	go h.consumerLoop(h.ctx)

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

	// Cancel the handler context
	if h.cancel != nil {
		h.cancel()
	}

	close(h.stopChan)
	h.isRunning = false

	// Wait for consumer loop to finish (unlock mutex first)
	h.mu.Unlock()
	h.wg.Wait()
	h.mu.Lock()

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

	// Wait for in-flight messages to complete or timeout
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		h.logger.Println("Kafka Handler: All messages drained successfully")
		return nil
	case <-time.After(h.config.DrainTimeout):
		inFlight := atomic.LoadInt64(&h.stats.MessagesInFlight)
		h.logger.Printf("Kafka Handler: Drain timeout reached with %d messages still in flight", inFlight)
		return fmt.Errorf("timeout waiting for %d messages to drain after %v", inFlight, h.config.DrainTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OnMaintenanceEnd handles maintenance mode deactivation
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	h.logger.Println("Kafka Handler: Exiting maintenance mode - resuming message processing")
	atomic.StoreInt32(&h.inMaintenance, 0)
	h.SetState(handlers.StateNormal)

	// Restart the handler if it was running
	h.mu.Lock()
	wasRunning := h.isRunning
	h.mu.Unlock()

	if !wasRunning {
		// Start the handler with a fresh context
		if err := h.Start(ctx); err != nil {
			h.logger.Printf("Kafka Handler: Failed to restart after maintenance: %v", err)
			return err
		}
		h.logger.Println("Kafka Handler: Message processing resumed after maintenance")
	} else {
		h.logger.Println("Kafka Handler: Message processing will continue automatically")
	}

	return nil
}

// IsHealthy performs Kafka health check
func (h *Handler) IsHealthy() bool {
	// First check the base handler state
	if !h.BaseHandler.IsHealthy() {
		return false
	}

	// Check Kafka client connectivity
	if h.client == nil || h.client.Closed() {
		h.logger.Println("Kafka Handler: Health check failed - client is closed")
		return false
	}

	// Check if we can get broker list
	brokers := h.client.Brokers()
	if len(brokers) == 0 {
		h.logger.Println("Kafka Handler: Health check failed - no brokers available")
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
		"topics":             h.config.Topics,
		"consumer_group":     h.config.ConsumerGroup,
		"brokers":            h.config.Brokers,
		"in_maintenance":     atomic.LoadInt32(&h.inMaintenance) == 1,
		"messages_received":  h.stats.MessagesReceived,
		"messages_processed": h.stats.MessagesProcessed,
		"messages_failed":    h.stats.MessagesFailed,
		"messages_in_flight": h.stats.MessagesInFlight,
		"last_message_time":  h.stats.LastMessageTime,
		"drain_timeout":      h.config.DrainTimeout.String(),
		"max_workers":        h.config.MaxWorkers,
		"consumer_lag":       h.stats.ConsumerLag,
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

// consumerLoop is the main consumer group loop
func (h *Handler) consumerLoop(ctx context.Context) {
	defer h.wg.Done()
	defer func() {
		h.mu.Lock()
		h.isRunning = false
		h.mu.Unlock()
	}()

	h.logger.Println("Kafka Handler: Consumer loop started")

	groupHandler := &ConsumerGroupHandler{handler: h}

	for {
		select {
		case <-ctx.Done():
			h.logger.Println("Kafka Handler: Context cancelled, stopping consumer loop")
			return
		case <-h.stopChan:
			h.logger.Println("Kafka Handler: Stop signal received, stopping consumer loop")
			return
		case err := <-h.consumerGroup.Errors():
			if err != nil {
				h.logger.Printf("Kafka Handler: Consumer group error: %v", err)
				h.addProcessingError(err.Error())
			}
		default:
			// Skip consuming new messages if in maintenance mode
			if atomic.LoadInt32(&h.inMaintenance) == 1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Consume messages from topics
			err := h.consumerGroup.Consume(ctx, h.config.Topics, groupHandler)
			if err != nil {
				h.logger.Printf("Kafka Handler: Error consuming messages: %v", err)
				h.addProcessingError(err.Error())

				// Retry after backoff
				select {
				case <-time.After(h.config.RetryBackoff):
				case <-ctx.Done():
					return
				case <-h.stopChan:
					return
				}
			}
		}
	}
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (cgh *ConsumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	cgh.handler.logger.Println("Kafka Handler: Consumer group session setup")
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (cgh *ConsumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	cgh.handler.logger.Println("Kafka Handler: Consumer group session cleanup")
	return nil
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages()
func (cgh *ConsumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// NOTE: Do not move the code below to a goroutine.
	// The `ConsumeClaim` itself is called within a goroutine
	for {
		select {
		case message := <-claim.Messages():
			if message == nil {
				return nil
			}

			// Track message reception
			atomic.AddInt64(&cgh.handler.stats.MessagesReceived, 1)
			atomic.AddInt64(&cgh.handler.stats.MessagesInFlight, 1)

			cgh.handler.stats.mu.Lock()
			cgh.handler.stats.LastMessageTime = time.Now()
			cgh.handler.stats.mu.Unlock()

			// Acquire worker slot (blocks if all workers are busy)
			cgh.handler.workerPool <- struct{}{}

			// Process message in goroutine
			cgh.handler.wg.Add(1)
			go cgh.handler.processMessage(session, message)

		// Should return when `session.Context()` is done.
		// If not, will raise `ErrRebalanceInProgress` or `ErrNotCoordinatorForConsumer` error.
		case <-session.Context().Done():
			return nil
		}
	}
}

// processMessage processes a single message
func (h *Handler) processMessage(session sarama.ConsumerGroupSession, message *sarama.ConsumerMessage) {
	defer h.wg.Done()
	defer atomic.AddInt64(&h.stats.MessagesInFlight, -1)
	defer func() { <-h.workerPool }() // Release worker slot

	// Process the message
	if err := h.processor.ProcessMessage(session.Context(), message); err != nil {
		h.logger.Printf("Kafka Handler: Failed to process message from topic %s partition %d offset %d: %v",
			message.Topic, message.Partition, message.Offset, err)
		atomic.AddInt64(&h.stats.MessagesFailed, 1)
		h.addProcessingError(fmt.Sprintf("Topic %s Partition %d Offset %d: %v",
			message.Topic, message.Partition, message.Offset, err))
		return
	}

	// Mark message as processed
	session.MarkMessage(message, "")

	// Commit the offset if auto-commit is disabled
	if !h.config.EnableAutoCommit {
		session.Commit()
	}

	atomic.AddInt64(&h.stats.MessagesProcessed, 1)
	h.logger.Printf("Kafka Handler: Successfully processed message from topic %s partition %d offset %d",
		message.Topic, message.Partition, message.Offset)
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

// Close closes the handler and releases resources
func (h *Handler) Close() error {
	h.Stop()

	var errs []string

	if h.consumerGroup != nil {
		if err := h.consumerGroup.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("consumer group close error: %v", err))
		}
	}

	if h.client != nil {
		if err := h.client.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("client close error: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("kafka handler close errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

// GetTopicMetadata returns metadata about the configured topics
func (h *Handler) GetTopicMetadata() (map[string]interface{}, error) {
	if h.client == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	metadata := make(map[string]interface{})

	for _, topic := range h.config.Topics {
		partitions, err := h.client.Partitions(topic)
		if err != nil {
			metadata[topic] = map[string]interface{}{
				"error": err.Error(),
			}
			continue
		}

		topicInfo := map[string]interface{}{
			"partitions":     len(partitions),
			"partition_list": partitions,
		}

		metadata[topic] = topicInfo
	}

	return metadata, nil
}
