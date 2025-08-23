package gorm

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

// Handler implements the Handler interface for GORM database operations
type Handler struct {
	*handlers.BaseHandler
	db               DB
	logger           *log.Logger
	originalSettings *ConnectionSettings
	settingsMux      sync.RWMutex
}

// NewGORMHandler creates a new GORM handler
func NewGORMHandler(db DB, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.Default()
	}
	return &Handler{
		BaseHandler: handlers.NewBaseHandler("gorm"),
		db:          db,
		logger:      logger,
	}
}

// SetOriginalSettings allows manual configuration of the original connection settings
// This is useful when you want to override the defaults that would be used for restoration
func (h *Handler) SetOriginalSettings(settings *ConnectionSettings) {
	h.settingsMux.Lock()
	defer h.settingsMux.Unlock()
	h.originalSettings = settings
	h.logger.Printf("GORM Handler: Manual original settings configured - MaxIdle: %d, MaxOpen: %d",
		settings.MaxIdleCons, settings.MaxOpenCons)
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

	h.logger.Printf("GORM Handler: Cached original settings - MaxOpen: %d (MaxIdle and timeouts use defaults)",
		h.originalSettings.MaxOpenCons)

	return nil
}

// OnMaintenanceStart handles maintenance mode activation
func (h *Handler) OnMaintenanceStart(ctx context.Context) error {
	h.logger.Println("GORM Handler: Maintenance mode enabled - Preparing database for maintenance")
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
	sqlDB.SetMaxIdleConns(0)                  // No idle connections
	sqlDB.SetMaxOpenConns(1)                  // Only 1 connection maximum
	sqlDB.SetConnMaxLifetime(1 * time.Second) // Short lifetime to force reconnection
	sqlDB.SetConnMaxIdleTime(1 * time.Second) // Very short idle time

	h.logger.Println("GORM Handler: Database prepared for maintenance mode with minimal connection settings")
	return nil
}

// OnMaintenanceEnd handles maintenance mode deactivation
func (h *Handler) OnMaintenanceEnd(ctx context.Context) error {
	h.logger.Println("GORM Handler: Maintenance mode disabled - Restoring original database operations")

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

	h.logger.Printf("GORM Handler: Restoring original settings - MaxIdle: %d, MaxOpen: %d",
		originalSettings.MaxIdleCons, originalSettings.MaxOpenCons)

	sqlDB.SetMaxIdleConns(originalSettings.MaxIdleCons)
	sqlDB.SetMaxOpenConns(originalSettings.MaxOpenCons)
	sqlDB.SetConnMaxLifetime(originalSettings.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(originalSettings.ConnMaxIdleTime)

	h.SetState(handlers.StateNormal)
	h.logger.Println("GORM Handler: Original database operations restored")
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
		h.logger.Printf("GORM Handler: Failed to get underlying sql.DB: %v", err)
		return false
	}

	// Ping database with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		h.logger.Printf("GORM Handler: Database health check failed: %v", err)
		return false
	}

	return true
}

// GetStats returns database connection statistics
func (h *Handler) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["handler_name"] = h.Name()
	stats["handler_state"] = h.State().String()

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
