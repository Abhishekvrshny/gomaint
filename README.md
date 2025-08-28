# GoMaint - Go Maintenance Library

A simple and efficient Go library for gracefully handling service maintenance mode based on etcd key changes. GoMaint allows any Go service to seamlessly transition into and out of maintenance mode while properly draining connections and managing resources.

## Features

- **Simple API**: One function to get started - `StartWithEtcd()`
- **etcd Integration**: Built-in etcd event source for maintenance state changes
- **Multiple Handler Support**: Built-in handlers for HTTP, gRPC, Kafka, SQS, and databases (GORM, XORM, etc.)
- **Graceful Draining**: Configurable timeout for draining active connections/requests
- **Thread-Safe**: Concurrent-safe operations with proper synchronization
- **Health Monitoring**: Built-in health checks and statistics for handlers
- **Zero Dependencies**: Works with your existing server infrastructure

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

    // Create HTTP handler for maintenance
    handler := httpHandler.NewHTTPHandler(server, 30*time.Second)
    
    // Skip health check endpoints from maintenance mode
    handler.SkipPaths("/health")

    // Start maintenance manager with etcd - that's it!
    ctx := context.Background()
    mgr, err := gomaint.StartWithEtcd(
        ctx,
        []string{"localhost:2379"},  // etcd endpoints
        "/maintenance/my-service",   // etcd key to watch
        30*time.Second,              // drain timeout
        handler,                     // handlers to manage
    )
    if err != nil {
        panic(err)
    }
    defer mgr.Stop()

    // Start your server
    server.ListenAndServe()
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
handler := databaseHandler.NewDatabaseHandler("gorm", db, 30*time.Second, log.Default())

// Using XORM  
import "xorm.io/xorm"
engine, _ := xorm.NewEngine("sqlite3", "test.db")
wrapper := &XormWrapper{engine: engine} // Implement DB interface
handler := databaseHandler.NewDatabaseHandler("xorm", wrapper, 30*time.Second, log.Default())

// Any database that implements the DB interface
handler := databaseHandler.NewDatabaseHandler("custom", customDB, 30*time.Second, log.Default())
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

### Environment Variables

Configure your service with environment variables:

```bash
# HTTP service example
HTTP_PORT=8080 \
ETCD_ENDPOINTS=localhost:2379 \
ETCD_KEY=/maintenance/my-service \
DRAIN_TIMEOUT=30s \
./my-service
```

## Health Monitoring

```go
// Check if in maintenance mode
inMaintenance := mgr.IsInMaintenance()

// Get health status of all handlers
health := mgr.GetHandlerHealth()
fmt.Printf("HTTP Handler Health: %v", health["http"])

// Get specific handler for detailed stats
if handler, exists := mgr.GetHandler("http"); exists {
    if httpHandler, ok := handler.(*httpHandler.Handler); ok {
        stats := httpHandler.GetStats()
        fmt.Printf("Handler stats: %+v", stats)
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

func (h *CustomHandler) IsHealthy() bool {
    // Return your health check logic
    return h.GetState() != handlers.StateError
}
```

## Examples

See the `examples/` directory for complete working examples:

- `examples/http-service/` - HTTP service with maintenance mode
- `examples/grpc-service/` - gRPC service with maintenance mode  
- `examples/gorm-service/` - Database service with GORM
- `examples/xorm-service/` - Database service with XORM  
- `examples/kafka-service/` - Kafka message processing with consumer groups
- `examples/sqs-service/` - SQS message processing with polling

Each example includes Docker Compose files for easy testing with etcd.

## Maintenance Values

The library recognizes the following values as "maintenance mode enabled":
- `true`
- `1`
- `on`
- `enabled`
- `maintenance`

Any other value (including empty or missing key) is considered "maintenance mode disabled".

## Architecture

GoMaint uses a simple and clean architecture:

```
┌─────────────────┐    ┌──────────────┐    ┌─────────────────┐
│   Your Service  │◄───┤   Manager    │◄───┤ Etcd EventSource│
│   (HTTP/gRPC/   │    │              │    └─────────────────┘
│    Database)    │    │  - Etcd      │           │
└─────────────────┘    │    Source    │           │
                       │  - Multiple  │           │ 
┌─────────────────┐    │    Handlers  │           │
│   More Services │◄───┤              │           │
└─────────────────┘    └──────────────┘      ┌────▼──────┐
                                              │   etcd    │
                                              │  cluster  │
                                              └───────────┘
```

1. **etcd change** → Etcd watches key for changes
2. **Manager** → Receives event and updates internal state  
3. **Handlers** → Manager calls OnMaintenanceStart/End on all handlers
4. **Services** → Handlers manage your HTTP servers, databases, queues, etc.

## Error Handling

The library provides clear error information:

```go
if err := gomaint.StartWithEtcd(ctx, endpoints, keyPath, timeout, handler); err != nil {
    log.Fatalf("Failed to start maintenance manager: %v", err)
}
```

## Thread Safety

All operations are thread-safe and can be called concurrently from multiple goroutines.

## Development

### Makefile

The project includes a comprehensive Makefile for development and CI/CD tasks:

```bash
# Install development tools
make install-tools

# Code quality checks
make goimports-check      # Check import formatting
make gofmt-check         # Check Go code formatting 
make lint-check          # Run staticcheck linter

# Fix formatting issues
make goimports-fix       # Fix import formatting
make gofmt-fix          # Fix Go code formatting
make format             # Fix all formatting issues

# Testing and building
make test               # Run tests
make test-race          # Run tests with race detection
make coverage           # Generate coverage report
make build              # Build the project
make build-examples     # Build all example services

# All-in-one commands
make lint               # Run all linting checks
make pre-commit         # Run lint + test (useful before commits)
make ci                 # Full CI pipeline (deps, lint, test, race, build, examples)

# Get help
make help              # Show all available targets
```

### Pre-commit Hook

For consistent code quality, run before committing:

```bash
make pre-commit
```

This runs linting and tests to ensure code quality.

## Requirements

- Go 1.23 or later
- etcd v3.6+ for the etcd server

## Dependencies

- `go.etcd.io/etcd/client/v3` - etcd client
- `github.com/IBM/sarama` - Kafka client (optional, only if using Kafka handler)
- `github.com/aws/aws-sdk-go-v2/service/sqs` - AWS SQS client (optional, only if using SQS handler)
- `gorm.io/gorm` - GORM ORM (optional, only if using GORM)
- `xorm.io/xorm` - XORM ORM (optional, only if using XORM)

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for your changes
4. Ensure all tests pass
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For questions, issues, or contributions, please open an issue on GitHub.