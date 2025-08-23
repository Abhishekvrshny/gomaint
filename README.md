# GoMaint - Go Maintenance Library

A comprehensive Go library for gracefully handling service maintenance mode based on etcd key changes. GoMaint allows any Go service to seamlessly transition into and out of maintenance mode while properly draining connections and managing resources.

## Features

- **Event-Driven Architecture**: Flexible event system supporting multiple state change sources
- **etcd Integration**: Built-in ETCD event source for maintenance state changes
- **Extensible Event Sources**: Easy to add new event sources (file system, webhooks, databases, etc.)
- **Multiple Handler Support**: Built-in handlers for HTTP, gRPC, Kafka, SQS, and generic database support (GORM, XORM, etc.)
- **Event Filtering**: Advanced event filtering and routing capabilities
- **Graceful Draining**: Configurable timeout for draining active connections/requests
- **Thread-Safe**: Concurrent-safe operations with proper synchronization
- **Pluggable Architecture**: Easy to extend with custom handlers and event sources
- **Health Monitoring**: Built-in health checks and statistics for handlers and event sources

## Installation

```bash
go get github.com/abhishekvarshney/gomaint
```

## Quick Start

```go
package main

import (
    "context"
    "net/http"
    "time"

    "github.com/abhishekvarshney/gomaint"
    httpHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/http"
)

func main() {
    // Create HTTP server
    server := &http.Server{
        Addr:    ":8080",
        Handler: http.DefaultServeMux,
    }

    // Create maintenance configuration
    config := gomaint.NewConfig(
        []string{"localhost:2379"},  // etcd endpoints
        "/maintenance/my-service",   // etcd key to watch
        30*time.Second,              // drain timeout
    )

    // Create HTTP handler for maintenance
    httpMaintHandler := httpHandler.NewHTTPHandler(server, 30*time.Second)

    // Start maintenance manager
    ctx := context.Background()
    mgr, err := gomaint.Start(ctx, config, httpMaintHandler)
    if err != nil {
        panic(err)
    }
    defer mgr.Stop()

    // Start your server
    server.ListenAndServe()
}
```

## Configuration

### Basic Configuration

```go
config := &gomaint.Config{
    EtcdEndpoints: []string{"localhost:2379"},
    KeyPath:       "/maintenance/my-service",
    DrainTimeout:  30 * time.Second,
    EtcdTimeout:   5 * time.Second,
}
```

### TLS Configuration

```go
config := &gomaint.Config{
    EtcdEndpoints: []string{"localhost:2379"},
    EtcdTLS:       true,
    EtcdCertFile:  "/path/to/cert.pem",
    EtcdKeyFile:   "/path/to/key.pem",
    EtcdCAFile:    "/path/to/ca.pem",
    KeyPath:       "/maintenance/my-service",
    DrainTimeout:  30 * time.Second,
}
```

## Handlers

### HTTP Handler

Gracefully drains HTTP connections and returns 503 Service Unavailable during maintenance.

```go
import httpHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/http"

server := &http.Server{Addr: ":8080", Handler: mux}
handler := httpHandler.NewHTTPHandler(server, 30*time.Second)

// Skip health check endpoints from maintenance mode
handler.SkipPaths("/health", "/metrics")
```

### gRPC Handler

Gracefully drains gRPC connections and returns maintenance errors during maintenance mode while preserving health checks.

```go
import (
    grpcHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/grpc"
    "net"
)

lis, _ := net.Listen("tcp", ":50051")
handler := grpcHandler.NewGRPCHandler(lis, 30*time.Second)

// Register your gRPC services
pb.RegisterUserServiceServer(handler.GetServer(), userService)

// Start the server
handler.Start(ctx)
```

### Database Handler

Manages database connections during maintenance mode. Works with any ORM that provides access to `*sql.DB`.

```go
import (
    databaseHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/database"
    "gorm.io/gorm"
    "gorm.io/driver/sqlite"
)

// Using GORM
db, _ := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
handler := databaseHandler.NewGORMHandler(db, log.Default())

// Using XORM  
import "xorm.io/xorm"
engine, _ := xorm.NewEngine("sqlite3", "test.db")
handler := databaseHandler.NewXORMHandler(engine, log.Default())

// Generic database handler
handler := databaseHandler.NewDatabaseHandler("custom", customDB, log.Default())
```

### Kafka Handler

Gracefully handles Kafka consumer groups during maintenance mode.

```go
import kafkaHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/kafka"

brokers := []string{"localhost:9092"}
topics := []string{"my-topic"}
consumerGroup := "my-service-group"

config := kafkaHandler.DefaultConfig(brokers, topics, consumerGroup)
processor := &MyMessageProcessor{} // Implement MessageProcessor interface

handler, err := kafkaHandler.NewKafkaHandler(brokers, config, processor, log.Default())
if err != nil {
    log.Fatal(err)
}
```

### SQS Handler

Stops SQS polling during maintenance and waits for in-flight messages to complete.

```go
import (
    sqsHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/sqs"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
)

client := sqs.NewFromConfig(awsConfig)
config := sqsHandler.DefaultConfig("https://sqs.region.amazonaws.com/account/queue")
processor := &MyMessageProcessor{} // Implement MessageProcessor interface

handler := sqsHandler.NewSQSHandler(client, config, processor, log.Default())
```

## Maintenance Control

### Using etcdctl

```bash
# Enable maintenance mode
etcdctl put /maintenance/my-service true

# Disable maintenance mode
etcdctl put /maintenance/my-service false

# Delete key (disables maintenance)
etcdctl del /maintenance/my-service
```

### Programmatic Control

```go
// Set maintenance mode
err := mgr.SetMaintenanceMode(ctx, true)

// Get current maintenance mode
inMaintenance, err := mgr.GetMaintenanceMode(ctx)

// Wait for maintenance mode change
err := mgr.WaitForMaintenanceMode(ctx, true, 30*time.Second)
```

## Health Monitoring

```go
// Check if in maintenance mode
inMaintenance := mgr.IsInMaintenance()

// Get health status of all handlers
health := mgr.HealthCheck()
fmt.Printf("HTTP Handler Health: %v", health["http"])

// Get handler statistics
if httpHandler, ok := mgr.GetHandler("http"); ok {
    if h, ok := httpHandler.(*httpHandler.HTTPHandler); ok {
        activeRequests := h.GetActiveRequestCount()
        fmt.Printf("Active requests: %d", activeRequests)
    }
}
```

## Custom Handlers

Implement the `Handler` interface to create custom handlers:

```go
type CustomHandler struct {
    *handlers.BaseHandler
    // Your custom fields
}

func NewCustomHandler() *CustomHandler {
    return &CustomHandler{
        BaseHandler: handlers.NewBaseHandler("custom"),
    }
}

func (h *CustomHandler) OnMaintenanceStart(ctx context.Context) error {
    h.SetState(handlers.StateMaintenance)
    // Your maintenance start logic
    return nil
}

func (h *CustomHandler) OnMaintenanceEnd(ctx context.Context) error {
    h.SetState(handlers.StateNormal)
    // Your maintenance end logic
    return nil
}
```

## Event System

The new event-driven architecture allows you to listen to state change events from multiple sources and provides advanced event handling capabilities.

### Event Sources

Currently supported event sources:
- **ETCD**: Watches ETCD keys for changes (built-in)
- **File System**: Watch file changes (coming soon)
- **HTTP Webhooks**: Receive HTTP webhook events (coming soon)
- **Database**: Monitor database triggers (coming soon)

### Advanced Event Usage

```go
// Get the event manager for advanced usage
eventManager := mgr.GetEventManager()

// Subscribe to maintenance change events with custom filtering
subscriptionID, err := eventManager.SubscribeWithFilter(
    events.EventTypeMaintenanceChange,
    func(event events.Event) bool {
        // Only process events from specific sources
        return event.Source == "etcd"
    },
    func(event events.Event) error {
        log.Printf("Event: %s from %s, Value: %v", 
            event.Type, event.Source, event.Value)
        return nil
    },
)

// Check event source health
health := mgr.GetEventSourceHealth()
for source, healthy := range health {
    log.Printf("Source %s: healthy=%v", source, healthy)
}

// List available event sources
sources := mgr.ListEventSources()
log.Printf("Available sources: %v", sources)
```

### Event Types

- `EventTypeMaintenanceChange`: Maintenance mode state changes
- `EventTypeConfigChange`: Configuration changes (future)
- `EventTypeHealthChange`: Health status changes (future)

### Custom Event Sources

You can implement custom event sources by implementing the `EventSource` interface:

```go
type CustomEventSource struct {
    // Your implementation
}

func (c *CustomEventSource) Start(ctx context.Context) error { /* ... */ }
func (c *CustomEventSource) Stop() error { /* ... */ }
func (c *CustomEventSource) Subscribe(eventType events.EventType, callback events.EventCallback) error { /* ... */ }
func (c *CustomEventSource) Unsubscribe(subscriptionID string) error { /* ... */ }
func (c *CustomEventSource) Name() string { return "custom" }
func (c *CustomEventSource) IsHealthy() bool { /* ... */ }
func (c *CustomEventSource) GetLastEvent() *events.Event { /* ... */ }

// Add to manager
mgr.AddEventSource(customSource)
```

## Examples

See the `examples/` directory for complete working examples:

- `examples/http-service/` - HTTP service with maintenance mode
- `examples/gorm-service/` - Database service with GORM
- `examples/xorm-service/` - Database service with XORM  
- `examples/kafka-service/` - Kafka message processing with consumer groups
- `examples/sqs-service/` - SQS message processing with polling

## Maintenance Values

The library recognizes the following values as "maintenance mode enabled":
- `true`
- `1`
- `on`
- `enabled`
- `maintenance`

Any other value (including empty) is considered "maintenance mode disabled".

## Error Handling

The library provides detailed error information:

```go
if err := mgr.Start(ctx); err != nil {
    if configErr, ok := err.(*config.ErrInvalidConfig); ok {
        fmt.Printf("Configuration error in field '%s': %s", configErr.Field, configErr.Reason)
    }
    // Handle other errors
}
```

## Thread Safety

All operations are thread-safe and can be called concurrently from multiple goroutines.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for your changes
4. Ensure all tests pass
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Requirements

- Go 1.23 or later
- etcd v3.6+ for the etcd server

## Dependencies

- `go.etcd.io/etcd/client/v3` - etcd client
- `github.com/IBM/sarama` - Kafka client (optional, only if using Kafka handler)
- `github.com/aws/aws-sdk-go-v2/service/sqs` - AWS SQS client (optional, only if using SQS handler)
- `gorm.io/gorm` - GORM ORM (optional, only if using GORM)
- `xorm.io/xorm` - XORM ORM (optional, only if using XORM)

## Support

For questions, issues, or contributions, please open an issue on GitHub.
