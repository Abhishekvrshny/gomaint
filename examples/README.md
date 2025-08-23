# GoMaint Examples

This directory contains comprehensive examples demonstrating the GoMaint library with different configurations and ORM libraries.

## Available Examples

### 1. HTTP Service (`http-service/`)
- **Purpose**: Basic HTTP service with maintenance mode
- **Features**: HTTP request handling, graceful degradation
- **Port**: `8080`
- **etcd Key**: `/maintenance/http-service`

### 2. Database Service (`gorm-service/`)
- **Purpose**: Database-backed service using generic database handler
- **ORM**: Originally designed for GORM, now works with any ORM
- **Database**: `testdb` on PostgreSQL (port `5432`)
- **HTTP Port**: `8080`
- **etcd**: Port `2379/2380`
- **etcd Key**: `/maintenance/database-service`

### 3. XORM Service (`xorm-service/`)
- **Purpose**: XORM-specific database service
- **ORM**: XORM with PostgreSQL
- **Database**: `xormdb` on PostgreSQL (port `5433`)
- **HTTP Port**: `8081`
- **etcd**: Port `2389/2390`
- **etcd Key**: `/maintenance/xorm-service`

## Running Multiple Services

All examples are designed to run independently without conflicts:

### Port Allocation
| Service | HTTP | PostgreSQL | etcd Client | etcd Peer |
|---------|------|------------|-------------|-----------|
| http-service | 8080 | N/A | N/A | N/A |
| gorm-service | 8080 | 5432 | 2379 | 2380 |
| xorm-service | 8081 | 5433 | 2389 | 2390 |

### Database Separation
- **gorm-service**: Uses database `testdb`
- **xorm-service**: Uses database `xormdb`

### Container Naming
- **gorm-service**: `database-postgres`, `etcd`, `database-service`
- **xorm-service**: `xorm-postgres`, `xorm-etcd`, `xorm-service`

## Quick Start

### Run Individual Services

```bash
# HTTP Service only
cd http-service
docker-compose up -d
curl http://localhost:8080/health

# Database Service (Generic handler)
cd ../gorm-service
docker-compose up -d
curl http://localhost:8080/health

# XORM Service
cd ../xorm-service
docker-compose up -d
curl http://localhost:8081/health
```

### Run Multiple Services Simultaneously

```bash
# Terminal 1 - Start gorm-service
cd examples/gorm-service
docker-compose up

# Terminal 2 - Start xorm-service
cd examples/xorm-service
docker-compose up

# Terminal 3 - Test both
curl http://localhost:8080/users  # gorm-service
curl http://localhost:8081/users  # xorm-service
```

## Generic Database Handler

Both `gorm-service` and `xorm-service` demonstrate the same generic database handler working with different ORMs:

```go
// GORM usage
gormHandler := database.NewGORMHandler(gormDB, logger)

// XORM usage  
xormWrapper := &XormWrapper{engine: xormEngine}
xormHandler := database.NewXORMHandler(xormWrapper, logger)

// Generic usage
genericHandler := database.NewDatabaseHandler("my-orm", ormDB, logger)
```

## Maintenance Mode Testing

Each service can be independently put into maintenance mode:

```bash
# gorm-service
etcdctl put /maintenance/database-service true
curl http://localhost:8080/health  # Should show maintenance mode

# xorm-service  
ETCDCTL_API=3 etcdctl --endpoints=localhost:2389 put /maintenance/xorm-service true
curl http://localhost:8081/health  # Should show maintenance mode
```

## Architecture Comparison

All services follow the same architecture pattern:

1. **HTTP Layer**: REST API with health checks
2. **Business Logic**: CRUD operations
3. **Database Handler**: Generic maintenance-aware database connection management
4. **ORM Layer**: GORM, XORM, or direct SQL
5. **Database**: PostgreSQL
6. **Coordination**: etcd for maintenance state

The key insight is that the generic database handler abstracts maintenance mode management from the specific ORM choice, allowing the same connection pool management logic to work across different database libraries.