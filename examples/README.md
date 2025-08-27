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

### 4. SQS Service (`sqs-service/`)
- **Purpose**: SQS message processing service with drain capabilities
- **Queue**: Amazon SQS (via LocalStack)
- **HTTP Port**: `8082`
- **LocalStack**: Port `4566`
- **etcd**: Port `2399/2400`
- **etcd Key**: `/maintenance/sqs-service`

### 5. Gin Service (`gin-service/`)
- **Purpose**: RESTful API service using Gin framework
- **Framework**: Gin Web Framework
- **HTTP Port**: `8084`
- **etcd**: Port `2403/2404`
- **etcd Key**: `/maintenance/gin-service`
- **Features**: CRUD operations, user search, JSON API

## Running Multiple Services

All examples are designed to run independently without conflicts:

### Port Allocation
| Service | HTTP | PostgreSQL/Kafka/LocalStack | etcd Client | etcd Peer |
|---------|------|-----------------------------|-------------|-----------|
| http-service | 8080 | N/A | N/A | N/A |
| gorm-service | 8080 | 5432 | 2379 | 2380 |
| xorm-service | 8081 | 5433 | 2389 | 2390 |
| sqs-service | 8082 | 4566 (LocalStack) | 2399 | 2400 |
| gin-service | 8084 | N/A | 2403 | 2404 |
| kafka-sarama-service | 8083 | 9092 (Kafka) | 2401 | 2402 |
| kafka-confluent-service | 8084 | 9092 (Kafka) | 2401 | 2402 |

### Resource Separation
- **gorm-service**: Uses database `testdb`
- **xorm-service**: Uses database `xormdb`
- **sqs-service**: Uses SQS queue `test-queue` in LocalStack

### Container Naming
- **gorm-service**: `database-postgres`, `etcd`, `database-service`
- **xorm-service**: `xorm-postgres`, `xorm-etcd`, `xorm-service`
- **sqs-service**: `sqs-localstack`, `sqs-etcd`, `sqs-service`, `sqs-aws-cli`
- **gin-service**: `gin-etcd`, `gin-service`

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

# SQS Service
cd ../sqs-service
docker-compose up -d
curl http://localhost:8082/health

# Gin Service
cd ../gin-service
docker-compose up -d
curl http://localhost:8084/health
```

### Run Multiple Services Simultaneously

```bash
# Terminal 1 - Start gorm-service
cd examples/gorm-service
docker-compose up

# Terminal 2 - Start xorm-service
cd examples/xorm-service
docker-compose up

# Terminal 3 - Start sqs-service  
cd examples/sqs-service
docker-compose up

# Terminal 4 - Start gin-service
cd examples/gin-service
docker-compose up

# Terminal 5 - Test all services
curl http://localhost:8080/users         # gorm-service
curl http://localhost:8081/users         # xorm-service  
curl http://localhost:8082/messages/stats # sqs-service
curl http://localhost:8084/api/v1/users  # gin-service
```

## Generic Database Handler

Both `gorm-service` and `xorm-service` demonstrate the same generic database handler working with different ORMs:

```go
// GORM usage
gormHandler := database.NewDatabaseHandler("gorm", gormDB, 30*time.Second, logger)

// XORM usage  
xormWrapper := &XormWrapper{engine: xormEngine}
xormHandler := database.NewDatabaseHandler("xorm", xormWrapper, 30*time.Second, logger)

// Any ORM that provides access to sql.DB
genericHandler := database.NewDatabaseHandler("my-orm", ormDB, 30*time.Second, logger)
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

# sqs-service
ETCDCTL_API=3 etcdctl --endpoints=localhost:2399 put /maintenance/sqs-service true
curl http://localhost:8082/health  # Should show maintenance mode

# gin-service
ETCDCTL_API=3 etcdctl --endpoints=localhost:2403 put /maintenance/gin-service true
curl http://localhost:8084/health  # Should show maintenance mode
```

## Architecture Comparison

All services follow the same architecture pattern:

1. **HTTP Layer**: REST API with health checks
2. **Business Logic**: CRUD operations or message processing
3. **Resource Handler**: Maintenance-aware resource management
   - **HTTP Handler**: Request rejection and graceful shutdown (http-service, gin-service)
   - **Database Handler**: Connection pool management (GORM, XORM)
   - **SQS Handler**: Message processing and draining
4. **Resource Layer**: PostgreSQL, SQS, or HTTP requests
5. **Coordination**: etcd for maintenance state

The key insight is that different handlers abstract maintenance mode management from the specific resource type, providing consistent behavior across databases, message queues, and HTTP services while handling their unique maintenance requirements (connection pooling vs. message draining vs. request rejection). The gin-service demonstrates advanced HTTP service patterns with RESTful APIs while maintaining the same maintenance mode integration.