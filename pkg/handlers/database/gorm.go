package database

import (
	"log"
)

// GormDB interface for GORM compatibility
type GormDB interface {
	DB
}

// NewGORMHandler creates a new database handler specifically for GORM
// This is a convenience wrapper to maintain backward compatibility
func NewGORMHandler(db GormDB, logger *log.Logger) *Handler {
	return NewDatabaseHandler("gorm", db, logger)
}