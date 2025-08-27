package logger

import "log"

// Logger interface for structured logging across gomaint
type Logger interface {
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
}

// DefaultLogger implements Logger using standard log package
type DefaultLogger struct{}

// NewDefaultLogger creates a new default logger instance
func NewDefaultLogger() Logger {
	return &DefaultLogger{}
}

// Info logs an info message
func (l *DefaultLogger) Info(args ...interface{}) {
	log.Println(append([]interface{}{"INFO:"}, args...)...)
}

// Infof logs a formatted info message
func (l *DefaultLogger) Infof(format string, args ...interface{}) {
	log.Printf("INFO: "+format, args...)
}

// Warn logs a warning message
func (l *DefaultLogger) Warn(args ...interface{}) {
	log.Println(append([]interface{}{"WARN:"}, args...)...)
}

// Warnf logs a formatted warning message
func (l *DefaultLogger) Warnf(format string, args ...interface{}) {
	log.Printf("WARN: "+format, args...)
}

// Error logs an error message
func (l *DefaultLogger) Error(args ...interface{}) {
	log.Println(append([]interface{}{"ERROR:"}, args...)...)
}

// Errorf logs a formatted error message
func (l *DefaultLogger) Errorf(format string, args ...interface{}) {
	log.Printf("ERROR: "+format, args...)
}
