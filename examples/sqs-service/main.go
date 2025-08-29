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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/abhishekvarshney/gomaint"
	sqsHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/sqs"
)

// MessageProcessor implements the SQS message processing logic
type MessageProcessor struct {
	logger *log.Logger
}

// ProcessMessage processes a single SQS message
func (mp *MessageProcessor) ProcessMessage(ctx context.Context, message types.Message) error {
	messageBody := aws.ToString(message.Body)
	messageId := aws.ToString(message.MessageId)

	mp.logger.Printf("Processing message %s: %s", messageId, messageBody)

	// Simulate processing time (1-3 seconds)
	processingTime := time.Duration(1+rand.Intn(3)) * time.Second

	// Check for context cancellation during processing
	timer := time.NewTimer(processingTime)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Processing completed successfully
		mp.logger.Printf("Successfully processed message %s after %v", messageId, processingTime)
		return nil
	case <-ctx.Done():
		mp.logger.Printf("Processing cancelled for message %s: %v", messageId, ctx.Err())
		return ctx.Err()
	}
}

// App holds the application dependencies
type App struct {
	sqsClient  *sqs.Client
	sqsHandler *sqsHandler.Handler
	manager    *gomaint.Manager
	server     *http.Server
	queueURL   string
}

func main() {
	fmt.Println("SQS Service with Maintenance Mode")
	fmt.Println("=================================")

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
	queueURL := getEnv("SQS_QUEUE_URL", "http://localstack:4566/000000000000/test-queue")
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", "etcd:2379")
	awsEndpoint := getEnv("AWS_ENDPOINT", "http://localstack:4566")
	awsRegion := getEnv("AWS_REGION", "us-east-1")

	log.Printf("Configuring SQS client for queue: %s", queueURL)
	log.Printf("AWS endpoint: %s, region: %s", awsEndpoint, awsRegion)

	// Configure AWS SDK for LocalStack
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(awsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			"test", "test", "", // LocalStack accepts any credentials
		)),
		config.WithBaseEndpoint(awsEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create SQS client
	sqsClient := sqs.NewFromConfig(cfg)

	// Create message processor
	messageProcessor := &MessageProcessor{
		logger: log.Default(),
	}

	// Create SQS handler configuration
	sqsConfig := sqsHandler.DefaultConfig(queueURL)
	sqsConfig.DrainTimeout = 45 * time.Second // Allow more time for message draining
	sqsConfig.MaxNumberOfMessages = 5
	sqsConfig.WaitTimeSeconds = 10

	// Create SQS handler
	handler := sqsHandler.NewSQSHandler(sqsClient, sqsConfig, messageProcessor, log.Default())

	// Create maintenance manager using new simplified API
	mgr, err := gomaint.StartWithEtcd(
		context.Background(),
		strings.Split(etcdEndpoints, ","),
		"/maintenance/sqs-service",
		30*time.Second,
		handler, // Register handler during creation
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create maintenance manager: %w", err)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	app := &App{
		sqsClient:  sqsClient,
		sqsHandler: handler,
		manager:    mgr,
		queueURL:   queueURL,
		server: &http.Server{
			Addr:    ":8080",
			Handler: mux,
		},
	}

	// Setup routes
	app.setupRoutes(mux)

	// Initialize queue (create if it doesn't exist)
	if err := app.initializeQueue(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize queue: %w", err)
	}

	// Send initial test messages
	if err := app.sendInitialMessages(context.Background()); err != nil {
		log.Printf("Warning: Failed to send initial messages: %v", err)
		// Don't fail startup for this
	}

	return app, nil
}

func (app *App) initializeQueue(ctx context.Context) error {
	// Extract queue name from URL (LocalStack format)
	queueName := "test-queue"
	if parts := strings.Split(app.queueURL, "/"); len(parts) > 0 {
		queueName = parts[len(parts)-1]
	}

	// Try to create the queue (idempotent operation)
	_, err := app.sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
		Attributes: map[string]string{
			"VisibilityTimeout":      "30",
			"MessageRetentionPeriod": "1209600", // 14 days
		},
	})

	if err != nil {
		// Queue might already exist, which is fine
		log.Printf("Queue creation result: %v (queue might already exist)", err)
	} else {
		log.Printf("Queue %s created successfully", queueName)
	}

	return nil
}

func (app *App) sendInitialMessages(ctx context.Context) error {
	log.Println("Sending initial test messages to queue...")

	for i := 1; i <= 3; i++ {
		message := fmt.Sprintf("Initial test message %d - %s", i, time.Now().Format(time.RFC3339))
		_, err := app.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(app.queueURL),
			MessageBody: aws.String(message),
		})

		if err != nil {
			return fmt.Errorf("failed to send initial message %d: %w", i, err)
		}
		log.Printf("Sent initial message %d", i)
	}

	log.Println("Successfully sent initial test messages")
	return nil
}

func (app *App) setupRoutes(mux *http.ServeMux) {
	// Health check endpoint
	mux.HandleFunc("/health", app.healthHandler)

	// SQS management endpoints
	mux.HandleFunc("/messages/send", app.sendMessageHandler)
	mux.HandleFunc("/messages/stats", app.statsHandler)

	// Queue management endpoints
	mux.HandleFunc("/queue/info", app.queueInfoHandler)
}

func (app *App) run(ctx context.Context) error {
	// Maintenance manager is already started by StartWithEtcd

	// Start SQS message processing
	if err := app.sqsHandler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start SQS handler: %w", err)
	}

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on %s", app.server.Addr)
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server failed: %v", err)
		}
	}()

	fmt.Printf("Server running on http://localhost:8082\n")
	fmt.Println("Endpoints:")
	fmt.Println("  Health check: http://localhost:8082/health")
	fmt.Println("  Send message: http://localhost:8082/messages/send")
	fmt.Println("  Stats: http://localhost:8082/messages/stats")
	fmt.Println("  Queue info: http://localhost:8082/queue/info")
	fmt.Println()
	fmt.Println("To enable maintenance mode:")
	fmt.Println("  etcdctl --endpoints=localhost:2399 put /maintenance/sqs-service true")
	fmt.Println()
	fmt.Println("To disable maintenance mode:")
	fmt.Println("  etcdctl --endpoints=localhost:2399 put /maintenance/sqs-service false")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop SQS handler
	app.sqsHandler.Stop()

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
		"queue_url":   app.queueURL,
		"timestamp":   time.Now().UTC(),
	})
}

func (app *App) sendMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Message string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if request.Message == "" {
		request.Message = fmt.Sprintf("Test message sent at %s", time.Now().Format(time.RFC3339))
	}

	// Send message to SQS
	result, err := app.sqsClient.SendMessage(r.Context(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(app.queueURL),
		MessageBody: aws.String(request.Message),
	})

	if err != nil {
		log.Printf("Failed to send message: %v", err)
		http.Error(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message_id": aws.ToString(result.MessageId),
		"message":    request.Message,
		"queue_url":  app.queueURL,
		"timestamp":  time.Now().UTC(),
	})
}

func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	// Get SQS handler stats
	var sqsStats map[string]interface{}
	if handler, exists := app.manager.GetHandler("sqs"); exists {
		if sqsHandler, ok := handler.(*sqsHandler.Handler); ok {
			sqsStats = sqsHandler.GetStats()
		}
	}

	stats := map[string]interface{}{
		"maintenance": app.manager.IsInMaintenance(),
		"timestamp":   time.Now().UTC(),
		"handlers":    app.manager.GetHandlerHealth(),
		"sqs":         sqsStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (app *App) queueInfoHandler(w http.ResponseWriter, r *http.Request) {
	// Get queue attributes
	result, err := app.sqsClient.GetQueueAttributes(r.Context(), &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(app.queueURL),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
			types.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
			types.QueueAttributeNameVisibilityTimeout,
			types.QueueAttributeNameCreatedTimestamp,
		},
	})

	if err != nil {
		log.Printf("Failed to get queue attributes: %v", err)
		http.Error(w, "Failed to get queue information", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"queue_url":  app.queueURL,
		"attributes": result.Attributes,
		"timestamp":  time.Now().UTC(),
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
