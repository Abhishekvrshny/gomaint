# gRPC Service with Maintenance Mode - Docker Example

This example demonstrates how to run a gRPC service with etcd using Docker Compose, including comprehensive maintenance mode capabilities.

## Prerequisites

- Docker
- Docker Compose
- Go 1.23+ (for local development)
- protoc (Protocol Buffer Compiler)

## Quick Start

1. Navigate to the grpc-service example directory:
   ```bash
   cd examples/grpc-service
   ```

2. Start the services:
   ```bash
   docker-compose up -d
   ```

3. Check the services are running:
   ```bash
   docker-compose ps
   ```

4. Run the demo client:
   ```bash
   docker-compose --profile client up grpc-client
   ```

## Services

### etcd
- **Port**: 2379 (client), 2380 (peer)
- **Data**: Persisted in Docker volume `etcd_data`
- **Health Check**: Automatic health checking with retry logic

### grpc-service
- **Port**: 50051
- **Dependencies**: Waits for etcd to be healthy before starting
- **Environment**: Configured to connect to etcd container
- **Health Check**: Built-in gRPC health check endpoint

### grpc-client (Optional)
- **Profile**: `client` - only runs when explicitly requested
- **Purpose**: Demonstrates all gRPC service functionality
- **Dependencies**: Waits for grpc-service to be healthy

## gRPC Service Features

The example implements a comprehensive User Management service with:

### Unary RPCs
- `CreateUser` - Create a new user
- `GetUser` - Retrieve user by ID  
- `UpdateUser` - Update existing user
- `DeleteUser` - Delete user by ID
- `ListUsers` - List users with pagination

### Streaming RPCs
- `StreamUsers` - Server streaming: stream users in batches
- `SubscribeToUserEvents` - Client streaming: batch create users
- `ChatWithUsers` - Bidirectional streaming: real-time user creation

### Built-in Health Checks
- Standard gRPC health check protocol
- Custom health check utility
- Integration with Docker health checks

## Usage

### Access the gRPC Service

The service runs on `localhost:50051` and supports:

- **Health Check**: Use `grpc_health_v1.Health/Check`
- **All User Operations**: Full CRUD + streaming operations
- **Maintenance Mode**: Automatic request blocking during maintenance

### Control Maintenance Mode

You can control the maintenance mode using etcdctl from within the etcd container:

1. **Enable maintenance mode**:
   ```bash
   docker exec etcd etcdctl put /maintenance/grpc-service true
   ```

2. **Disable maintenance mode**:
   ```bash
   docker exec etcd etcdctl put /maintenance/grpc-service false
   ```

3. **Check current value**:
   ```bash
   docker exec etcd etcdctl get /maintenance/grpc-service
   ```

### Monitor Logs

- **All services**:
  ```bash
  docker-compose logs -f
  ```

- **gRPC service only**:
  ```bash
  docker-compose logs -f grpc-service
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

2. Test normal operation with the client:
   ```bash
   docker-compose --profile client up grpc-client
   ```

3. Enable maintenance mode:
   ```bash
   docker exec etcd etcdctl put /maintenance/grpc-service true
   ```

4. Run client again (should get maintenance errors):
   ```bash
   docker-compose --profile client up grpc-client
   ```

5. Health check should still work:
   ```bash
   docker exec grpc-service /app/grpc-healthcheck
   ```

6. Disable maintenance mode:
   ```bash
   docker exec etcd etcdctl put /maintenance/grpc-service false
   ```

7. Verify normal operation restored:
   ```bash
   docker-compose --profile client up grpc-client
   ```

## Local Development

### Generate Protocol Buffers

```bash
cd proto
go generate .
```

### Run Service Locally

```bash
go run main.go
```

### Run Client Locally

```bash
go run client/client.go
```

### Build Health Check Utility

```bash
go build -o grpc-healthcheck ./cmd/healthcheck/main.go
```

## Configuration

### Environment Variables

The grpc-service container uses these environment variables:

- `GRPC_PORT`: Port for gRPC server (default: 50051)
- `ETCD_ENDPOINTS`: Comma-separated etcd endpoints (default: "localhost:2379")
- `ETCD_KEY`: etcd key for maintenance mode (default: "/maintenance/grpc-service")
- `DRAIN_TIMEOUT`: Timeout for request draining (default: "30s")

### etcd Configuration

The etcd service is configured with:
- Single-node cluster
- Data persistence
- Health checks
- Auto-compaction enabled
- 4GB backend quota

## Cleanup

Stop and remove all containers and networks:
```bash
docker-compose down
```

Remove volumes (this will delete etcd data):
```bash
docker-compose down -v
```

## Protocol Buffer Schema

The service uses a comprehensive user management schema:

```protobuf
service UserService {
  // Unary RPCs
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse);
  rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
  
  // Streaming RPCs
  rpc StreamUsers(StreamUsersRequest) returns (stream StreamUsersResponse);
  rpc SubscribeToUserEvents(stream CreateUserRequest) returns (google.protobuf.Empty);
  rpc ChatWithUsers(stream CreateUserRequest) returns (stream CreateUserResponse);
}
```

## Maintenance Mode Behavior

When maintenance mode is enabled:

1. **Health checks continue working** - Service remains "healthy" for orchestrator
2. **New requests are blocked** - Returns `ErrMaintenanceMode` for business operations
3. **Active requests drain gracefully** - Existing requests complete normally
4. **Configurable drain timeout** - Force shutdown after timeout if requests don't complete
5. **Streaming requests handled properly** - Both client and server streams are blocked

## Troubleshooting

### Service won't start
- Check if port 50051 is already in use
- Verify Docker and Docker Compose are installed and running
- Check that etcd is healthy: `docker-compose logs etcd`

### etcd connection issues
- Ensure etcd container is healthy: `docker-compose ps`
- Check etcd logs: `docker-compose logs etcd`
- Verify network connectivity: `docker exec grpc-service ping etcd`

### gRPC service can't connect to etcd
- Verify both containers are on the same network
- Check if etcd is responding: `docker exec etcd etcdctl endpoint health`

### Protocol buffer generation issues
- Ensure protoc is installed: `protoc --version`
- Install Go plugins: `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`
- Verify GOPATH/bin is in PATH: `echo $PATH | grep $(go env GOPATH)/bin`

### Client connection failures
- Verify gRPC service is running: `docker-compose ps grpc-service`
- Check service health: `docker exec grpc-service /app/grpc-healthcheck`
- Review service logs: `docker-compose logs grpc-service`