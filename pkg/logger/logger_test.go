package logger

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func TestDefaultLogger(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(originalOutput)
	}()

	logger := NewDefaultLogger()

	// Test Info
	logger.Info("test info message")
	output := buf.String()
	if !strings.Contains(output, "INFO:") || !strings.Contains(output, "test info message") {
		t.Errorf("Expected INFO log, got: %s", output)
	}

	// Reset buffer
	buf.Reset()

	// Test Infof
	logger.Infof("test %s message", "formatted")
	output = buf.String()
	if !strings.Contains(output, "INFO:") || !strings.Contains(output, "test formatted message") {
		t.Errorf("Expected formatted INFO log, got: %s", output)
	}

	// Reset buffer
	buf.Reset()

	// Test Warn
	logger.Warn("test warning")
	output = buf.String()
	if !strings.Contains(output, "WARN:") || !strings.Contains(output, "test warning") {
		t.Errorf("Expected WARN log, got: %s", output)
	}

	// Reset buffer
	buf.Reset()

	// Test Warnf
	logger.Warnf("warning with %d number", 42)
	output = buf.String()
	if !strings.Contains(output, "WARN:") || !strings.Contains(output, "warning with 42 number") {
		t.Errorf("Expected formatted WARN log, got: %s", output)
	}

	// Reset buffer
	buf.Reset()

	// Test Error
	logger.Error("test error")
	output = buf.String()
	if !strings.Contains(output, "ERROR:") || !strings.Contains(output, "test error") {
		t.Errorf("Expected ERROR log, got: %s", output)
	}

	// Reset buffer
	buf.Reset()

	// Test Errorf
	logger.Errorf("error code: %d", 500)
	output = buf.String()
	if !strings.Contains(output, "ERROR:") || !strings.Contains(output, "error code: 500") {
		t.Errorf("Expected formatted ERROR log, got: %s", output)
	}
}

func TestLoggerInterface(t *testing.T) {
	// Redirect log output to discard during interface test
	log.SetOutput(os.Stderr)

	l := NewDefaultLogger()

	// Test that it implements the interface correctly
	if l == nil {
		t.Error("Logger should not be nil")
	}

	// Test all methods exist (will cause compilation error if not)
	l.Info("test")
	l.Infof("test %s", "format")
	l.Warn("test")
	l.Warnf("test %s", "format")
	l.Error("test")
	l.Errorf("test %s", "format")
}
