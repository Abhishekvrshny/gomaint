# Logger Package

The logger package provides a structured logging interface for the gomaint library, allowing for consistent logging across all handlers and components.

## Interface

The `Logger` interface defines the contract for logging implementations:

```go
type Logger interface {
    Info(args ...interface{})
    Infof(format string, args ...interface{})
    Warn(args ...interface{})
    Warnf(format string, args ...interface{})
    Error(args ...interface{})
    Errorf(format string, args ...interface{})
}
```

## Default Implementation

The package provides a default implementation using Go's standard `log` package:

```go
logger := logger.NewDefaultLogger()
logger.Info("Service started")
logger.Warnf("Connection timeout after %d seconds", 30)
logger.Error("Failed to connect to database")
```

## Usage in Handlers

Handlers can access the logger through the BaseHandler:

```go
func (h *MyHandler) OnMaintenanceStart(ctx context.Context) error {
    h.Logger().Info("Starting maintenance mode")
    // ... maintenance logic
    return nil
}
```

## Custom Logger Integration

You can provide your own logger implementation by implementing the Logger interface:

```go
type CustomLogger struct {
    // your logger implementation
}

func (l *CustomLogger) Info(args ...interface{}) {
    // custom logging logic
}

// ... implement other methods

// Set custom logger on handler
handler := handlers.NewBaseHandler("my-handler")
handler.SetLogger(&CustomLogger{})
```

## Log Levels

The logger interface supports three log levels:
- **Info**: General information about program execution
- **Warn**: Warning messages for potentially harmful situations  
- **Error**: Error events that might still allow the application to continue

Each level has both variadic (`Info`, `Warn`, `Error`) and formatted (`Infof`, `Warnf`, `Errorf`) variants.