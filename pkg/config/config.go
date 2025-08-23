package config

import (
	"time"
)

// Config holds the configuration for the maintenance manager
type Config struct {
	// etcd configuration
	EtcdEndpoints []string `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	EtcdUsername  string   `json:"etcd_username" yaml:"etcd_username"`
	EtcdPassword  string   `json:"etcd_password" yaml:"etcd_password"`
	EtcdTLS       bool     `json:"etcd_tls" yaml:"etcd_tls"`
	EtcdCertFile  string   `json:"etcd_cert_file" yaml:"etcd_cert_file"`
	EtcdKeyFile   string   `json:"etcd_key_file" yaml:"etcd_key_file"`
	EtcdCAFile    string   `json:"etcd_ca_file" yaml:"etcd_ca_file"`

	// Key to watch for maintenance state
	KeyPath string `json:"key_path" yaml:"key_path"`

	// Timeout for draining connections during maintenance
	DrainTimeout time.Duration `json:"drain_timeout" yaml:"drain_timeout"`

	// etcd connection timeout
	EtcdTimeout time.Duration `json:"etcd_timeout" yaml:"etcd_timeout"`

	// Watch timeout for etcd operations
	WatchTimeout time.Duration `json:"watch_timeout" yaml:"watch_timeout"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		EtcdEndpoints: []string{"localhost:2379"},
		KeyPath:       "/maintenance/service",
		DrainTimeout:  30 * time.Second,
		EtcdTimeout:   5 * time.Second,
		WatchTimeout:  10 * time.Second,
		EtcdTLS:       false,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if len(c.EtcdEndpoints) == 0 {
		return ErrInvalidConfig{Field: "etcd_endpoints", Reason: "at least one endpoint required"}
	}
	if c.KeyPath == "" {
		return ErrInvalidConfig{Field: "key_path", Reason: "key path cannot be empty"}
	}
	if c.DrainTimeout <= 0 {
		return ErrInvalidConfig{Field: "drain_timeout", Reason: "drain timeout must be positive"}
	}
	if c.EtcdTimeout <= 0 {
		return ErrInvalidConfig{Field: "etcd_timeout", Reason: "etcd timeout must be positive"}
	}
	return nil
}

// ErrInvalidConfig represents a configuration validation error
type ErrInvalidConfig struct {
	Field  string
	Reason string
}

func (e ErrInvalidConfig) Error() string {
	return "invalid config field '" + e.Field + "': " + e.Reason
}
