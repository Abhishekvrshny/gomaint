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
	"strconv"
	"syscall"
	"time"

	"github.com/abhishekvarshney/gomaint"
	"github.com/abhishekvarshney/gomaint/pkg/handlers/database"
		_ "github.com/lib/pq"
	"xorm.io/xorm"
)

// User model for XORM
type User struct {
	ID        int64     `xorm:"pk autoincr" json:"id"`
	Name      string    `xorm:"varchar(255) not null" json:"name"`
	Email     string    `xorm:"varchar(255) unique not null" json:"email"`
	CreatedAt time.Time `xorm:"created" json:"created_at"`
	UpdatedAt time.Time `xorm:"updated" json:"updated_at"`
}

// TableName returns the table name for XORM
func (User) TableName() string {
	return "users"
}

// XormWrapper wraps the XORM engine to match our database interface
type XormWrapper struct {
	engine *xorm.Engine
}

// DB returns the underlying sql.DB from XORM engine
func (x *XormWrapper) DB() (*sql.DB, error) {
	return x.engine.DB().DB, nil
}

// App holds the application dependencies
type App struct {
	engine  *xorm.Engine
	manager *gomaint.Manager
	server  *http.Server
}

func main() {
	fmt.Println("XORM Service with Maintenance Mode")
	fmt.Println("==================================")

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
	host := getEnv("DB_HOST", "postgres")
	port := 5432
	user := "postgres"
	password := "postgres"
	dbname := getEnv("DB_NAME", "xormdb")
	sslmode := "disable"

	// Construct the connection string (DSN)
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)
	
	log.Printf("Connecting to database: host=%s, port=%d, dbname=%s", host, port, dbname)

	// Create XORM engine
	engine, err := xorm.NewEngine("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create XORM engine: %w", err)
	}

	// Enable SQL logging (optional)
	engine.ShowSQL(true)

	// Sync the schema (create tables if they don't exist)
	if err := engine.Sync2(new(User)); err != nil {
		return nil, fmt.Errorf("failed to sync database schema: %w", err)
	}

	// Insert sample data if table is empty
	if err := insertSampleData(engine); err != nil {
		log.Printf("Warning: Failed to insert sample data: %v", err)
		// Don't fail the startup for this
	}

	// Create XORM database handler using the convenience wrapper
	xormWrapper := &XormWrapper{engine: engine}
	xormHandler := database.NewXORMHandler(xormWrapper, log.Default())

	// Create maintenance manager using new simplified API
	endpoints := []string{getEnv("ETCD_ENDPOINTS", "localhost:2379")}
	mgr, err := gomaint.StartWithEtcd(
		context.Background(),
		endpoints,
		"/maintenance/xorm-service",
		30*time.Second,
		xormHandler, // Register handler during creation
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create maintenance manager: %w", err)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	app := &App{
		engine:  engine,
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

	fmt.Printf("Server running on http://localhost:8081\n")
	fmt.Println("Endpoints:")
	fmt.Println("  Health check: http://localhost:8081/health")
	fmt.Println("  Users API: http://localhost:8081/users")
	fmt.Println("  Stats: http://localhost:8081/stats")
	fmt.Println()
	fmt.Println("To enable maintenance mode:")
	fmt.Println("  etcdctl put /maintenance/xorm-service true")
	fmt.Println()
	fmt.Println("To disable maintenance mode:")
	fmt.Println("  etcdctl put /maintenance/xorm-service false")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop maintenance manager
	app.manager.Stop()

	// Close XORM engine
	app.engine.Close()

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
	if app.manager.IsInMaintenance() {
		http.Error(w, "Service under maintenance", http.StatusServiceUnavailable)
		return
	}

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
	var users []User
	if err := app.engine.Find(&users); err != nil {
		log.Printf("Failed to fetch users: %v", err)
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
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

	// XORM will automatically set created_at and updated_at
	affected, err := app.engine.Insert(&user)
	if err != nil {
		log.Printf("Failed to create user: %v", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	if affected == 0 {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (app *App) getUser(w http.ResponseWriter, r *http.Request) {
	// Simple ID extraction - in production use proper routing
	idStr := r.URL.Path[len("/users/"):]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var user User
	has, err := app.engine.ID(id).Get(&user)
	if err != nil {
		log.Printf("Failed to fetch user: %v", err)
		http.Error(w, "Failed to fetch user", http.StatusInternalServerError)
		return
	}

	if !has {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (app *App) updateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/users/"):]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var updates User
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// XORM will automatically set updated_at
	affected, err := app.engine.ID(id).Cols("name", "email").Update(&updates)
	if err != nil {
		log.Printf("Failed to update user: %v", err)
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	if affected == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Fetch the updated user
	var user User
	has, err := app.engine.ID(id).Get(&user)
	if err != nil || !has {
		log.Printf("Failed to fetch updated user: %v", err)
		http.Error(w, "Failed to fetch updated user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (app *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/users/"):]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	affected, err := app.engine.ID(id).Delete(&User{})
	if err != nil {
		log.Printf("Failed to delete user: %v", err)
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	if affected == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	// Get XORM handler stats
	var xormStats map[string]interface{}
	if handler, exists := app.manager.GetHandler("xorm"); exists {
		if xormHandler, ok := handler.(*database.Handler); ok {
			xormStats = xormHandler.GetStats()
		}
	}

	stats := map[string]interface{}{
		"maintenance": app.manager.IsInMaintenance(),
		"timestamp":   time.Now().UTC(),
		"handlers":    app.manager.GetHandlerHealth(),
		"xorm":        xormStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func insertSampleData(engine *xorm.Engine) error {
	// Check if we already have data
	count, err := engine.Count(&User{})
	if err != nil {
		return fmt.Errorf("failed to count existing users: %w", err)
	}

	// Only insert if table is empty
	if count > 0 {
		log.Printf("Users table already has %d records, skipping sample data insertion", count)
		return nil
	}

	// Insert sample users
	sampleUsers := []User{
		{Name: "Alice Johnson", Email: "alice@example.com"},
		{Name: "Charlie Brown", Email: "charlie@example.com"},
		{Name: "Diana Prince", Email: "diana@example.com"},
	}

	affected, err := engine.Insert(&sampleUsers)
	if err != nil {
		return fmt.Errorf("failed to insert sample data: %w", err)
	}

	log.Printf("Inserted %d sample users", affected)
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}