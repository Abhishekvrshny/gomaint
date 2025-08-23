package eventsource

import (
	"testing"
	"time"
)

func TestNewEtcdConfig(t *testing.T) {
	config := NewEtcdConfig("test", []string{"localhost:2379"}, "/test/key")
	
	if config.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", config.Name)
	}
	
	if config.Type != "etcd" {
		t.Errorf("Expected type 'etcd', got '%s'", config.Type)
	}
	
	if len(config.Endpoints) != 1 || config.Endpoints[0] != "localhost:2379" {
		t.Errorf("Expected endpoints [localhost:2379], got %v", config.Endpoints)
	}
	
	if config.KeyPath != "/test/key" {
		t.Errorf("Expected key path '/test/key', got '%s'", config.KeyPath)
	}
	
	if config.Timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", config.Timeout)
	}
}

func TestNewEtcdEventSource(t *testing.T) {
	config := NewEtcdConfig("test", []string{"localhost:2379"}, "/test/key")
	
	source, err := NewEtcdEventSource(config)
	if err != nil {
		t.Fatalf("Failed to create etcd event source: %v", err)
	}
	
	if source.Name() != "test" {
		t.Errorf("Expected name 'test', got '%s'", source.Name())
	}
	
	if source.Type() != "etcd" {
		t.Errorf("Expected type 'etcd', got '%s'", source.Type())
	}
	
	if source.IsHealthy() {
		t.Error("Expected new source to not be healthy until started")
	}
}

func TestNewEtcdEventSourceValidation(t *testing.T) {
	// Test nil config
	_, err := NewEtcdEventSource(nil)
	if err == nil {
		t.Error("Expected error for nil config")
	}
	
	// Test empty endpoints
	config := &EtcdConfig{
		Config: Config{Name: "test", Type: "etcd"},
		KeyPath: "/test",
	}
	_, err = NewEtcdEventSource(config)
	if err == nil {
		t.Error("Expected error for empty endpoints")
	}
	
	// Test empty key path
	config = &EtcdConfig{
		Config: Config{Name: "test", Type: "etcd"},
		Endpoints: []string{"localhost:2379"},
	}
	_, err = NewEtcdEventSource(config)
	if err == nil {
		t.Error("Expected error for empty key path")
	}
}

func TestParseMaintenanceValue(t *testing.T) {
	source := &EtcdEventSource{}
	
	trueValues := []string{"true", "1", "yes", "on", "enabled", "maintenance", "TRUE", "True"}
	for _, value := range trueValues {
		if !source.parseMaintenanceValue(value) {
			t.Errorf("Expected '%s' to be parsed as true", value)
		}
	}
	
	falseValues := []string{"false", "0", "no", "off", "disabled", "", "random"}
	for _, value := range falseValues {
		if source.parseMaintenanceValue(value) {
			t.Errorf("Expected '%s' to be parsed as false", value)
		}
	}
}