# Kafka Service with Maintenance Mode

This example demonstrates how to use the GoMaint library with Apache Kafka for managing maintenance mode in a message streaming service. It showcases graceful message processing draining during maintenance windows while ensuring no messages are lost.

## Features

- **Kafka Message Processing**: Consumer group-based message processing with configurable concurrency
- **Maintenance Mode**: Graceful draining of in-flight messages during maintenance
- **Multi-Topic Support**: Process messages from multiple Kafka topics simultaneously
- **Producer Integration**: HTTP API for publishing test messages to Kafka topics
- **KRaft Mode**: Modern Kafka setup without Zookeeper dependency
- **Kafka UI**: Web-based interface for topic and partition monitoring
- **Health Checks**: Kafka broker connectivity and message processing monitoring
- **Message Statistics**: Detailed metrics on message processing performance
- **HTTP API**: REST endpoints for service management and monitoring

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ Kafka Service   │    │     Kafka       │    │      etcd       │
│                 │    │   (KRaft Mode)  │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   HTTP API  │ │    │ │ test-topic  │ │    │ │ Maintenance │ │
│ │             │ │    │ │user-events  │ │    │ │    State    │ │
│ └─────────────┘ │    │ │system-logs  │ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │ └─────────────┘ │    │                 │
│ │Kafka Handler│ │◄──►│                 │    │                 │
│ │(Drain Mode) │ │    │ Consumer Groups │    │                 │
│ └─────────────┘ │    │ Partitions      │    │                 │
│ ┌─────────────┐ │    └─────────────────┘    └─────────────────┘
│ │  Manager    │ │                                     ▲
│ │             │ │◄────────────────────────────────────┘
│ └─────────────┘ │
└─────────────────┘

┌─────────────────┐
│    Kafka UI     │
│  (Port 8084)    │    ← Web Interface for Kafka Management
│ Topic Monitoring│
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
   curl http://localhost:8083/health
   ```

3. **View Kafka topics**:
   ```bash
   curl http://localhost:8083/topics/info
   ```

4. **Send a test message**:
   ```bash
   curl -X POST http://localhost:8083/messages/send \
     -H "Content-Type: application/json" \
     -d '{"topic": "test-topic", "message": "Hello from Kafka!", "key": "test-key"}'
   ```

5. **Check message processing statistics**:
   ```bash
   curl http://localhost:8083/messages/stats
   ```

6. **Access Kafka UI**:
   Open http://localhost:8084 in your browser for visual topic/partition management

7. **Enable maintenance mode**:
   ```bash
   docker exec kafka-etcd etcdctl put /maintenance/kafka-service true
   ```

8. **Disable maintenance mode**:
   ```bash
   docker exec kafka-etcd etcdctl put /maintenance/kafka-service false
   ```

### Manual Setup

1. **Start Kafka with KRaft**:
   ```bash
   docker run -d --name kafka \
     -p 9092:9092 -p 29092:29092 \
     -e KAFKA_NODE_ID=1 \
     -e KAFKA_PROCESS_ROLES=broker,controller \
     -e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT \
     -e KAFKA_LISTENERS=PLAINTEXT://0.0.0.0:29092,CONTROLLER://0.0.0.0:29093,PLAINTEXT_HOST://0.0.0.0:9092 \
     -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:29092,PLAINTEXT_HOST://localhost:9092 \
     -e KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER \
     -e KAFKA_CONTROLLER_QUORUM_VOTERS=1@localhost:29093 \
     -e KAFKA_AUTO_CREATE_TOPICS_ENABLE=true \
     confluentinc/cp-kafka:7.6.0
   ```

2. **Start etcd**:
   ```bash
   docker run -d --name kafka-etcd \
     -p 2401:2379 -p 2402:2380 \
     quay.io/coreos/etcd:v3.5.9 \
     /usr/local/bin/etcd \
     --name=etcd0 --data-dir=/etcd-data \
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
   export KAFKA_BROKERS="localhost:9092"
   export KAFKA_TOPICS="test-topic,user-events,system-logs"
   export KAFKA_CONSUMER_GROUP="gomaint-consumer-group"
   export ETCD_ENDPOINTS="localhost:2401"
   go run main.go
   ```

## API Endpoints

### Health Check
- **GET** `/health` - Service health status and maintenance mode

### Message Management
- **POST** `/messages/send` - Send messages to Kafka topics
- **GET** `/messages/stats` - Message processing statistics

### Topic Information
- **GET** `/topics/info` - Kafka topic metadata and partition information

## Maintenance Mode Behavior

### Normal Operation
- Consumes messages from configured topics using consumer groups
- Processes messages concurrently with configurable worker pool
- Commits offsets after successful processing (manual commit mode)
- Tracks processing statistics and errors
- Supports multiple topics simultaneously

### Maintenance Mode Activation
1. **Stop New Message Reception**: Consumer leaves the consumer group
2. **Drain In-Flight Messages**: Waits for currently processing messages to complete
3. **Commit Offsets**: Ensures processed messages are committed before shutdown
4. **Timeout Handling**: If drain timeout is exceeded, returns error with count of remaining messages
5. **Graceful Degradation**: HTTP API returns 503 for non-health endpoints

### Maintenance Mode Deactivation
1. **Rejoin Consumer Group**: Consumer rejoins and gets partition assignments
2. **Resume Message Processing**: Continues from last committed offsets
3. **Normal Operation**: All endpoints return to normal functionality

## Configuration

Environment variables:

- `KAFKA_BROKERS`: Comma-separated Kafka broker addresses (default: `kafka:29092`)
- `KAFKA_TOPICS`: Comma-separated topic names (default: `test-topic,user-events,system-logs`)
- `KAFKA_CONSUMER_GROUP`: Consumer group ID (default: `gomaint-consumer-group`)
- `ETCD_ENDPOINTS`: etcd endpoints (default: `etcd:2379`)

### Port Configuration

To avoid conflicts with other examples:

- **HTTP Service**: Port `8083`
- **Kafka Broker**: Port `9092` (external), `29092` (internal)
- **Kafka UI**: Port `8084`
- **etcd**: Ports `2401/2402`
- **etcd Key**: `/maintenance/kafka-service`

## Kafka Handler Configuration

The Kafka handler supports the following configuration options:

```go
type Config struct {
    Brokers           []string      // Kafka broker addresses
    Topics            []string      // Topics to consume from
    ConsumerGroup     string        // Consumer group ID
    BatchSize         int           // Message batch size
    SessionTimeout    time.Duration // Consumer session timeout
    HeartbeatInterval time.Duration // Heartbeat interval
    DrainTimeout      time.Duration // Max time to wait for drain
    MaxProcessingTime time.Duration // Max processing time per message
    OffsetInitial     int64         // Initial offset (oldest/newest)
    EnableAutoCommit  bool          // Auto-commit offsets
    AutoCommitInterval time.Duration // Auto-commit interval
    RetryBackoff      time.Duration // Retry backoff duration
    MaxWorkers        int           // Maximum concurrent workers
}
```

Default configuration provides:
- **Consumer Group**: `gomaint-consumer-group`
- **Session Timeout**: 30 seconds
- **Heartbeat Interval**: 3 seconds
- **Drain Timeout**: 45 seconds
- **Max Workers**: 5 concurrent processors
- **Initial Offset**: Oldest unread messages
- **Auto Commit**: Disabled (manual commit after processing)

## Message Processing

### Processing Flow
1. **Join Consumer Group**: Handler joins consumer group and gets partition assignments
2. **Consume Messages**: Batch consume from assigned partitions
3. **Concurrent Processing**: Each message processed in separate goroutine (worker pool)
4. **Manual Offset Commit**: Commit offsets only after successful processing
5. **Error Handling**: Failed messages remain uncommitted for retry
6. **Rebalancing**: Graceful handling of consumer group rebalancing

### Statistics Tracking
- **Messages Received**: Total messages consumed from Kafka
- **Messages Processed**: Successfully processed and committed messages
- **Messages Failed**: Processing failures (with error details)
- **Messages In-Flight**: Currently being processed
- **Consumer Lag**: Lag per topic/partition
- **Processing Errors**: Last 10 error messages with timestamps

### Topic Management
- **Multi-Topic Support**: Process messages from multiple topics
- **Auto Topic Creation**: Topics created automatically if enabled in Kafka
- **Partition Metadata**: Detailed partition information per topic
- **Consumer Group Monitoring**: Track consumer group membership and lag

## Testing Maintenance Mode

### 1. Start the Service
```bash
docker-compose up -d
curl http://localhost:8083/health
```

### 2. Generate Test Messages
```bash
# Send messages to different topics
curl -X POST http://localhost:8083/messages/send \
  -H "Content-Type: application/json" \
  -d '{"topic": "test-topic", "message": "Test message 1"}'

curl -X POST http://localhost:8083/messages/send \
  -H "Content-Type: application/json" \
  -d '{"topic": "user-events", "message": "{\"event\": \"user_login\", \"user_id\": \"user123\"}"}'

curl -X POST http://localhost:8083/messages/send \
  -H "Content-Type: application/json" \
  -d '{"topic": "system-logs", "message": "{\"level\": \"info\", \"message\": \"System status check\"}"}'
```

### 3. Monitor Message Processing
```bash
# Watch message statistics
watch -n 1 "curl -s http://localhost:8083/messages/stats | jq .kafka"

# Check Kafka UI
open http://localhost:8084
```

### 4. Enable Maintenance Mode
```bash
# Trigger maintenance mode
docker exec kafka-etcd etcdctl put /maintenance/kafka-service true

# Check drain behavior
curl http://localhost:8083/health
```

### 5. Observe Drain Behavior
- Consumer leaves consumer group
- In-flight messages continue processing
- Statistics show declining in-flight count
- Health endpoint shows maintenance status
- Other consumers in group get partition reassignments

### 6. Disable Maintenance Mode
```bash
docker exec kafka-etcd etcdctl put /maintenance/kafka-service false
```

## Kafka Integration

This example uses Apache Kafka in KRaft mode with modern features:

### Features Used
- **Consumer Groups**: Automatic partition assignment and rebalancing
- **Manual Offset Commits**: Commit only after successful processing
- **Multiple Topics**: Process messages from different topics simultaneously
- **Producer Integration**: HTTP API for message publishing
- **Topic Auto-Creation**: Automatic topic creation for development
- **Health Monitoring**: Broker connectivity and consumer group health

### Kafka Configuration
The setup uses Kafka 7.6.0 with KRaft mode (no Zookeeper):
- **Broker Role**: Combined broker and controller in single node
- **Auto Topic Creation**: Enabled for development convenience
- **Replication Factor**: 1 (single node setup)
- **Default Partitions**: Kafka default (typically 1)
- **Retention**: 168 hours (7 days)

### Message Format
Messages include headers for tracking:
- `source`: Message origin (api, init, etc.)
- `timestamp`: Message creation time
- Custom headers supported via API

### Manual Kafka Testing with CLI
```bash
# List topics
docker exec kafka-broker kafka-topics --bootstrap-server localhost:9092 --list

# Create topic manually
docker exec kafka-broker kafka-topics --bootstrap-server localhost:9092 --create --topic new-topic

# Produce messages
docker exec -it kafka-broker kafka-console-producer --bootstrap-server localhost:9092 --topic test-topic

# Consume messages
docker exec -it kafka-broker kafka-console-consumer --bootstrap-server localhost:9092 --topic test-topic --from-beginning

# List consumer groups
docker exec kafka-broker kafka-consumer-groups --bootstrap-server localhost:9092 --list

# Describe consumer group
docker exec kafka-broker kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group gomaint-consumer-group
```

## Production Considerations

### Kafka Deployment
1. **Multi-Broker Setup**: Deploy multiple Kafka brokers for high availability
2. **Replication Factor**: Set appropriate replication factor (typically 3)
3. **Partition Strategy**: Design partition count based on parallelism needs
4. **Security**: Configure SASL/SSL for authentication and encryption
5. **Monitoring**: Integrate with Kafka metrics and monitoring solutions

### Performance Tuning
1. **Consumer Configuration**: Tune fetch sizes and session timeouts
2. **Worker Pool Size**: Adjust based on processing complexity and resources
3. **Batch Processing**: Consider batch processing for higher throughput
4. **Offset Management**: Choose appropriate offset commit strategy
5. **Producer Settings**: Configure producer for reliability vs performance

### Error Handling
1. **Retry Logic**: Implement exponential backoff for transient failures
2. **Dead Letter Topics**: Route permanently failed messages to DLQ
3. **Poison Messages**: Handle malformed or corrupt messages gracefully
4. **Consumer Lag Monitoring**: Alert on high consumer lag
5. **Circuit Breakers**: Prevent cascade failures during outages

### Scaling Considerations
1. **Horizontal Scaling**: Add more consumer instances for higher throughput
2. **Partition Count**: Increase partitions for better parallelism
3. **Consumer Group Strategy**: Use appropriate partition assignment strategy
4. **Resource Limits**: Set appropriate CPU and memory limits
5. **Network Optimization**: Configure network settings for low latency

## Kafka UI Features

Access the web interface at http://localhost:8084:

- **Topic Management**: Create, delete, and configure topics
- **Message Browser**: View messages in topics with filtering
- **Consumer Groups**: Monitor consumer group status and lag
- **Broker Information**: View cluster and broker details
- **Configuration**: Manage topic and broker configurations
- **Performance Metrics**: View throughput and latency metrics

## Cleanup

```bash
# Stop and remove containers
docker-compose down -v

# Remove images (optional)
docker-compose down --rmi all -v

# Clean up Kafka data
docker volume rm kafka-service_kafka_data kafka-service_kafka_etcd_data
```

## Comparison with Other Handlers

| Feature | HTTP Handler | Database Handler | SQS Handler | Kafka Handler |
|---------|-------------|------------------|-------------|---------------|
| **Resource Type** | HTTP requests | DB connections | SQS messages | Kafka messages |
| **Maintenance Behavior** | Reject new requests | Reduce connections | Stop polling, drain | Leave consumer group, drain |
| **Drain Mechanism** | Wait for requests | Connection pool | Message completion | Consumer group rebalancing |
| **Health Check** | Server status | DB ping | SQS connectivity | Broker connectivity |
| **Statistics** | Request counts | Connection stats | Message metrics | Consumer group metrics |
| **Scalability** | Load balancer | Connection pool | Multiple consumers | Consumer groups |
| **Ordering** | N/A | N/A | FIFO queues | Partition ordering |
| **Persistence** | Stateless | Database | Queue retention | Log-based storage |

The Kafka handler provides unique streaming capabilities with consumer group coordination, ensuring scalable message processing with partition-based ordering and at-least-once delivery guarantees.

## Dependencies

This example uses:

- **IBM Sarama**: `github.com/IBM/sarama` - Go client library for Apache Kafka
- **Confluent Kafka**: Apache Kafka distribution with KRaft mode
- **Kafka UI**: Web-based management interface for Kafka
- **GoMaint**: Maintenance mode management library
- **etcd**: Coordination and configuration management

## Troubleshooting

### Common Issues

1. **Consumer Group Rebalancing**: Increase session timeout if frequent rebalancing occurs
2. **Message Processing Timeout**: Adjust max processing time for long-running tasks
3. **Offset Commit Failures**: Check network connectivity and broker health
4. **Topic Not Found**: Verify topic names and auto-creation settings
5. **Consumer Lag**: Monitor partition assignment and processing capacity

### Debug Commands

```bash
# Check service logs
docker-compose logs kafka-service

# Check Kafka broker logs
docker-compose logs kafka

# Verify consumer group status
docker exec kafka-broker kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group gomaint-consumer-group

# Check topic partitions
docker exec kafka-broker kafka-topics --bootstrap-server localhost:9092 --describe --topic test-topic
```