package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abhishekvarshney/gomaint"

	"github.com/abhishekvarshney/gomaint/pkg/handlers/database"
	_ "github.com/lib/pq"
)

// MockDB implements the database.DB interface for demonstration
type MockDB struct {
	db *sql.DB
}

func (m *MockDB) DB() (*sql.DB, error) {
	return m.db, nil
}

// User model for demonstration
type User struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// App holds the application dependencies
type App struct {
	db      *sql.DB
	manager *gomaint.Manager
	server  *http.Server
}

func main() {
	fmt.Println("Database Service with Maintenance Mode")
	fmt.Println("=======================================")

	app, err := setupApp()
	if err != nil {
		log.Fatalf("Failed to setup application: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Start the application
	if err := app.run(ctx); err != nil {
		log.Fatalf("Application failed: %v", err)
	}

	log.Println("Application stopped")
}

func setupApp() (*App, error) {
	host := "postgres"
	port := 5432
	user := "postgres"
	password := "postgres"
	dbname := "testdb"
	sslmode := "disable"

	// 2. Construct the connection string (DSN)
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create in-memory database: %w", err)
	}

	// Create users table
	if err := createUsersTable(db); err != nil {
		return nil, fmt.Errorf("failed to create users table: %w", err)
	}

	// Create database handler (works with GORM, XORM, or any ORM with sql.DB access)
	mockDB := &MockDB{db: db}
	dbHandler := database.NewDatabaseHandler("database", mockDB, 30*time.Second, log.Default())

	// Create maintenance manager using new simplified API
	endpoints := []string{getEnv("ETCD_ENDPOINTS", "localhost:2379")}
	mgr, err := gomaint.StartWithEtcd(
		context.Background(),
		endpoints,
		"/maintenance/database-service",
		30*time.Second,
		dbHandler, // Register handler during creation
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create maintenance manager: %w", err)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	app := &App{
		db:      db,
		manager: mgr,
		server: &http.Server{
			Addr:    ":8080",
			Handler: mux,
		},
	}

	// Setup routes
	app.setupRoutes(mux)

	return app, nil
}

func createUsersTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,  -- or BIGSERIAL for a larger range
    name TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

	_, err := db.Exec(query)
	return err
}

func (app *App) setupRoutes(mux *http.ServeMux) {
	// Health check endpoint
	mux.HandleFunc("/health", app.healthHandler)

	// User CRUD endpoints
	mux.HandleFunc("/users", app.usersHandler)
	mux.HandleFunc("/users/", app.userHandler)

	// Stats endpoint
	mux.HandleFunc("/stats", app.statsHandler)
}

func (app *App) run(ctx context.Context) error {
	// Maintenance manager is already started by StartWithEtcd

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on %s", app.server.Addr)
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server failed: %v", err)
		}
	}()

	fmt.Printf("Server running on http://localhost%s\n", app.server.Addr)
	fmt.Println("Endpoints:")
	fmt.Println("  Health check: http://localhost:8080/health")
	fmt.Println("  Users API: http://localhost:8080/users")
	fmt.Println("  Stats: http://localhost:8080/stats")
	fmt.Println()
	fmt.Println("To enable maintenance mode:")
	fmt.Println("  etcdctl put /maintenance/database-service true")
	fmt.Println()
	fmt.Println("To disable maintenance mode:")
	fmt.Println("  etcdctl put /maintenance/database-service false")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop maintenance manager
	app.manager.Stop()

	// Close database connection
	app.db.Close()

	// Shutdown HTTP server
	return app.server.Shutdown(shutdownCtx)
}

func (app *App) healthHandler(w http.ResponseWriter, r *http.Request) {
	if app.manager.IsInMaintenance() {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "Service Unavailable",
			"message": "Service is currently under maintenance. Please try again later.",
			"code":    503,
		})
		return
	}

	// Check database health
	health := app.manager.GetHandlerHealth()
	allHealthy := true
	for _, healthy := range health {
		if !healthy {
			allHealthy = false
			break
		}
	}

	status := "healthy"
	statusCode := http.StatusOK
	if !allHealthy {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      status,
		"maintenance": app.manager.IsInMaintenance(),
		"handlers":    health,
		"timestamp":   time.Now().UTC(),
	})
}

func (app *App) usersHandler(w http.ResponseWriter, r *http.Request) {
	/*
		if app.manager.IsInMaintenance() {
			http.Error(w, "Service under maintenance", http.StatusServiceUnavailable)
			return
		}
	*/

	switch r.Method {
	case http.MethodGet:
		app.listUsers(w, r)
	case http.MethodPost:
		app.createUser(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *App) userHandler(w http.ResponseWriter, r *http.Request) {
	if app.manager.IsInMaintenance() {
		http.Error(w, "Service under maintenance", http.StatusServiceUnavailable)
		return
	}

	// Extract user ID from path
	// This is a simple implementation - in production, use a proper router
	switch r.Method {
	case http.MethodGet:
		app.getUser(w, r)
	case http.MethodPut:
		app.updateUser(w, r)
	case http.MethodDelete:
		app.deleteUser(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *App) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := app.db.Query("SELECT id, name, email, created_at, updated_at FROM users ORDER BY id")
	if err != nil {
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.UpdatedAt); err != nil {
			http.Error(w, "Failed to scan user", http.StatusInternalServerError)
			return
		}
		users = append(users, user)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func (app *App) createUser(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	query := "INSERT INTO users (name, email) VALUES (?, ?) RETURNING id, created_at, updated_at"
	err := app.db.QueryRow(query, user.Name, user.Email).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		// SQLite doesn't support RETURNING, so we need to do it differently
		result, err := app.db.Exec("INSERT INTO users (name, email) VALUES (?, ?)", user.Name, user.Email)
		if err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}

		id, err := result.LastInsertId()
		if err != nil {
			http.Error(w, "Failed to get user ID", http.StatusInternalServerError)
			return
		}

		user.ID = int(id)
		user.CreatedAt = time.Now()
		user.UpdatedAt = time.Now()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (app *App) getUser(w http.ResponseWriter, r *http.Request) {
	// Simple ID extraction - in production use proper routing
	id := r.URL.Path[len("/users/"):]

	var user User
	query := "SELECT id, name, email, created_at, updated_at FROM users WHERE id = ?"
	err := app.db.QueryRow(query, id).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to fetch user", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (app *App) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/users/"):]

	var updates User
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	query := "UPDATE users SET name = ?, email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
	result, err := app.db.Exec(query, updates.Name, updates.Email, id)
	if err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Failed to get affected rows", http.StatusInternalServerError)
		return
	}

	if rowsAffected == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Fetch the updated user
	var user User
	query = "SELECT id, name, email, created_at, updated_at FROM users WHERE id = ?"
	err = app.db.QueryRow(query, id).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		http.Error(w, "Failed to fetch updated user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (app *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/users/"):]

	result, err := app.db.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Failed to get affected rows", http.StatusInternalServerError)
		return
	}

	if rowsAffected == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	// Get database handler stats
	var dbStats map[string]interface{}
	if handler, exists := app.manager.GetHandler("database"); exists {
		if dbHandler, ok := handler.(*database.Handler); ok {
			dbStats = dbHandler.GetStats()
		}
	}

	stats := map[string]interface{}{
		"maintenance": app.manager.IsInMaintenance(),
		"timestamp":   time.Now().UTC(),
		"handlers":    app.manager.GetHandlerHealth(),
		"database":    dbStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
