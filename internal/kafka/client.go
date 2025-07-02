package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Client configuration options
type Config struct {
	BootstrapServers  string
	SecurityProtocol  string
	SaslMechanisms    string
	SaslUsername      string
	SaslPassword      string
	GroupID           string
	AutoOffsetReset   string
	EnableAutoCommit  bool
	SessionTimeout    time.Duration
	DefaultTimeout    time.Duration
	ProducerAcks      string
	ConsumerBatchSize int
}

// ConsumerConfig contains configuration for a Kafka consumer
type ConsumerConfig struct {
	GroupID         string
	AutoOffsetReset string
}

// ProducerConfig contains configuration for a Kafka producer
type ProducerConfig struct {
	BatchSize    int
	BatchTimeout time.Duration
}

// Message represents a Kafka message
type Message struct {
	Key       []byte
	Value     []byte
	Topic     string
	Partition int32
	Offset    int64
	Timestamp time.Time
	Headers   []MessageHeader
}

// MessageHeader represents a Kafka message header
type MessageHeader struct {
	Key   string
	Value []byte
}

// Consumer is defined in consumer.go

// Producer is defined in producer.go

// Client is a wrapper around Kafka clients
type Client struct {
	config *Config
	log    *logger.Logger
}

// NewClient creates a new Kafka client
func NewClient(config *Config) (*Client, error) {
	// Initialize default config if nil
	if config == nil {
		config = DefaultConfig()
	}

	// Create a logger
	log := logger.GetLogger("kafka.client")

	return &Client{
		config: config,
		log:    log,
	}, nil
}

// NewProducer creates a new Kafka producer
func (c *Client) NewProducer(topic string) (*Producer, error) {
	// Create configuration
	config := &kafka.ConfigMap{
		"bootstrap.servers":  c.config.BootstrapServers,
		"acks":               c.config.ProducerAcks,
		"enable.idempotence": true,
	}

	// Add security configuration if specified
	if c.config.SecurityProtocol != "" {
		config.SetKey("security.protocol", c.config.SecurityProtocol)
	}
	if c.config.SaslMechanisms != "" {
		config.SetKey("sasl.mechanisms", c.config.SaslMechanisms)
	}
	if c.config.SaslUsername != "" {
		config.SetKey("sasl.username", c.config.SaslUsername)
	}
	if c.config.SaslPassword != "" {
		config.SetKey("sasl.password", c.config.SaslPassword)
	}

	// Create producer
	producer, err := kafka.NewProducer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create producer: %w", err)
	}

	// Create producer wrapper
	p := &Producer{
		producer:           producer,
		topic:              topic,
		timeout:            c.config.DefaultTimeout,
		deliveryReportChan: nil, // No delivery report channel by default
		log:                c.log,
	}

	// Start delivery report handler
	go p.handleDeliveryReports()

	return p, nil
}

// NewConsumer creates a new Kafka consumer
func (c *Client) NewConsumer(topics []string, consumerConfig *ConsumerConfig) (*Consumer, error) {
	// Create configuration
	config := &kafka.ConfigMap{
		"bootstrap.servers":        c.config.BootstrapServers,
		"group.id":                 consumerConfig.GroupID,
		"session.timeout.ms":       int(c.config.SessionTimeout.Milliseconds()),
		"auto.offset.reset":        consumerConfig.AutoOffsetReset,
		"enable.auto.commit":       c.config.EnableAutoCommit,
		"enable.partition.eof":     false,
		"enable.auto.offset.store": false,
		"go.events.channel.enable": false,
	}

	// Add security configuration if specified
	if c.config.SecurityProtocol != "" {
		config.SetKey("security.protocol", c.config.SecurityProtocol)
	}
	if c.config.SaslMechanisms != "" {
		config.SetKey("sasl.mechanisms", c.config.SaslMechanisms)
	}
	if c.config.SaslUsername != "" {
		config.SetKey("sasl.username", c.config.SaslUsername)
	}
	if c.config.SaslPassword != "" {
		config.SetKey("sasl.password", c.config.SaslPassword)
	}

	// Create consumer
	consumer, err := kafka.NewConsumer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	// Create consumer wrapper
	cons := &Consumer{
		consumer: consumer,
		log:      c.log,
	}

	// Subscribe to topics
	for _, topic := range topics {
		err = consumer.Subscribe(topic, nil)
		if err != nil {
			consumer.Close()
			return nil, fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}
	}

	return cons, nil
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		BootstrapServers:  "localhost:9092",
		SecurityProtocol:  "",
		SaslMechanisms:    "",
		SaslUsername:      "",
		SaslPassword:      "",
		GroupID:           "quant-finance-group",
		AutoOffsetReset:   "earliest",
		EnableAutoCommit:  false,
		SessionTimeout:    30 * time.Second,
		DefaultTimeout:    10 * time.Second,
		ProducerAcks:      "all",
		ConsumerBatchSize: 100,
	}
}

// CreateTopic creates a new Kafka topic
func (c *Client) CreateTopic(ctx context.Context, topic string, partitions int, replicationFactor int) error {
	// We need an AdminClient to create topics
	adminClient, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers": c.config.BootstrapServers,
	})
	if err != nil {
		return fmt.Errorf("failed to create admin client: %w", err)
	}
	defer adminClient.Close()

	// Create the topic specification
	topicSpec := kafka.TopicSpecification{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: replicationFactor,
	}

	// Create the topic
	results, err := adminClient.CreateTopics(ctx, []kafka.TopicSpecification{topicSpec})
	if err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}

	// Check for per-topic errors
	for _, result := range results {
		if result.Error.Code() != kafka.ErrNoError {
			return fmt.Errorf("error creating topic %s: %w", result.Topic, result.Error)
		}
	}

	return nil
}

// DeleteTopic deletes a Kafka topic
func (c *Client) DeleteTopic(ctx context.Context, topic string) error {
	// We need an AdminClient to delete topics
	adminClient, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers": c.config.BootstrapServers,
	})
	if err != nil {
		return fmt.Errorf("failed to create admin client: %w", err)
	}
	defer adminClient.Close()

	// Delete the topic
	results, err := adminClient.DeleteTopics(ctx, []string{topic})
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}

	// Check for per-topic errors
	for _, result := range results {
		if result.Error.Code() != kafka.ErrNoError {
			return fmt.Errorf("error deleting topic %s: %w", result.Topic, result.Error)
		}
	}

	return nil
}

// ListTopics lists all Kafka topics
func (c *Client) ListTopics(ctx context.Context) ([]string, error) {
	// We need an AdminClient to list topics
	adminClient, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers": c.config.BootstrapServers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create admin client: %w", err)
	}
	defer adminClient.Close()

	// List the topics
	metadata, err := adminClient.GetMetadata(nil, true, int(time.Second.Milliseconds()))
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	// Extract topic names
	topics := make([]string, 0, len(metadata.Topics))
	for topic := range metadata.Topics {
		topics = append(topics, topic)
	}

	return topics, nil
}

// EnsureTopicExists ensures a Kafka topic exists
func (c *Client) EnsureTopicExists(ctx context.Context, topic string, partitions int, replicationFactor int) error {
	// List existing topics
	topics, err := c.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("failed to list topics: %w", err)
	}

	// Check if topic already exists
	for _, t := range topics {
		if t == topic {
			c.log.Infof("Topic %s already exists", topic)
			return nil
		}
	}

	// Create the topic if it doesn't exist
	c.log.Infof("Creating topic %s with %d partitions and replication factor %d", topic, partitions, replicationFactor)
	err = c.CreateTopic(ctx, topic, partitions, replicationFactor)
	if err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}

	c.log.Infof("Successfully created topic %s", topic)
	return nil
}

// Close closes the client and releases resources
func (c *Client) Close() error {
	c.log.Info("Closing Kafka client")
	// This is a simple implementation - just logs the action
	// Client doesn't maintain its own connections that need to be closed
	// Consumers and Producers created from the client should be closed separately
	return nil
}
