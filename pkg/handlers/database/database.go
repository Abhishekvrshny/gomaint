package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/abhishekvarshney/gomaint/pkg/handlers"
)

// DB interface for database operations (allows for easier testing)
// Compatible with GORM, XORM, and other ORM libraries that expose sql.DB
type DB interface {
	DB() (*sql.DB, error)
}

// ConnectionSettings holds the original database connection settings
type ConnectionSettings struct {
	MaxIdleCons     int
	MaxOpenCons     int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Handler implements the Handler interface for database operations
// Works with any ORM that provides access to the underlying sql.DB
type Handler struct {
	*handlers.BaseHandler
	db               DB
	logger           *log.Logger
	originalSettings *ConnectionSettings
	settingsMux      sync.RWMutex
	drainTimeout     time.Duration
	activeConnections int32 // atomic counter for active connections
}

// NewDatabaseHandler creates a new database handler
// Compatible with GORM, XORM, and other ORM libraries
func NewDatabaseHandler(name string, db DB, drainTimeout time.Duration, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.Default()
	}
	return &Handler{
		BaseHandler:  handlers.NewBaseHandler(name),
		db:           db,
		logger:       logger,
		drainTimeout: drainTimeout,
	}
}

// SetOriginalSettings allows manual configuration of the original connection settings
// This is useful when you want to override the defaults that would be used for restoration
func (h *Handler) SetOriginalSettings(settings *ConnectionSettings) {
	h.settingsMux.Lock()
	defer h.settingsMux.Unlock()
	h.originalSettings = settings
	h.logger.Printf("Database Handler (%s): Manual original settings configured - MaxIdle: %d, MaxOpen: %d",
		h.Name(), settings.MaxIdleCons, settings.MaxOpenCons)
}

// cacheCurrentSettings stores the current database connection settings
func (h *Handler) cacheCurrentSettings(sqlDB *sql.DB) error {
	h.settingsMux.Lock()
	defer h.settingsMux.Unlock()

	// Only cache if we haven't already cached the original settings
	if h.originalSettings != nil {
		return nil
	}

	// Get current stats to determine current settings
	stats := sqlDB.Stats()

	// Note: Go's sql.DB doesn't expose current MaxIdleCons, ConnMaxLifetime, and ConnMaxIdleTime
	// We can only get MaxOpenConnections from stats. For the others, we'll use reasonable defaults
	// that are commonly used in production applications
	h.originalSettings = &ConnectionSettings{
		MaxIdleCons:     2, // Common default for MaxIdleCons
		MaxOpenCons:     stats.MaxOpenConnections,
		ConnMaxLifetime: time.Hour,        // Common default
		ConnMaxIdleTime: 30 * time.Minute, // Common default
	}

	// If MaxOpenConnections is 0 (unlimited), use a reasonable default
	if h.originalSettings.MaxOpenCons == 0 {
		h.originalSettings.MaxOpenCons = 100 // Reasonable default for unlimited
	}

	h.logger.Printf("Database Handler (%s): Cached original settings - MaxOpen: %d (MaxIdle and timeouts use defaults)",
		h.Name(), h.originalSettings.MaxOpenCons)

	return nil
}

// OnMaintenanceStart handles maintenance mode activation
func (h *Handler) OnMaintenanceStart(ctx context.Context) error {
	h.logger.Printf("Database Handler (%s): Maintenance mode enabled - Preparing database for maintenance", h.Name())
	h.SetState(handlers.StateMaintenance)

	// Get database connection
	sqlDB, err := h.db.DB()
	if err != nil {
		h.SetState(handlers.StateError)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Cache current settings before modifying them
	if err := h.cacheCurrentSettings(sqlDB); err != nil {
		h.SetState(handlers.StateError)
		return fmt.Errorf("failed to cache current settings: %w", err)
	}

	// Set minimum possible connection pool settings during maintenance
	// These are the absolute minimum values to reduce database load
	sqlDB.SetMaxIdleConns(0)                       // No idle connections
	sqlDB.SetMaxOpenConns(1)                       // Only 1 connection maximum
	sqlDB.SetConnMaxLifetime(h.drainTimeout / 10)  // Short lifetime relative to drain timeout
	sqlDB.SetConnMaxIdleTime(h.drainTimeout / 10)  // Short idle time relative to drain timeout

	// Wait for active connections to drain or timeout
	h.logger.Printf("Database Handler (%s): Waiting for active connections to drain (timeout: %v)", h.Name(), h.drainTimeout)
	
	drainStart := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.drainTimeout):
			stats := sqlDB.Stats()
			h.logger.Printf("Database Handler (%s): Drain timeout reached with %d open connections", h.Name(), stats.OpenConnections)
			return fmt.Errorf("timeout waiting for connections to drain after %v, %d connections still open", h.drainTimeout, stats.OpenConnections)
		case <-ticker.C:
			stats := sqlDB.Stats()
			if stats.OpenConnections <= 1 { // Allow 1 connection for basic operations
				h.logger.Printf("Database Handler (%s): Database prepared for maintenance mode with minimal connections (drained in %v)", h.Name(), time.Since(drainStart))
				return nil
			}
			h.logger.Printf("Database Handler (%s): Still waiting for %d connections to drain", h.Name(), stats.OpenConnections)
		}
	}
}

// OnMaintenanceEnd handles maintenance mode deactivation
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	h.logger.Printf("Database Handler (%s): Maintenance mode disabled - Restoring original database operations", h.Name())

	// Get database connection
	sqlDB, err := h.db.DB()
	if err != nil {
		h.SetState(handlers.StateError)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Restore original connection pool settings
	h.settingsMux.RLock()
	originalSettings := h.originalSettings
	h.settingsMux.RUnlock()

	h.logger.Printf("Database Handler (%s): Restoring original settings - MaxIdle: %d, MaxOpen: %d",
		h.Name(), originalSettings.MaxIdleCons, originalSettings.MaxOpenCons)

	sqlDB.SetMaxIdleConns(originalSettings.MaxIdleCons)
	sqlDB.SetMaxOpenConns(originalSettings.MaxOpenCons)
	sqlDB.SetConnMaxLifetime(originalSettings.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(originalSettings.ConnMaxIdleTime)

	h.SetState(handlers.StateNormal)
	h.logger.Printf("Database Handler (%s): Original database operations restored", h.Name())
	return nil
}

// IsHealthy performs database health check
func (h *Handler) IsHealthy() bool {
	// First check the base handler state
	if !h.BaseHandler.IsHealthy() {
		return false
	}

	// Perform database ping
	sqlDB, err := h.db.DB()
	if err != nil {
		h.logger.Printf("Database Handler (%s): Failed to get underlying sql.DB: %v", h.Name(), err)
		return false
	}

	// Ping database with timeout (use 1/10 of drain timeout, min 1 second, max 10 seconds)
	healthTimeout := h.drainTimeout / 10
	if healthTimeout < time.Second {
		healthTimeout = time.Second
	} else if healthTimeout > 10*time.Second {
		healthTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		h.logger.Printf("Database Handler (%s): Database health check failed: %v", h.Name(), err)
		return false
	}

	return true
}

// GetStats returns database connection statistics
func (h *Handler) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["handler_name"] = h.Name()
	stats["handler_state"] = h.State().String()
	stats["drain_timeout"] = h.drainTimeout.String()

	sqlDB, err := h.db.DB()
	if err != nil {
		stats["error"] = err.Error()
		return stats
	}

	dbStats := sqlDB.Stats()
	stats["max_open_connections"] = dbStats.MaxOpenConnections
	stats["open_connections"] = dbStats.OpenConnections
	stats["in_use"] = dbStats.InUse
	stats["idle"] = dbStats.Idle
	stats["wait_count"] = dbStats.WaitCount
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = dbStats.MaxIdleClosed
	stats["max_idle_time_closed"] = dbStats.MaxIdleTimeClosed
	stats["max_lifetime_closed"] = dbStats.MaxLifetimeClosed

	// Include cached original settings information
	h.settingsMux.RLock()
	originalSettings := h.originalSettings
	h.settingsMux.RUnlock()

	if originalSettings != nil {
		stats["cached_original_settings"] = map[string]interface{}{
			"max_idle_conns":     originalSettings.MaxIdleCons,
			"max_open_conns":     originalSettings.MaxOpenCons,
			"conn_max_lifetime":  originalSettings.ConnMaxLifetime.String(),
			"conn_max_idle_time": originalSettings.ConnMaxIdleTime.String(),
		}
	} else {
		stats["cached_original_settings"] = nil
	}

	return stats
}
