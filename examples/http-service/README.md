# HTTP Service with Maintenance Mode - Docker Example

This example demonstrates how to run the HTTP service with etcd using Docker Compose.

## Prerequisites

- Docker
- Docker Compose

## Quick Start

1. Navigate to the http-service example directory:
   ```bash
   cd examples/http-service
   ```

2. Start the services:
   ```bash
   docker-compose up -d
   ```

3. Check the services are running:
   ```bash
   docker-compose ps
   ```

## Services

### etcd
- **Port**: 2379 (client), 2380 (peer)
- **Data**: Persisted in Docker volume `etcd_data`
- **Health Check**: Automatic health checking with retry logic

### http-service
- **Port**: 8080
- **Dependencies**: Waits for etcd to be healthy before starting
- **Environment**: Configured to connect to etcd container

## Usage

### Access the HTTP Service

- Main endpoint: http://localhost:8080
- Health check: http://localhost:8080/health
- API endpoint: http://localhost:8080/api/data

### Control Maintenance Mode

You can control the maintenance mode using etcdctl from within the etcd container:

1. **Enable maintenance mode**:
   ```bash
   docker exec etcd etcdctl put /maintenance/http-service true
   ```

2. **Disable maintenance mode**:
   ```bash
   docker exec etcd etcdctl put /maintenance/http-service false
   ```

3. **Check current value**:
   ```bash
   docker exec etcd etcdctl get /maintenance/http-service
   ```

### Monitor Logs

- **All services**:
  ```bash
  docker-compose logs -f
  ```

- **HTTP service only**:
  ```bash
  docker-compose logs -f http-service
  ```

- **etcd only**:
  ```bash
  docker-compose logs -f etcd
  ```

## Testing Maintenance Mode

1. Start the services:
   ```bash
   docker-compose up -d
   ```

2. Test normal operation:
   ```bash
   curl http://localhost:8080/health
   curl http://localhost:8080/api/data
   ```

3. Enable maintenance mode:
   ```bash
   docker exec etcd etcdctl put /maintenance/http-service true
   ```

4. Test maintenance mode (should return 503):
   ```bash
   curl -i http://localhost:8080/api/data
   ```

5. Health check should still work:
   ```bash
   curl http://localhost:8080/health
   ```

6. Disable maintenance mode:
   ```bash
   docker exec etcd etcdctl put /maintenance/http-service false
   ```

## Cleanup

Stop and remove all containers and networks:
```bash
docker-compose down
```

Remove volumes (this will delete etcd data):
```bash
docker-compose down -v
```

## Configuration

### Environment Variables

The http-service container uses the following environment variables:

- `ETCD_ENDPOINTS`: Comma-separated list of etcd endpoints (default: "etcd:2379")

### etcd Configuration

The etcd service is configured with:
- Single-node cluster
- Data persistence
- Health checks
- Auto-compaction enabled
- 4GB backend quota

## Troubleshooting

### Service won't start
- Check if ports 8080, 2379, or 2380 are already in use
- Verify Docker and Docker Compose are installed and running

### etcd connection issues
- Ensure etcd container is healthy: `docker-compose ps`
- Check etcd logs: `docker-compose logs etcd`

### HTTP service can't connect to etcd
- Verify both containers are on the same network
- Check if etcd is responding: `docker exec etcd etcdctl endpoint health`
