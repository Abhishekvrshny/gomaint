# Database Service with Maintenance Mode

This example demonstrates how to use the GoMaint library with the generic database handler for managing maintenance mode in a web service. The handler works with any ORM library that provides access to the underlying `*sql.DB` (GORM, XORM, etc.).

## Features

- **Database Operations**: CRUD operations for users using SQL database
- **Maintenance Mode**: Automatic maintenance mode handling via etcd
- **Health Checks**: Database health monitoring
- **Connection Pool Management**: Automatic adjustment of database connection pools during maintenance
- **Docker Support**: Complete Docker Compose setup with PostgreSQL and etcd

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│Database Service │    │   PostgreSQL    │    │      etcd       │
│                 │    │                 │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   HTTP API  │ │    │ │    Users    │ │    │ │ Maintenance │ │
│ │             │ │    │ │    Table    │ │    │ │    State    │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │                 │    │                 │
│ │DB Handler   │ │◄──►│                 │    │                 │
│ │(Generic)    │ │    │                 │    │                 │
│ └─────────────┘ │    └─────────────────┘    └─────────────────┘
│ ┌─────────────┐ │                                     ▲
│ │  Manager    │ │◄────────────────────────────────────┘
│ │             │ │
│ └─────────────┘ │
└─────────────────┘
```

## Quick Start

### Using Docker Compose (Recommended)

1. **Start all services**:
   ```bash
   docker-compose up -d
   ```

2. **Check service health**:
   ```bash
   curl http://localhost:8080/health
   ```

3. **Test the API**:
   ```bash
   # List users
   curl http://localhost:8080/users

   # Create a user
   curl -X POST http://localhost:8080/users \
     -H "Content-Type: application/json" \
     -d '{"name":"Alice","email":"alice@example.com"}'

   # Get a specific user
   curl http://localhost:8080/users/1
   ```

4. **Enable maintenance mode**:
   ```bash
   docker-compose exec etcdctl etcdctl put /maintenance/database-service true
   ```

5. **Disable maintenance mode**:
   ```bash
   docker-compose exec etcdctl etcdctl put /maintenance/database-service false
   ```

### Manual Setup

1. **Start PostgreSQL**:
   ```bash
   docker run -d --name postgres \
     -e POSTGRES_DB=testdb \
     -e POSTGRES_USER=postgres \
     -e POSTGRES_PASSWORD=postgres \
     -p 5432:5432 \
     postgres:15-alpine
   ```

2. **Start etcd**:
   ```bash
   docker run -d --name etcd \
     -p 2379:2379 \
     -p 2380:2380 \
     quay.io/coreos/etcd:v3.5.9 \
     /usr/local/bin/etcd \
     --name=etcd0 \
     --data-dir=/etcd-data \
     --listen-client-urls=http://0.0.0.0:2379 \
     --advertise-client-urls=http://0.0.0.0:2379 \
     --listen-peer-urls=http://0.0.0.0:2380 \
     --initial-advertise-peer-urls=http://0.0.0.0:2380 \
     --initial-cluster=etcd0=http://0.0.0.0:2380 \
     --initial-cluster-token=etcd-cluster-1 \
     --initial-cluster-state=new
   ```

3. **Run the service**:
   ```bash
   go mod tidy
   go run main.go
   ```

## API Endpoints

### Health Check
- **GET** `/health` - Service health status

### Users API
- **GET** `/users` - List all users
- **POST** `/users` - Create a new user
- **GET** `/users/{id}` - Get user by ID
- **PUT** `/users/{id}` - Update user by ID
- **DELETE** `/users/{id}` - Delete user by ID

### Statistics
- **GET** `/stats` - Service and database statistics

## Maintenance Mode Behavior

When maintenance mode is enabled:

1. **HTTP Endpoints**: Return `503 Service Unavailable` for all user operations
2. **Health Check**: Still responds but indicates maintenance status
3. **Database Connections**: Connection pool is reduced to minimize database load
4. **Graceful Handling**: Existing connections are allowed to complete

When maintenance mode is disabled:

1. **HTTP Endpoints**: Resume normal operation
2. **Database Connections**: Connection pool is restored to normal levels
3. **Health Check**: Returns to normal status

## Configuration

Environment variables:

- `DATABASE_URL`: PostgreSQL connection string (default: `host=localhost user=postgres password=postgres dbname=testdb port=5432 sslmode=disable`)
- `ETCD_ENDPOINTS`: etcd endpoints (default: `localhost:2379`)

## Database Schema

```sql
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Database Handler Features

The generic database handler provides:

- **Connection Pool Management**: Automatically adjusts connection pool settings during maintenance
- **Health Monitoring**: Continuous database health checks
- **Statistics**: Detailed connection pool and database statistics
- **Graceful Degradation**: Smooth transition between normal and maintenance modes

## Testing Maintenance Mode

1. **Start the service** and verify it's running:
   ```bash
   curl http://localhost:8080/health
   ```

2. **Create some test data**:
   ```bash
   curl -X POST http://localhost:8080/users \
     -H "Content-Type: application/json" \
     -d '{"name":"Test User","email":"test@example.com"}'
   ```

3. **Enable maintenance mode**:
   ```bash
   # Using docker-compose
   docker-compose exec etcdctl etcdctl put /maintenance/database-service true
   
   # Or using local etcdctl
   etcdctl put /maintenance/database-service true
   ```

4. **Test maintenance behavior**:
   ```bash
   # Health check should show maintenance mode
   curl http://localhost:8080/health
   
   # User operations should return 503
   curl http://localhost:8080/users
   ```

5. **Check statistics during maintenance**:
   ```bash
   curl http://localhost:8080/stats
   ```

6. **Disable maintenance mode**:
   ```bash
   docker-compose exec etcdctl etcdctl put /maintenance/database-service false
   ```

## Cleanup

```bash
# Stop and remove containers
docker-compose down -v

# Remove images (optional)
docker-compose down --rmi all -v
```

## Production Considerations

1. **Database Connections**: Tune connection pool settings based on your database capacity
2. **Health Checks**: Implement more sophisticated health checks for production
3. **Monitoring**: Add metrics and logging for better observability
4. **Security**: Use proper authentication and TLS in production
5. **Backup**: Ensure proper database backup strategies during maintenance
