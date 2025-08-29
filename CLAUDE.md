# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GoMaint is a Go maintenance library that enables services to transition into and out of maintenance mode while gracefully draining connections. It uses etcd for distributed maintenance state coordination and supports multiple service types (HTTP, gRPC, Kafka, SQS, database ORMs).

## Development Commands

### Build and Quality
```bash
make install-tools    # Install development dependencies (goimports, staticcheck)
make lint            # Run all linting (gofmt, goimports, staticcheck) 
make format          # Auto-fix formatting issues
make pre-commit      # Run lint + test (recommended before commits)
make ci              # Full CI pipeline: deps, lint, test, race, build, examples
```

### Testing
```bash
make test            # Standard test suite
make test-race       # Test with race condition detection  
make coverage        # Generate HTML coverage report
```

### Building
```bash
make build           # Build main library
make build-examples  # Build all example services
```

## Architecture

### Core Design Patterns

**Event-Driven with Adapter Pattern**: The library uses an EventSource interface (implemented via etcd) that triggers maintenance state changes across multiple handlers simultaneously.

**Manager Coordination**: The `Manager` (`pkg/maintenance/manager.go`) coordinates multiple handlers with thread-safe state transitions and configurable drain timeouts.

**Generic Handler Strategy**: Each service type (HTTP, gRPC, Kafka, etc.) implements specific maintenance behaviors via the Handler interface (`pkg/handlers/handler.go`).

### Key Packages

- **`pkg/maintenance/`** - Central coordination and state management
- **`pkg/eventsource/`** - Event source abstractions and etcd implementation  
- **`pkg/handlers/`** - Base handler functionality and service-specific implementations
- **`pkg/logger/`** - Structured logging abstraction

### Service Handler Types

- **HTTP** (`pkg/handlers/http/`) - Request rejection with 503 responses, path exclusions
- **gRPC** (`pkg/handlers/grpc/`) - Connection draining while preserving health checks
- **Kafka** (`pkg/handlers/kafka/`) - Generic consumer management with Sarama/Confluent adapters
- **Database** (`pkg/handlers/database/`) - Connection pool management (works with GORM, XORM, any ORM)
- **SQS** (`pkg/handlers/sqs/`) - AWS SQS polling cessation with LocalStack support

### Interface Design

The codebase heavily uses interface abstractions:
- **Consumer Interface** - Library-agnostic Kafka abstraction
- **MessageProcessor Interface** - Business logic separation from transport  
- **Logger Interface** - Pluggable logging

## Examples Structure

Each example in `/examples/` demonstrates real-world usage patterns with:
- Environment-based configuration with defaults
- Docker Compose setup with dedicated etcd instances
- Health check endpoints excluded from maintenance mode
- Consistent maintenance triggers via etcd keys (service_name:maintenance)

Example services run on different ports:
- gin-service (8084), http-service (8080), gorm-service (8080), xorm-service (8081)
- kafka-sarama-service (8083), kafka-confluent-service (8084), sqs-service (8082)

## Development Notes

- Go 1.23+ required (specified in go.mod toolchain)
- Uses staticcheck instead of deprecated golint
- Extensive concurrent safety with sync.RWMutex and atomic operations
- Context-based cancellation throughout for graceful shutdown
- Database handlers work with any ORM providing `*sql.DB` access