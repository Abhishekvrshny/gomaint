package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/abhishekvarshney/gomaint"
	kafkaHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/kafka"
	"github.com/abhishekvarshney/gomaint/pkg/handlers/kafka/adapters"
)

// MessageProcessor implements the Kafka message processing logic
type MessageProcessor struct {
	logger *log.Logger
}

// ProcessMessage processes a single Kafka message
func (mp *MessageProcessor) ProcessMessage(ctx context.Context, message kafkaHandler.Message) error {
	messageBody := string(message.Value())

	mp.logger.Printf("Processing message from topic %s partition %d offset %d: %s",
		message.Topic(), message.Partition(), message.Offset(), messageBody)

	// Simulate processing time (1-3 seconds)
	processingTime := time.Duration(1+rand.Intn(3)) * time.Second

	// Check for context cancellation during processing
	timer := time.NewTimer(processingTime)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Processing completed successfully
		mp.logger.Printf("Successfully processed message from topic %s partition %d offset %d after %v",
			message.Topic(), message.Partition(), message.Offset(), processingTime)
		return nil
	case <-ctx.Done():
		mp.logger.Printf("Processing cancelled for message from topic %s partition %d offset %d: %v",
			message.Topic(), message.Partition(), message.Offset(), ctx.Err())
		return ctx.Err()
	}
}

// App holds the application dependencies
type App struct {
	consumer      *kafka.Consumer
	kafkaHandler  *kafkaHandler.Handler
	kafkaProducer *kafka.Producer
	manager       *gomaint.Manager
	server        *http.Server
	topics        []string
}

func main() {
	fmt.Println("Confluent Kafka Service with Maintenance Mode")
	fmt.Println("==============================================")

	app, err := setupApp()
	if err != nil {
		log.Fatalf("Failed to setup application: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start the application
	if err := app.run(ctx); err != nil {
		log.Fatalf("Application failed: %v", err)
	}

	log.Println("Application stopped")
}

func setupApp() (*App, error) {
	// Get configuration from environment
	brokersStr := getEnv("KAFKA_BROKERS", "kafka:9092")
	topicsStr := getEnv("KAFKA_TOPICS", "test-topic,user-events,system-logs")
	consumerGroupName := getEnv("KAFKA_CONSUMER_GROUP", "confluent-gomaint-consumer-group")
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", "etcd:2379")

	brokers := strings.Split(brokersStr, ",")
	topics := strings.Split(topicsStr, ",")

	log.Printf("Configuring Kafka client for brokers: %v", brokers)
	log.Printf("Topics: %v, Consumer Group: %s", topics, consumerGroupName)

	// Create message processor
	messageProcessor := &MessageProcessor{
		logger: log.Default(),
	}

	// Create Confluent consumer using the builder
	confluentConsumer, err := adapters.NewConfluentConsumerBuilder(brokersStr, consumerGroupName).
		WithAutoOffsetReset("earliest"). // Start from oldest unread messages
		WithSessionTimeout("30000"). // 30 seconds
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create Confluent consumer: %w", err)
	}

	// Create Confluent adapter
	consumer := adapters.NewConfluentAdapter(confluentConsumer, topics, messageProcessor)

	// Create Kafka handler configuration
	config := kafkaHandler.DefaultConfig()
	config.DrainTimeout = 45 * time.Second // Allow more time for message draining

	// Create generic Kafka handler with Confluent adapter
	handler := kafkaHandler.NewKafkaHandler(consumer, config, log.Default())

	// Create Confluent producer for sending test messages
	producerConfig := &kafka.ConfigMap{
		"bootstrap.servers": brokersStr,
		"acks":              "all",
		"retries":           3,
		"compression.type":  "snappy",
	}

	producer, err := kafka.NewProducer(producerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Confluent producer: %w", err)
	}

	// Create maintenance manager using new simplified API
	mgr, err := gomaint.StartWithEtcd(
		context.Background(),
		strings.Split(etcdEndpoints, ","),
		"/maintenance/confluent-kafka-service",
		30*time.Second,
		handler, // Register handler during creation
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create maintenance manager: %w", err)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	app := &App{
		consumer:      confluentConsumer,
		kafkaHandler:  handler,
		kafkaProducer: producer,
		manager:       mgr,
		topics:        topics,
		server: &http.Server{
			Addr:    ":8085", // Different port than other services
			Handler: mux,
		},
	}

	// Setup routes
	app.setupRoutes(mux)

	// Send initial test messages
	if err := app.sendInitialMessages(context.Background()); err != nil {
		log.Printf("Warning: Failed to send initial messages: %v", err)
		// Don't fail startup for this
	}

	return app, nil
}


func (app *App) sendInitialMessages(ctx context.Context) error {
	log.Println("Sending initial test messages to topics...")

	messages := []struct {
		topic   string
		message string
	}{
		{"test-topic", "Initial test message - " + time.Now().Format(time.RFC3339)},
		{"user-events", `{"event": "user_login", "user_id": "user123", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`},
		{"system-logs", `{"level": "info", "message": "Confluent Kafka service started", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`},
	}

	for _, msg := range messages {
		// Skip if topic is not in our configured topics
		found := false
		for _, configuredTopic := range app.topics {
			if configuredTopic == msg.topic {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		kafkaMsg := &kafka.Message{
			TopicPartition: kafka.TopicPartition{Topic: &msg.topic, Partition: kafka.PartitionAny},
			Key:            []byte("initial"),
			Value:          []byte(msg.message),
			Headers: []kafka.Header{
				{
					Key:   "source",
					Value: []byte("confluent-kafka-service-init"),
				},
			},
		}

		deliveryChan := make(chan kafka.Event)
		err := app.kafkaProducer.Produce(kafkaMsg, deliveryChan)
		if err != nil {
			return fmt.Errorf("failed to produce initial message to topic %s: %w", msg.topic, err)
		}

		// Wait for delivery confirmation
		e := <-deliveryChan
		m := e.(*kafka.Message)
		if m.TopicPartition.Error != nil {
			return fmt.Errorf("failed to send initial message to topic %s: %w", msg.topic, m.TopicPartition.Error)
		}
		log.Printf("Sent initial message to topic %s partition %d offset %v", msg.topic, m.TopicPartition.Partition, m.TopicPartition.Offset)
		close(deliveryChan)
	}

	log.Println("Successfully sent initial test messages")
	return nil
}

func (app *App) setupRoutes(mux *http.ServeMux) {
	// Health check endpoint
	mux.HandleFunc("/health", app.healthHandler)

	// Kafka management endpoints
	mux.HandleFunc("/messages/send", app.sendMessageHandler)
	mux.HandleFunc("/messages/stats", app.statsHandler)

	// Topic management endpoints
	mux.HandleFunc("/topics/info", app.topicsInfoHandler)
}

func (app *App) run(ctx context.Context) error {
	// Maintenance manager is already started by StartWithEtcd

	// Start Kafka message processing
	if err := app.kafkaHandler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start Kafka handler: %w", err)
	}

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on %s", app.server.Addr)
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server failed: %v", err)
		}
	}()

	fmt.Printf("Server running on http://localhost:8085\\n")
	fmt.Println("Endpoints:")
	fmt.Println("  Health check: http://localhost:8085/health")
	fmt.Println("  Send message: http://localhost:8085/messages/send")
	fmt.Println("  Stats: http://localhost:8085/messages/stats")
	fmt.Println("  Topics info: http://localhost:8085/topics/info")
	fmt.Println()
	fmt.Println("To enable maintenance mode:")
	fmt.Println("  etcdctl --endpoints=localhost:2401 put /maintenance/confluent-kafka-service true")
	fmt.Println()
	fmt.Println("To disable maintenance mode:")
	fmt.Println("  etcdctl --endpoints=localhost:2401 put /maintenance/confluent-kafka-service false")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop Kafka handler
	app.kafkaHandler.Stop()

	// Close Kafka producer
	if app.kafkaProducer != nil {
		app.kafkaProducer.Close()
	}

	// Close consumer
	if app.consumer != nil {
		app.consumer.Close()
	}

	// Stop maintenance manager
	app.manager.Stop()

	// Shutdown HTTP server
	return app.server.Shutdown(shutdownCtx)
}

func (app *App) healthHandler(w http.ResponseWriter, r *http.Request) {
	if app.manager.IsInMaintenance() {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "Service Unavailable",
			"message": "Service is currently under maintenance. Please try again later.",
			"code":    503,
		})
		return
	}

	// Check handler health
	health := app.manager.GetHandlerHealth()
	allHealthy := true
	for _, healthy := range health {
		if !healthy {
			allHealthy = false
			break
		}
	}

	status := "healthy"
	statusCode := http.StatusOK
	if !allHealthy {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      status,
		"maintenance": app.manager.IsInMaintenance(),
		"handlers":    health,
		"topics":      app.topics,
		"timestamp":   time.Now().UTC(),
	})
}

func (app *App) sendMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Topic   string `json:"topic"`
		Message string `json:"message"`
		Key     string `json:"key,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Default topic if not specified
	if request.Topic == "" {
		request.Topic = "test-topic"
	}

	// Default message if not specified
	if request.Message == "" {
		request.Message = fmt.Sprintf("Test message sent at %s", time.Now().Format(time.RFC3339))
	}

	// Default key if not specified
	if request.Key == "" {
		request.Key = "api-generated"
	}

	// Check if topic is in our configured topics
	validTopic := false
	for _, topic := range app.topics {
		if topic == request.Topic {
			validTopic = true
			break
		}
	}
	if !validTopic {
		http.Error(w, fmt.Sprintf("Topic %s is not configured. Available topics: %v", request.Topic, app.topics), http.StatusBadRequest)
		return
	}

	// Send message to Kafka using Confluent
	kafkaMsg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &request.Topic, Partition: kafka.PartitionAny},
		Key:            []byte(request.Key),
		Value:          []byte(request.Message),
		Headers: []kafka.Header{
			{
				Key:   "source",
				Value: []byte("confluent-kafka-service-api"),
			},
			{
				Key:   "timestamp",
				Value: []byte(time.Now().Format(time.RFC3339)),
			},
		},
	}

	deliveryChan := make(chan kafka.Event, 1)
	defer close(deliveryChan)
	
	err := app.kafkaProducer.Produce(kafkaMsg, deliveryChan)
	if err != nil {
		log.Printf("Failed to produce message to topic %s: %v", request.Topic, err)
		http.Error(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	// Wait for delivery confirmation
	e := <-deliveryChan
	m := e.(*kafka.Message)
	if m.TopicPartition.Error != nil {
		log.Printf("Failed to deliver message to topic %s: %v", request.Topic, m.TopicPartition.Error)
		http.Error(w, "Failed to deliver message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"topic":     request.Topic,
		"partition": m.TopicPartition.Partition,
		"offset":    m.TopicPartition.Offset,
		"key":       request.Key,
		"message":   request.Message,
		"timestamp": time.Now().UTC(),
	})
}

func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	// Get Kafka handler stats
	var kafkaStats map[string]interface{}
	if handler, exists := app.manager.GetHandler("kafka"); exists {
		if kh, ok := handler.(*kafkaHandler.Handler); ok {
			kafkaStats = kh.GetStats()
		}
	}

	stats := map[string]interface{}{
		"maintenance": app.manager.IsInMaintenance(),
		"timestamp":   time.Now().UTC(),
		"handlers":    app.manager.GetHandlerHealth(),
		"kafka":       kafkaStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (app *App) topicsInfoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"topics":    app.topics,
		"timestamp": time.Now().UTC(),
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
