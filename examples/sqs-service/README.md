# SQS Service with Maintenance Mode

This example demonstrates how to use the GoMaint library with Amazon SQS (Simple Queue Service) for managing maintenance mode in a message processing service. It showcases graceful message draining during maintenance windows while ensuring no messages are lost.

## Features

- **SQS Message Processing**: Concurrent processing of SQS messages with configurable timeouts
- **Maintenance Mode**: Graceful draining of in-flight messages during maintenance
- **LocalStack Integration**: Complete local development setup using LocalStack
- **Health Checks**: SQS connectivity and message processing monitoring
- **Message Statistics**: Detailed metrics on message processing performance
- **HTTP API**: REST endpoints for service management and monitoring

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  SQS Service    │    │   LocalStack    │    │      etcd       │
│                 │    │      (SQS)      │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   HTTP API  │ │    │ │ test-queue  │ │    │ │ Maintenance │ │
│ │             │ │    │ │             │ │    │ │    State    │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │                 │    │                 │
│ │ SQS Handler │ │◄──►│                 │    │                 │
│ │(Drain Mode) │ │    │                 │    │                 │
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
   curl http://localhost:8082/health
   ```

3. **View queue information**:
   ```bash
   curl http://localhost:8082/queue/info
   ```

4. **Send a test message**:
   ```bash
   curl -X POST http://localhost:8082/messages/send \
     -H "Content-Type: application/json" \
     -d '{"message": "Hello from SQS!"}'
   ```

5. **Check message processing statistics**:
   ```bash
   curl http://localhost:8082/messages/stats
   ```

6. **Enable maintenance mode**:
   ```bash
   docker-compose exec sqs-etcd etcdctl put /maintenance/sqs-service true
   ```

7. **Disable maintenance mode**:
   ```bash
   docker-compose exec sqs-etcd etcdctl put /maintenance/sqs-service false
   ```

### Manual Setup

1. **Start LocalStack**:
   ```bash
   docker run -d --name localstack \
     -e SERVICES=sqs \
     -e DEFAULT_REGION=us-east-1 \
     -p 4566:4566 \
     localstack/localstack:3.8
   ```

2. **Start etcd**:
   ```bash
   docker run -d --name sqs-etcd \
     -p 2399:2379 \
     -p 2400:2380 \
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

3. **Create SQS queue**:
   ```bash
   aws --endpoint-url=http://localhost:4566 \
       --region=us-east-1 \
       sqs create-queue --queue-name test-queue
   ```

4. **Run the service**:
   ```bash
   export SQS_QUEUE_URL="http://localhost:4566/000000000000/test-queue"
   export AWS_ENDPOINT="http://localhost:4566"
   export ETCD_ENDPOINTS="localhost:2399"
   go run main.go
   ```

## API Endpoints

### Health Check
- **GET** `/health` - Service health status and maintenance mode

### Message Management
- **POST** `/messages/send` - Send a test message to the queue
- **GET** `/messages/stats` - Message processing statistics

### Queue Information
- **GET** `/queue/info` - SQS queue attributes and status

## Maintenance Mode Behavior

### Normal Operation
- Continuously polls SQS for messages (configurable batch size)
- Processes messages concurrently with simulated work
- Deletes successfully processed messages from queue
- Tracks processing statistics and errors

### Maintenance Mode Activation
1. **Stop New Message Reception**: Handler stops polling SQS for new messages
2. **Drain In-Flight Messages**: Waits for currently processing messages to complete
3. **Timeout Handling**: If drain timeout is exceeded, returns error with count of remaining messages
4. **Graceful Degradation**: HTTP API returns 503 for non-health endpoints

### Maintenance Mode Deactivation
1. **Resume Message Processing**: Handler resumes SQS polling
2. **Normal Operation**: All endpoints return to normal functionality

## Configuration

Environment variables:

- `SQS_QUEUE_URL`: SQS queue URL (default: `http://localstack:4566/000000000000/test-queue`)
- `AWS_ENDPOINT`: AWS endpoint URL (default: `http://localstack:4566`)
- `AWS_REGION`: AWS region (default: `us-east-1`)
- `ETCD_ENDPOINTS`: etcd endpoints (default: `etcd:2379`)

### Port Configuration

To avoid conflicts with other examples:

- **HTTP Service**: Port `8082`
- **LocalStack**: Port `4566` (standard)
- **etcd**: Ports `2399/2400`
- **etcd Key**: `/maintenance/sqs-service`

## SQS Handler Configuration

The SQS handler supports the following configuration options:

```go
type Config struct {
    QueueURL                string        // SQS queue URL
    MaxNumberOfMessages     int32         // Messages per batch (1-10)
    WaitTimeSeconds         int32         // Long polling timeout (0-20)
    VisibilityTimeoutSeconds int32        // Message visibility timeout
    DrainTimeout            time.Duration // Max time to wait for drain
    PollInterval            time.Duration // Polling interval
}
```

Default configuration provides:
- **Batch Size**: 10 messages per request
- **Long Polling**: 20 seconds wait time
- **Visibility Timeout**: 30 seconds
- **Drain Timeout**: 30 seconds
- **Poll Interval**: 1 second

## Message Processing

### Processing Flow
1. **Receive Messages**: Batch receive from SQS (up to 10 messages)
2. **Concurrent Processing**: Each message processed in separate goroutine
3. **Simulated Work**: Random processing time (1-3 seconds)
4. **Success Handling**: Delete message from queue
5. **Error Handling**: Message becomes visible again after timeout

### Statistics Tracking
- **Messages Received**: Total messages received from SQS
- **Messages Processed**: Successfully processed and deleted messages
- **Messages Failed**: Processing failures (with error details)
- **Messages In-Flight**: Currently being processed
- **Processing Errors**: Last 10 error messages with timestamps

## Testing Maintenance Mode

### 1. Start the Service
```bash
docker-compose up -d
curl http://localhost:8082/health
```

### 2. Generate Test Messages
```bash
# Send multiple messages to create processing backlog
for i in {1..20}; do
  curl -X POST http://localhost:8082/messages/send \
    -H "Content-Type: application/json" \
    -d "{\"message\": \"Test message $i\"}"
  sleep 0.1
done
```

### 3. Monitor Message Processing
```bash
# Watch message statistics
watch -n 1 "curl -s http://localhost:8082/messages/stats | jq .sqs"
```

### 4. Enable Maintenance Mode
```bash
# Trigger maintenance mode
docker-compose exec sqs-etcd etcdctl put /maintenance/sqs-service true

# Check drain behavior
curl http://localhost:8082/health
```

### 5. Observe Drain Behavior
- Service stops receiving new messages
- In-flight messages continue processing
- Statistics show declining in-flight count
- Health endpoint shows maintenance status

### 6. Disable Maintenance Mode
```bash
docker-compose exec sqs-etcd etcdctl put /maintenance/sqs-service false
```

## LocalStack Integration

This example uses LocalStack to simulate AWS SQS locally:

### Features Used
- **SQS Queue Creation**: Automatic queue initialization via init container
- **Message Operations**: Send, receive, delete messages
- **Queue Attributes**: Visibility timeout, retention period
- **Health Checks**: Queue connectivity verification

### LocalStack Configuration
The setup uses LocalStack 3.0 with minimal configuration to avoid service conflicts:
- **Services**: Only SQS enabled (no S3 dependencies)
- **Initialization**: SQS service creates queues automatically on startup
- **Health Checks**: Extended timeouts to allow proper startup
- **Initial Messages**: Service sends test messages after queue creation

### Manual SQS Testing with curl
```bash
# Send message via HTTP API
docker-compose exec queue-debug curl -X POST \
  'http://localstack:4566/000000000000/test-queue' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'Action=SendMessage&MessageBody=Manual+test+message&Version=2012-11-05'

# Receive messages
docker-compose exec queue-debug curl -X POST \
  'http://localstack:4566/000000000000/test-queue' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'Action=ReceiveMessage&Version=2012-11-05'

# Alternative: Use the service HTTP API
curl -X POST http://localhost:8082/messages/send \
  -H "Content-Type: application/json" \
  -d '{"message": "Test message via service"}'
```

## Production Considerations

### AWS Deployment
1. **IAM Permissions**: Configure proper SQS permissions
2. **VPC Configuration**: Ensure network access to SQS
3. **Error Handling**: Implement dead letter queues
4. **Monitoring**: Add CloudWatch metrics integration
5. **Scaling**: Consider horizontal scaling for high throughput

### Performance Tuning
1. **Batch Size**: Adjust based on message size and processing time
2. **Concurrency**: Tune goroutine limits for optimal throughput
3. **Visibility Timeout**: Match to maximum processing time
4. **Drain Timeout**: Allow sufficient time for message processing

### Error Handling
1. **Retry Logic**: Implement exponential backoff
2. **Dead Letter Queues**: Handle permanently failed messages
3. **Alerting**: Monitor processing failures and timeouts
4. **Circuit Breaker**: Prevent cascade failures

## Cleanup

```bash
# Stop and remove containers
docker-compose down -v

# Remove images (optional)
docker-compose down --rmi all -v
```

## Comparison with Other Handlers

| Feature | HTTP Handler | Database Handler | SQS Handler |
|---------|-------------|------------------|-------------|
| **Resource Type** | HTTP requests | DB connections | SQS messages |
| **Maintenance Behavior** | Reject new requests | Reduce connections | Stop polling, drain |
| **Drain Mechanism** | Wait for requests | Connection pool | Message completion |
| **Health Check** | Server status | DB ping | SQS connectivity |
| **Statistics** | Request counts | Connection stats | Message metrics |

The SQS handler provides unique message-oriented maintenance capabilities, ensuring no message loss during maintenance windows while providing detailed processing metrics.

## Dependencies

This example uses:

- **AWS SDK Go v2**: `github.com/aws/aws-sdk-go-v2` - AWS SDK for SQS operations
- **LocalStack**: Local AWS service simulation
- **GoMaint**: Maintenance mode management library
- **etcd**: Coordination and configuration management