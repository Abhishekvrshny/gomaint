package database

import (
	"log"
)

// XormDB interface for XORM compatibility
// XORM also provides access to sql.DB through DB() method
type XormDB interface {
	DB
}

// NewXORMHandler creates a new database handler specifically for XORM
func NewXORMHandler(db XormDB, logger *log.Logger) *Handler {
	return NewDatabaseHandler("xorm", db, logger)
}