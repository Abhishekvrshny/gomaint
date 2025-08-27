package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/abhishekvarshney/gomaint"
	httpHandler "github.com/abhishekvarshney/gomaint/pkg/handlers/http"
)

const (
	defaultPort         = "8080"
	defaultEtcdKey      = "/maintenance/gin-service"
	defaultEtcdEndpoint = "localhost:2379"
	defaultDrainTimeout = 30 * time.Second
)

// User represents a user in our API
type User struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Username string    `json:"username"`
	Created  time.Time `json:"created"`
}

// App holds the application dependencies
type App struct {
	ginEngine *gin.Engine
	server    *http.Server
	manager   *gomaint.Manager
	users     []User // In-memory store for demo
}

func main() {
	fmt.Println("Gin Service with Maintenance Mode")
	fmt.Println("=================================")

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
		log.Println("Shutting down Gin service...")
		cancel()
	}()

	// Start the application
	if err := app.run(ctx); err != nil {
		log.Fatalf("Application failed: %v", err)
	}

	log.Println("Application stopped")
}

func setupApp() (*App, error) {
	// Get configuration from environment variables
	port := getEnv("HTTP_PORT", defaultPort)
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", defaultEtcdEndpoint)
	etcdKey := getEnv("ETCD_KEY", defaultEtcdKey)

	// Parse drain timeout
	drainTimeoutStr := getEnv("DRAIN_TIMEOUT", "30s")
	drainTimeout, err := time.ParseDuration(drainTimeoutStr)
	if err != nil {
		log.Fatalf("Invalid DRAIN_TIMEOUT: %v", err)
	}

	log.Printf("Gin service starting on port %s", port)
	log.Printf("Drain timeout: %v", drainTimeout)
	log.Printf("etcd endpoints: %s", etcdEndpoints)
	log.Printf("etcd key: %s", etcdKey)

	// Set Gin to release mode if not in development
	if getEnv("GIN_MODE", "release") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin engine
	ginEngine := gin.New()
	ginEngine.Use(gin.Logger(), gin.Recovery())

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      ginEngine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	app := &App{
		ginEngine: ginEngine,
		server:    server,
		users: []User{
			{ID: 1, Name: "Alice Johnson", Email: "alice@example.com", Username: "alice", Created: time.Now().Add(-24 * time.Hour)},
			{ID: 2, Name: "Bob Smith", Email: "bob@example.com", Username: "bob", Created: time.Now().Add(-12 * time.Hour)},
			{ID: 3, Name: "Charlie Brown", Email: "charlie@example.com", Username: "charlie", Created: time.Now().Add(-6 * time.Hour)},
		},
	}

	// Setup routes
	app.setupRoutes()

	// Create HTTP handler for maintenance mode
	handler := httpHandler.NewHTTPHandler(server, drainTimeout)

	// Skip health check and metrics endpoints from maintenance mode
	handler.SkipPaths("/health", "/metrics", "/ping")

	// Parse etcd endpoints
	endpoints := strings.Split(etcdEndpoints, ",")

	// Start maintenance manager with etcd
	ctx := context.Background()
	mgr, err := gomaint.StartWithEtcd(ctx, endpoints, etcdKey, drainTimeout, handler)
	if err != nil {
		return nil, fmt.Errorf("failed to start maintenance manager: %v", err)
	}

	app.manager = mgr
	return app, nil
}

func (app *App) setupRoutes() {
	// Health endpoints (these will be skipped during maintenance)
	app.ginEngine.GET("/health", app.healthHandler)
	app.ginEngine.GET("/ping", app.pingHandler)
	app.ginEngine.GET("/metrics", app.metricsHandler)

	// API routes
	api := app.ginEngine.Group("/api/v1")
	{
		// User endpoints
		users := api.Group("/users")
		{
			users.GET("", app.listUsers)
			users.POST("", app.createUser)
			users.GET("/:id", app.getUser)
			users.PUT("/:id", app.updateUser)
			users.DELETE("/:id", app.deleteUser)
		}

		// Search endpoints
		api.GET("/search/users", app.searchUsers)

		// Status endpoint
		api.GET("/status", app.statusHandler)
	}

	// Static content
	app.ginEngine.GET("/", app.indexHandler)
	app.ginEngine.GET("/about", app.aboutHandler)
}

func (app *App) run(ctx context.Context) error {
	// Maintenance manager is already started by StartWithEtcd

	// Start HTTP server
	go func() {
		log.Printf("Starting Gin HTTP server on %s", app.server.Addr)
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	log.Println("Gin service started successfully")
	log.Println("Available endpoints:")
	log.Printf("  Main page:        http://localhost:%s/", getEnv("HTTP_PORT", defaultPort))
	log.Printf("  Health check:     http://localhost:%s/health", getEnv("HTTP_PORT", defaultPort))
	log.Printf("  Users API:        http://localhost:%s/api/v1/users", getEnv("HTTP_PORT", defaultPort))
	log.Printf("  Search API:       http://localhost:%s/api/v1/search/users?q=alice", getEnv("HTTP_PORT", defaultPort))
	log.Printf("  Service Status:   http://localhost:%s/api/v1/status", getEnv("HTTP_PORT", defaultPort))
	log.Println()
	log.Println("To enable maintenance mode:")
	log.Printf("  etcdctl put %s true", getEnv("ETCD_KEY", defaultEtcdKey))
	log.Println()
	log.Println("To disable maintenance mode:")
	log.Printf("  etcdctl put %s false", getEnv("ETCD_KEY", defaultEtcdKey))
	log.Println()
	log.Println("Press Ctrl+C to stop...")

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop maintenance manager
	app.manager.Stop()

	// Shutdown HTTP server
	if err := app.server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
		return err
	}

	log.Println("Gin service stopped gracefully")
	return nil
}

// Health and status handlers (skipped during maintenance)
func (app *App) healthHandler(c *gin.Context) {
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

	c.JSON(statusCode, gin.H{
		"status":      status,
		"maintenance": app.manager.IsInMaintenance(),
		"handlers":    health,
		"timestamp":   time.Now().UTC(),
	})
}

func (app *App) pingHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message":   "pong",
		"timestamp": time.Now().UTC(),
	})
}

func (app *App) metricsHandler(c *gin.Context) {
	// Get handler stats
	var httpStats map[string]interface{}
	if handler, exists := app.manager.GetHandler("http"); exists {
		if h, ok := handler.(*httpHandler.Handler); ok {
			httpStats = h.GetStats()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"maintenance": app.manager.IsInMaintenance(),
		"handlers":    app.manager.GetHandlerHealth(),
		"http_stats":  httpStats,
		"users_count": len(app.users),
		"timestamp":   time.Now().UTC(),
	})
}

// API handlers (affected by maintenance mode)
func (app *App) listUsers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"users": app.users,
		"count": len(app.users),
	})
}

func (app *App) createUser(c *gin.Context) {
	var newUser User
	if err := c.ShouldBindJSON(&newUser); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Simple ID assignment
	newUser.ID = len(app.users) + 1
	newUser.Created = time.Now()
	app.users = append(app.users, newUser)

	c.JSON(http.StatusCreated, gin.H{
		"message": "User created successfully",
		"user":    newUser,
	})
}

func (app *App) getUser(c *gin.Context) {
	id := c.Param("id")

	for _, user := range app.users {
		if fmt.Sprintf("%d", user.ID) == id {
			c.JSON(http.StatusOK, gin.H{"user": user})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
}

func (app *App) updateUser(c *gin.Context) {
	id := c.Param("id")

	for i, user := range app.users {
		if fmt.Sprintf("%d", user.ID) == id {
			var updateUser User
			if err := c.ShouldBindJSON(&updateUser); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Update fields (keep original ID and Created time)
			updateUser.ID = user.ID
			updateUser.Created = user.Created
			app.users[i] = updateUser

			c.JSON(http.StatusOK, gin.H{
				"message": "User updated successfully",
				"user":    updateUser,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
}

func (app *App) deleteUser(c *gin.Context) {
	id := c.Param("id")

	for i, user := range app.users {
		if fmt.Sprintf("%d", user.ID) == id {
			app.users = append(app.users[:i], app.users[i+1:]...)
			c.JSON(http.StatusOK, gin.H{
				"message": "User deleted successfully",
				"user":    user,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
}

func (app *App) searchUsers(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'q' is required"})
		return
	}

	var results []User
	for _, user := range app.users {
		if strings.Contains(strings.ToLower(user.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(user.Username), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(user.Email), strings.ToLower(query)) {
			results = append(results, user)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"query":   query,
		"results": results,
		"count":   len(results),
	})
}

func (app *App) statusHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":     "gin-service",
		"version":     "1.0.0",
		"maintenance": app.manager.IsInMaintenance(),
		"uptime":      time.Now().Format(time.RFC3339),
		"users_count": len(app.users),
		"gin_mode":    gin.Mode(),
	})
}

// Static content handlers
func (app *App) indexHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "Welcome to Gin Service with GoMaint!",
		"service": "gin-service",
		"endpoints": gin.H{
			"health":  "/health",
			"ping":    "/ping",
			"metrics": "/metrics",
			"users":   "/api/v1/users",
			"search":  "/api/v1/search/users?q=query",
			"status":  "/api/v1/status",
		},
		"maintenance": app.manager.IsInMaintenance(),
		"timestamp":   time.Now().UTC(),
	})
}

func (app *App) aboutHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":     "gin-service",
		"description": "A demonstration of GoMaint maintenance mode with Gin framework",
		"framework":   "Gin",
		"features": []string{
			"RESTful API with CRUD operations",
			"Maintenance mode support",
			"Graceful shutdown",
			"Health checks",
			"Search functionality",
			"In-memory data store",
		},
		"maintenance": app.manager.IsInMaintenance(),
		"timestamp":   time.Now().UTC(),
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
