# Gin Service with Maintenance Mode

A comprehensive example demonstrating GoMaint maintenance mode integration with the [Gin Web Framework](https://github.com/gin-gonic/gin). This service showcases how to build a RESTful API with maintenance mode support, graceful shutdowns, and proper HTTP handling.

## Features

- **RESTful API**: Full CRUD operations for user management
- **Gin Framework**: Fast HTTP web framework with middleware support  
- **Maintenance Mode**: Seamless transition with proper HTTP responses
- **Health Checks**: Comprehensive health monitoring endpoints
- **Search API**: User search functionality with query parameters
- **Graceful Shutdown**: Clean service termination with connection draining
- **Docker Support**: Containerized deployment with Docker Compose
- **Environment Configuration**: Flexible configuration via environment variables

## API Endpoints

### Health & Status (Always Available)
- `GET /health` - Service health check (bypasses maintenance mode)
- `GET /ping` - Simple ping endpoint (bypasses maintenance mode)
- `GET /metrics` - Service metrics and statistics (bypasses maintenance mode)

### Main Application (Affected by Maintenance Mode)
- `GET /` - Welcome page with service information
- `GET /about` - Service description and features
- `GET /api/v1/status` - Detailed service status

### User Management API (Affected by Maintenance Mode)
- `GET /api/v1/users` - List all users
- `POST /api/v1/users` - Create a new user
- `GET /api/v1/users/:id` - Get user by ID
- `PUT /api/v1/users/:id` - Update user by ID
- `DELETE /api/v1/users/:id` - Delete user by ID
- `GET /api/v1/search/users?q=query` - Search users by name, username, or email

## Quick Start

### Using Docker Compose (Recommended)

```bash
cd examples/gin-service
docker-compose up --build
```

The service will be available at http://localhost:8084

### Local Development

```bash
# From the project root directory
cd examples/gin-service

# Run with etcd (using Docker)
docker run -d --name etcd-gin \
  -p 2379:2379 \
  quay.io/coreos/etcd:v3.5.9 \
  etcd --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://localhost:2379

# Run the service from project root
cd ../../
go run examples/gin-service/main.go
```

The service will be available at http://localhost:8080

### Environment Variables

Configure the service using environment variables:

```bash
# Server configuration
HTTP_PORT=8080              # HTTP server port (default: 8080)
GIN_MODE=release           # Gin mode: debug, release, test (default: release)

# Maintenance configuration  
ETCD_ENDPOINTS=localhost:2379  # etcd endpoints (default: localhost:2379)
ETCD_KEY=/maintenance/gin-service  # etcd key to watch (default: /maintenance/gin-service)
DRAIN_TIMEOUT=30s          # Connection drain timeout (default: 30s)
```

Example with custom configuration:
```bash
HTTP_PORT=9090 \
ETCD_ENDPOINTS=etcd1:2379,etcd2:2379 \
ETCD_KEY=/maintenance/my-gin-app \
DRAIN_TIMEOUT=45s \
GIN_MODE=debug \
go run main.go
```

## Testing the API

### Basic API Usage

```bash
# Check service health
curl http://localhost:8084/health

# List users
curl http://localhost:8084/api/v1/users

# Create a new user
curl -X POST http://localhost:8084/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John Doe","email":"john@example.com","username":"johndoe"}'

# Get user by ID
curl http://localhost:8084/api/v1/users/1

# Update user
curl -X PUT http://localhost:8084/api/v1/users/1 \
  -H "Content-Type: application/json" \
  -d '{"name":"John Updated","email":"john.updated@example.com","username":"johnupdated"}'

# Search users
curl "http://localhost:8084/api/v1/search/users?q=alice"

# Delete user
curl -X DELETE http://localhost:8084/api/v1/users/1
```

### Maintenance Mode Testing

1. **Enable maintenance mode:**
   ```bash
   # Using etcd directly
   etcdctl --endpoints=localhost:2403 put /maintenance/gin-service true
   
   # Or using Docker
   docker exec -it gin-etcd etcdctl put /maintenance/gin-service true
   ```

2. **Test maintenance responses:**
   ```bash
   # API endpoints return 503 during maintenance
   curl -i http://localhost:8084/api/v1/users
   # HTTP/1.1 503 Service Unavailable
   # Retry-After: 30
   # Content-Type: application/json
   # {"error":"Service Unavailable","message":"Service is currently under maintenance..."}
   
   # Health endpoints still work
   curl http://localhost:8084/health
   # Returns 503 but with maintenance status
   
   # Ping always works
   curl http://localhost:8084/ping
   # {"message":"pong","timestamp":"..."}
   ```

3. **Disable maintenance mode:**
   ```bash
   etcdctl --endpoints=localhost:2403 put /maintenance/gin-service false
   ```

## Maintenance Mode Behavior

### During Maintenance Mode

- **API Endpoints** (`/api/v1/*`): Return `503 Service Unavailable` with proper JSON error messages
- **Main Pages** (`/`, `/about`): Return `503 Service Unavailable` with maintenance information
- **Health Check** (`/health`): Returns `503` but indicates maintenance status (not a real health issue)
- **Ping** (`/ping`): Always works (bypasses maintenance mode)
- **Metrics** (`/metrics`): Always works (bypasses maintenance mode)

### Graceful Shutdown Process

1. **Signal Reception**: Service receives SIGTERM/SIGINT
2. **New Request Blocking**: HTTP handler stops accepting new requests
3. **Active Request Draining**: Waits for in-flight requests to complete
4. **Timeout Handling**: Forces shutdown after drain timeout expires
5. **Resource Cleanup**: Closes connections and releases resources

### HTTP Response Details

**Normal Operation:**
```json
{
  "users": [...],
  "count": 3
}
```

**Maintenance Mode:**
```json
{
  "error": "Service Unavailable",
  "message": "Service is currently under maintenance. Please try again later.",
  "code": 503
}
```

**Health Check (Maintenance):**
```json
{
  "status": "maintenance",
  "message": "Service is under maintenance", 
  "maintenance": true,
  "timestamp": "2023-..."
}
```

## Architecture

The gin-service demonstrates the integration between Gin framework and GoMaint:

```
┌─────────────────┐    ┌──────────────┐    ┌─────────────────┐
│   Gin Router    │◄───┤ HTTP Handler │◄───┤ GoMaint Manager │
│   - API Routes  │    │              │    │                 │
│   - Middleware  │    │ - Maintenance│    │ - etcd Source   │
└─────────────────┘    │   Detection  │    │ - Handler Mgmt  │
                       │ - Request    │    └─────────────────┘
┌─────────────────┐    │   Tracking   │           │
│   HTTP Server   │◄───┤ - Draining   │           │
│   (Go net/http) │    └──────────────┘      ┌────▼──────┐
└─────────────────┘                          │   etcd    │
                                             │  cluster  │
                                             └───────────┘
```

### Key Integration Points

1. **Gin Engine**: Used as the HTTP handler for the Go `net/http` server
2. **HTTP Handler**: Wraps the entire Gin application for maintenance mode
3. **Skip Paths**: Health checks and metrics bypass maintenance mode
4. **Middleware**: Gin's built-in logging and recovery middleware
5. **JSON Responses**: Consistent JSON API with proper error handling

## Development

### Project Structure

```
gin-service/
├── main.go              # Main application with Gin setup
├── Dockerfile           # Container configuration
├── docker-compose.yml   # Multi-service orchestration
└── README.md           # This documentation
```

### Code Highlights

**Gin Integration:**
```go
// Create Gin engine
ginEngine := gin.New()
ginEngine.Use(gin.Logger(), gin.Recovery())

// Create HTTP server with Gin as handler
server := &http.Server{
    Addr:    ":8080",
    Handler: ginEngine,  // Gin engine as HTTP handler
}

// Wrap with maintenance handler
handler := httpHandler.NewHTTPHandler(server, drainTimeout)
handler.SkipPaths("/health", "/metrics", "/ping")
```

**Route Organization:**
```go
// API routes
api := app.ginEngine.Group("/api/v1")
{
    users := api.Group("/users")
    {
        users.GET("", app.listUsers)
        users.POST("", app.createUser)
        // ... more routes
    }
}
```

**Maintenance-Aware Handlers:**
```go
func (app *App) healthHandler(c *gin.Context) {
    if app.manager.IsInMaintenance() {
        c.JSON(http.StatusServiceUnavailable, gin.H{
            "status": "maintenance",
            "maintenance": true,
        })
        return
    }
    // Normal health check logic
}
```

## Comparison with Other Examples

| Feature | gin-service | http-service | gorm-service |
|---------|-------------|--------------|--------------|
| Framework | Gin | Standard net/http | Standard + GORM |
| API Style | RESTful JSON | Simple HTML/JSON | Database CRUD |
| Routes | Router groups | HandleFunc | HandleFunc |
| Middleware | Gin middleware | Custom wrapper | None |
| Response Format | JSON only | JSON + HTML | JSON |
| Use Case | Web APIs | Simple services | Database apps |

## Performance Notes

- **Gin Framework**: High-performance HTTP web framework
- **JSON Marshaling**: Efficient JSON handling with Gin's built-in methods  
- **Middleware Stack**: Minimal middleware for optimal performance
- **Connection Pooling**: Go's default HTTP server connection management
- **Memory Usage**: In-memory data store for demonstration (use database in production)

## Docker Deployment

The service includes Docker support with multi-stage builds:

```dockerfile
# Build stage - compiles Go binary
FROM golang:1.21-alpine AS builder

# Runtime stage - minimal Alpine image
FROM alpine:3.18
```

**Features:**
- Multi-stage build for smaller images
- Health checks for container orchestration
- Proper signal handling for graceful shutdowns
- Non-root user execution

## Troubleshooting

### Common Issues

1. **Port already in use:**
   ```bash
   # Change the port
   HTTP_PORT=9090 go run main.go
   ```

2. **etcd connection failed:**
   ```bash
   # Check etcd is running
   docker ps | grep etcd
   
   # Test etcd connectivity
   etcdctl --endpoints=localhost:2379 endpoint health
   ```

3. **Maintenance mode not working:**
   ```bash
   # Verify etcd key
   etcdctl --endpoints=localhost:2403 get /maintenance/gin-service
   
   # Check service logs
   docker-compose logs gin-service
   ```

4. **Docker build issues:**
   ```bash
   # Clean build
   docker-compose down
   docker-compose build --no-cache
   docker-compose up
   ```

## Production Considerations

1. **Database Integration**: Replace in-memory store with proper database
2. **Authentication**: Add JWT or session-based authentication
3. **Rate Limiting**: Implement request rate limiting
4. **Logging**: Use structured logging (logrus, zap)
5. **Monitoring**: Add Prometheus metrics
6. **TLS**: Enable HTTPS with proper certificates
7. **Configuration**: Use configuration files or secret management

## License

This example is part of the GoMaint project and follows the same licensing terms.