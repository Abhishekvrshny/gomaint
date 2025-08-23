# XORM Service with Maintenance Mode

This example demonstrates how to use the GoMaint library with XORM (eXtensible Object-Relational Mapping) for managing maintenance mode in a web service. It showcases the generic database handler working seamlessly with XORM.

## Features

- **Database Operations**: CRUD operations for users using XORM
- **Maintenance Mode**: Automatic maintenance mode handling via etcd
- **Health Checks**: Database health monitoring through XORM
- **Connection Pool Management**: Automatic adjustment of database connection pools during maintenance
- **Docker Support**: Complete Docker Compose setup with PostgreSQL and etcd
- **XORM Features**: Schema synchronization, SQL logging, and ORM capabilities

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  XORM Service   │    │   PostgreSQL    │    │      etcd       │
│                 │    │                 │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   HTTP API  │ │    │ │    Users    │ │    │ │ Maintenance │ │
│ │             │ │    │ │    Table    │ │    │ │    State    │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │                 │    │                 │
│ │XORM Handler │ │◄──►│                 │    │                 │
│ │(Generic DB) │ │    │                 │    │                 │
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
   curl http://localhost:8081/health
   ```

3. **Test the API**:
   ```bash
   # List users
   curl http://localhost:8081/users

   # Create a user
   curl -X POST http://localhost:8081/users \
     -H "Content-Type: application/json" \
     -d '{"name":"Alice Cooper","email":"alice.cooper@example.com"}'

   # Get a specific user
   curl http://localhost:8081/users/1
   ```

4. **Enable maintenance mode**:
   ```bash
   docker-compose exec xorm-etcd etcdctl put /maintenance/xorm-service true
   ```

5. **Disable maintenance mode**:
   ```bash
   docker-compose exec xorm-etcd etcdctl put /maintenance/xorm-service false
   ```

### Manual Setup

1. **Start PostgreSQL**:
   ```bash
   docker run -d --name xorm-postgres \
     -e POSTGRES_DB=xormdb \
     -e POSTGRES_USER=postgres \
     -e POSTGRES_PASSWORD=postgres \
     -p 5433:5432 \
     postgres:15-alpine
   ```

2. **Start etcd**:
   ```bash
   docker run -d --name xorm-etcd \
     -p 2389:2379 \
     -p 2390:2380 \
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

3. **Install XORM dependency**:
   ```bash
   go mod tidy
   ```

4. **Run the service**:
   ```bash
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

- `DB_HOST`: PostgreSQL host (default: `postgres`)
- `DB_NAME`: PostgreSQL database name (default: `xormdb`)
- `ETCD_ENDPOINTS`: etcd endpoints (default: `localhost:2379`)

**Note**: The service constructs the database connection string from individual environment variables rather than using a single `DATABASE_URL`. This provides more flexibility for configuration.

## Port Configuration

To avoid conflicts with the gorm-service example:

- **HTTP Service**: Port `8081` (instead of `8080`)
- **PostgreSQL**: Port `5433` (instead of `5432`)
- **etcd**: Ports `2389/2390` (instead of `2379/2380`)
- **Database**: `xormdb` (instead of `testdb`)

## Database Schema

The XORM model automatically creates the following schema via `Sync2()`:

```sql
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,    -- XORM uses BIGINT for pk autoincr
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**Note:** XORM handles the complete schema creation and sample data insertion automatically. No manual SQL initialization is required.

## XORM Features Demonstrated

This example showcases several XORM features:

- **Model Definition**: Struct tags for database mapping (`xorm:"pk autoincr"`)
- **Schema Synchronization**: `Sync2()` for automatic table creation
- **SQL Logging**: Enabled for debugging purposes
- **CRUD Operations**: Insert, Find, Get, Update, Delete
- **Automatic Timestamps**: `created` and `updated` tags for automatic timestamp management
- **Database Engine**: PostgreSQL integration through XORM

## XORM Handler Features

The XORM handler (using the generic database handler) provides:

- **Connection Pool Management**: Automatically adjusts connection pool settings during maintenance
- **Health Monitoring**: Continuous database health checks through XORM's underlying sql.DB
- **Statistics**: Detailed connection pool and database statistics
- **Graceful Degradation**: Smooth transition between normal and maintenance modes

## Testing Maintenance Mode

1. **Start the service** and verify it's running:
   ```bash
   curl http://localhost:8081/health
   ```

2. **Create some test data**:
   ```bash
   curl -X POST http://localhost:8081/users \
     -H "Content-Type: application/json" \
     -d '{"name":"Test User","email":"test@example.com"}'
   ```

3. **Enable maintenance mode**:
   ```bash
   # Using docker-compose
   docker-compose exec xorm-etcd etcdctl put /maintenance/xorm-service true
   
   # Or using local etcdctl (with correct port)
   ETCDCTL_API=3 etcdctl --endpoints=localhost:2389 put /maintenance/xorm-service true
   ```

4. **Test maintenance behavior**:
   ```bash
   # Health check should show maintenance mode
   curl http://localhost:8081/health
   
   # User operations should return 503
   curl http://localhost:8081/users
   ```

5. **Check statistics during maintenance**:
   ```bash
   curl http://localhost:8081/stats
   ```

6. **Disable maintenance mode**:
   ```bash
   docker-compose exec xorm-etcd etcdctl put /maintenance/xorm-service false
   ```

## XORM vs GORM Comparison

This example demonstrates that the generic database handler works seamlessly with different ORMs:

| Feature | XORM Implementation | GORM Implementation |
|---------|-------------------|-------------------|
| Model Definition | Struct tags (`xorm:"pk autoincr"`) | Struct tags (`gorm:"primaryKey"`) |
| Schema Sync | `engine.Sync2()` | `db.AutoMigrate()` |
| Create | `engine.Insert(&user)` | `db.Create(&user)` |
| Read | `engine.Find(&users)` | `db.Find(&users)` |
| Update | `engine.ID(id).Update()` | `db.Model(&user).Updates()` |
| Delete | `engine.ID(id).Delete()` | `db.Delete(&user, id)` |
| Handler | `database.NewDatabaseHandler("xorm", ...)` | `database.NewDatabaseHandler("gorm", ...)` |

Both use the same underlying generic database handler for maintenance mode management!

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
5. **Error Handling**: Implement comprehensive error handling and recovery
6. **XORM Configuration**: Fine-tune XORM settings for optimal performance
7. **Database Indexes**: Add appropriate indexes for your query patterns

## Dependencies

This example uses:

- **XORM**: `xorm.io/xorm` - The eXtensible Object-Relational Mapping library
- **PostgreSQL Driver**: `github.com/lib/pq` - PostgreSQL driver for database/sql
- **GoMaint**: The maintenance mode management library
- **etcd**: For coordination and configuration management