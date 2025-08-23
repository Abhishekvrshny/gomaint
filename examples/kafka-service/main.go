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

	"github.com/IBM/sarama"
	
	"github.com/abhishekvarshney/gomaint"
	kafkaHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/kafka"
	"github.com/abhishekvarshney/gomaint/pkg/manager"
)

// MessageProcessor implements the Kafka message processing logic
type MessageProcessor struct {
	logger *log.Logger
}

// ProcessMessage processes a single Kafka message
func (mp *MessageProcessor) ProcessMessage(ctx context.Context, message *sarama.ConsumerMessage) error {
	messageBody := string(message.Value)
	
	mp.logger.Printf("Processing message from topic %s partition %d offset %d: %s", 
		message.Topic, message.Partition, message.Offset, messageBody)
	
	// Simulate processing time (1-3 seconds)
	processingTime := time.Duration(1+rand.Intn(3)) * time.Second
	
	// Check for context cancellation during processing
	timer := time.NewTimer(processingTime)
	defer timer.Stop()
	
	select {
	case <-timer.C:
		// Processing completed successfully
		mp.logger.Printf("Successfully processed message from topic %s partition %d offset %d after %v", 
			message.Topic, message.Partition, message.Offset, processingTime)
		return nil
	case <-ctx.Done():
		mp.logger.Printf("Processing cancelled for message from topic %s partition %d offset %d: %v", 
			message.Topic, message.Partition, message.Offset, ctx.Err())
		return ctx.Err()
	}
}

// App holds the application dependencies
type App struct {
	kafkaClient   sarama.Client
	kafkaHandler  *kafkaHandler.Handler
	kafkaProducer sarama.SyncProducer
	manager       *manager.Manager
	server        *http.Server
	topics        []string
}

func main() {
	fmt.Println("Kafka Service with Maintenance Mode")
	fmt.Println("==================================")

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
	consumerGroup := getEnv("KAFKA_CONSUMER_GROUP", "gomaint-consumer-group")
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", "etcd:2379")

	brokers := strings.Split(brokersStr, ",")
	topics := strings.Split(topicsStr, ",")

	log.Printf("Configuring Kafka client for brokers: %v", brokers)
	log.Printf("Topics: %v, Consumer Group: %s", topics, consumerGroup)

	// Create message processor
	messageProcessor := &MessageProcessor{
		logger: log.Default(),
	}

	// Create Kafka handler configuration
	config := kafkaHandler.DefaultConfig(brokers, topics, consumerGroup)
	config.DrainTimeout = 45 * time.Second // Allow more time for message draining
	config.MaxWorkers = 5
	config.SessionTimeout = 30 * time.Second
	config.HeartbeatInterval = 3 * time.Second
	config.OffsetInitial = sarama.OffsetOldest // Start from oldest unread messages

	// Create Kafka handler
	handler, err := kafkaHandler.NewKafkaHandler(brokers, config, messageProcessor, log.Default())
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka handler: %w", err)
	}

	// Create Kafka producer for sending test messages
	producerConfig := sarama.NewConfig()
	producerConfig.Producer.Return.Successes = true
	producerConfig.Producer.RequiredAcks = sarama.WaitForAll
	producerConfig.Producer.Retry.Max = 3
	producerConfig.Producer.Compression = sarama.CompressionSnappy

	producer, err := sarama.NewSyncProducer(brokers, producerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	// Create maintenance manager
	cfg := gomaint.NewConfig(
		strings.Split(etcdEndpoints, ","),
		"/maintenance/kafka-service",
		30*time.Second,
	)

	mgr := manager.NewManager(cfg)

	// Register Kafka handler
	if err := mgr.RegisterHandler(handler); err != nil {
		return nil, fmt.Errorf("failed to register Kafka handler: %w", err)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	app := &App{
		kafkaHandler:  handler,
		kafkaProducer: producer,
		manager:       mgr,
		topics:        topics,
		server: &http.Server{
			Addr:    ":8080",
			Handler: mux,
		},
	}

	// Setup routes
	app.setupRoutes(mux)

	// Initialize topics (create if they don't exist)
	if err := app.initializeTopics(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize topics: %w", err)
	}

	// Send initial test messages
	if err := app.sendInitialMessages(context.Background()); err != nil {
		log.Printf("Warning: Failed to send initial messages: %v", err)
		// Don't fail startup for this
	}

	return app, nil
}

func (app *App) initializeTopics(ctx context.Context) error {
	log.Println("Initializing Kafka topics...")

	// Get topic metadata (this will trigger topic creation if auto-create is enabled)
	metadata, err := app.kafkaHandler.GetTopicMetadata()
	if err != nil {
		log.Printf("Warning: Could not get topic metadata: %v", err)
		return nil // Don't fail on this
	}

	for topic, info := range metadata {
		log.Printf("Topic %s: %+v", topic, info)
	}

	log.Println("Topic initialization completed")
	return nil
}

func (app *App) sendInitialMessages(ctx context.Context) error {
	log.Println("Sending initial test messages to topics...")

	messages := []struct {
		topic   string
		message string
	}{
		{"test-topic", "Initial test message - " + time.Now().Format(time.RFC3339)},
		{"user-events", `{"event": "user_login", "user_id": "user123", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`},
		{"system-logs", `{"level": "info", "message": "Kafka service started", "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`},
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

		kafkaMsg := &sarama.ProducerMessage{
			Topic: msg.topic,
			Key:   sarama.StringEncoder("initial"),
			Value: sarama.StringEncoder(msg.message),
			Headers: []sarama.RecordHeader{
				{
					Key:   []byte("source"),
					Value: []byte("kafka-service-init"),
				},
			},
		}

		partition, offset, err := app.kafkaProducer.SendMessage(kafkaMsg)
		if err != nil {
			return fmt.Errorf("failed to send initial message to topic %s: %w", msg.topic, err)
		}
		log.Printf("Sent initial message to topic %s partition %d offset %d", msg.topic, partition, offset)
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
	// Start maintenance manager
	if err := app.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start maintenance manager: %w", err)
	}

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

	fmt.Printf("Server running on http://localhost:8083\\n")
	fmt.Println("Endpoints:")
	fmt.Println("  Health check: http://localhost:8083/health")
	fmt.Println("  Send message: http://localhost:8083/messages/send")
	fmt.Println("  Stats: http://localhost:8083/messages/stats")
	fmt.Println("  Topics info: http://localhost:8083/topics/info")
	fmt.Println()
	fmt.Println("To enable maintenance mode:")
	fmt.Println("  etcdctl --endpoints=localhost:2401 put /maintenance/kafka-service true")
	fmt.Println()
	fmt.Println("To disable maintenance mode:")
	fmt.Println("  etcdctl --endpoints=localhost:2401 put /maintenance/kafka-service false")
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

	// Close Kafka handler
	if app.kafkaHandler != nil {
		app.kafkaHandler.Close()
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
	health := app.manager.HealthCheck()
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

	// Send message to Kafka
	kafkaMsg := &sarama.ProducerMessage{
		Topic: request.Topic,
		Key:   sarama.StringEncoder(request.Key),
		Value: sarama.StringEncoder(request.Message),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("source"),
				Value: []byte("kafka-service-api"),
			},
			{
				Key:   []byte("timestamp"),
				Value: []byte(time.Now().Format(time.RFC3339)),
			},
		},
	}

	partition, offset, err := app.kafkaProducer.SendMessage(kafkaMsg)
	if err != nil {
		log.Printf("Failed to send message to topic %s: %v", request.Topic, err)
		http.Error(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"topic":     request.Topic,
		"partition": partition,
		"offset":    offset,
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
		"handlers":    app.manager.HealthCheck(),
		"kafka":       kafkaStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (app *App) topicsInfoHandler(w http.ResponseWriter, r *http.Request) {
	// Get topic metadata
	metadata, err := app.kafkaHandler.GetTopicMetadata()
	if err != nil {
		log.Printf("Failed to get topic metadata: %v", err)
		http.Error(w, "Failed to get topic information", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"topics":      app.topics,
		"metadata":    metadata,
		"timestamp":   time.Now().UTC(),
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}