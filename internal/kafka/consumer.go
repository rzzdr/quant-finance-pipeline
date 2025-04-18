package kafka

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// MessageHandler is a function that processes Kafka messages
type MessageHandler func(*Message) error

// Consumer wraps a Kafka consumer with additional functionality
type Consumer struct {
	consumer *kafka.Consumer
	topic    string
	log      *logger.Logger
	wg       sync.WaitGroup
	stopCh   chan struct{}
}

// ConsumeMessage consumes a single message from the topic
func (c *Consumer) ConsumeMessage(ctx context.Context, timeout time.Duration) (*Message, error) {
	// Poll for a message
	ev := c.consumer.Poll(int(timeout.Milliseconds()))
	if ev == nil {
		// No message available within timeout
		return nil, nil
	}

	// Handle different event types
	switch e := ev.(type) {
	case *kafka.Message:
		// Convert message
		message := &Message{
			Key:       e.Key,
			Value:     e.Value,
			Topic:     *e.TopicPartition.Topic,
			Partition: e.TopicPartition.Partition,
			Offset:    int64(e.TopicPartition.Offset),
			Timestamp: e.Timestamp,
		}

		// Extract headers if any
		if len(e.Headers) > 0 {
			message.Headers = make([]MessageHeader, len(e.Headers))
			for i, h := range e.Headers {
				message.Headers[i] = MessageHeader{
					Key:   h.Key,
					Value: h.Value,
				}
			}
		}

		return message, nil

	case kafka.Error:
		// Handle Kafka error
		c.log.Errorf("Kafka error: %v", e)
		return nil, fmt.Errorf("kafka error: %v", e)

	default:
		// Handle other event types (like EOF, partition assignment, etc.)
		c.log.Debugf("Ignored event: %v", e)
		return c.ConsumeMessage(ctx, timeout)
	}
}

// ConsumeMessages starts consuming messages and passes them to the handler function
func (c *Consumer) ConsumeMessages(ctx context.Context, handler MessageHandler) error {
	c.stopCh = make(chan struct{})
	c.wg.Add(1)

	go func() {
		defer c.wg.Done()
		c.log.Info("Starting consumer for topic: %s", c.topic)

		for {
			select {
			case <-ctx.Done():
				c.log.Info("Context cancelled, stopping consumer for topic: %s", c.topic)
				return
			case <-c.stopCh:
				c.log.Info("Consumer stopped for topic: %s", c.topic)
				return
			default:
				// Poll for messages
				ev := c.consumer.Poll(100)
				if ev == nil {
					continue
				}

				switch e := ev.(type) {
				case *kafka.Message:
					// Convert to our Message type
					msg := &Message{
						Key:       e.Key,
						Value:     e.Value,
						Topic:     *e.TopicPartition.Topic,
						Partition: e.TopicPartition.Partition,
						Offset:    int64(e.TopicPartition.Offset),
						Timestamp: e.Timestamp,
					}

					// Add headers if present
					if len(e.Headers) > 0 {
						msg.Headers = make([]MessageHeader, len(e.Headers))
						for i, h := range e.Headers {
							msg.Headers[i] = MessageHeader{
								Key:   h.Key,
								Value: h.Value,
							}
						}
					}

					// Process the message
					if err := handler(msg); err != nil {
						c.log.Error("Error processing message: %v", err)
						continue
					}

					// Commit the offset
					if _, err := c.consumer.CommitMessage(e); err != nil {
						c.log.Error("Error committing offset: %v", err)
					}

				case kafka.Error:
					c.log.Error("Kafka error: %v (%v)", e.Code(), e)
					if e.Code() == kafka.ErrAllBrokersDown {
						return
					}

				default:
					// Ignore other event types
				}
			}
		}
	}()

	return nil
}

// ConsumeBatch consumes a batch of messages up to batchSize or until maxWait time has passed
func (c *Consumer) ConsumeBatch(ctx context.Context, batchSize int, maxWait time.Duration) ([]*Message, error) {
	messages := make([]*Message, 0, batchSize)
	deadline := time.Now().Add(maxWait)

	for len(messages) < batchSize && time.Now().Before(deadline) {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return messages, ctx.Err()
		default:
			// Continue processing
		}

		// Poll for messages with a short timeout
		timeLeft := deadline.Sub(time.Now())
		if timeLeft <= 0 {
			break
		}

		pollTimeout := 100 // ms
		ev := c.consumer.Poll(pollTimeout)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			// Convert to our Message type
			msg := &Message{
				Key:       e.Key,
				Value:     e.Value,
				Topic:     *e.TopicPartition.Topic,
				Partition: e.TopicPartition.Partition,
				Offset:    int64(e.TopicPartition.Offset),
				Timestamp: e.Timestamp,
			}

			// Add headers if present
			if len(e.Headers) > 0 {
				msg.Headers = make([]MessageHeader, len(e.Headers))
				for i, h := range e.Headers {
					msg.Headers[i] = MessageHeader{
						Key:   h.Key,
						Value: h.Value,
					}
				}
			}

			messages = append(messages, msg)
		case kafka.Error:
			c.log.Error("Kafka error: %v (%v)", e.Code(), e)
			if e.Code() == kafka.ErrAllBrokersDown {
				return messages, fmt.Errorf("all brokers down: %v", e)
			}
		}
	}

	return messages, nil
}

// CommitOffset commits the given offset for the topic/partition
func (c *Consumer) CommitOffset(topic string, partition int32, offset int64) error {
	tp := kafka.TopicPartition{
		Topic:     &topic,
		Partition: partition,
		Offset:    kafka.Offset(offset + 1), // commit next offset
	}

	_, err := c.consumer.CommitOffsets([]kafka.TopicPartition{tp})
	return err
}

// Stop stops the consumer gracefully
func (c *Consumer) Stop() error {
	if c.stopCh != nil {
		close(c.stopCh)
	}
	c.wg.Wait()
	return c.consumer.Close()
}

// ConsumeMessagesBatch consumes messages in batches
func (c *Consumer) ConsumeMessagesBatch(ctx context.Context, batchSize int, timeout time.Duration, handler func([]*Message) error) error {
	c.log.Infof("Starting batch consumer for topic: %s, batch size: %d", c.topic, batchSize)

	for {
		select {
		case <-ctx.Done():
			// Context canceled, close consumer
			c.log.Info("Batch consumer context canceled, stopping")
			return ctx.Err()
		default:
			// Consume batch of messages
			batch := make([]*Message, 0, batchSize)
			batchStart := time.Now()

			// Poll for messages until we have a batch or timeout
			for len(batch) < batchSize && time.Since(batchStart) < timeout {
				// Check if context is canceled
				if ctx.Err() != nil {
					return ctx.Err()
				}

				// Try to consume a message
				msg, err := c.ConsumeMessage(ctx, 100*time.Millisecond) // Use shorter timeout for batch polling
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					// Log error and continue
					c.log.Errorf("Error consuming message for batch: %v", err)
					continue
				}

				// If message is available, add to batch
				if msg != nil {
					batch = append(batch, msg)
				}
			}

			// If batch is empty, continue
			if len(batch) == 0 {
				continue
			}

			// Process batch
			if err := handler(batch); err != nil {
				c.log.Errorf("Error handling batch: %v", err)
				// Continue consuming despite error
			}

			// Commit offset of last message in batch if auto-commit is disabled
			if _, err := c.consumer.CommitMessage(messageToKafkaMessage(batch[len(batch)-1])); err != nil {
				c.log.Errorf("Error committing batch offset: %v", err)
			}
		}
	}
}

// Close closes the consumer
func (c *Consumer) Close() error {
	c.log.Info("Closing consumer")
	return c.consumer.Close()
}

// GetTopicPartitions gets the topic partitions
func (c *Consumer) GetTopicPartitions(timeout time.Duration) ([]kafka.TopicPartition, error) {
	metadata, err := c.consumer.GetMetadata(&c.topic, false, int(timeout.Milliseconds()))
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	topic, exists := metadata.Topics[c.topic]
	if !exists {
		return nil, fmt.Errorf("topic %s not found", c.topic)
	}

	partitions := make([]kafka.TopicPartition, len(topic.Partitions))
	for i, partition := range topic.Partitions {
		partitions[i] = kafka.TopicPartition{
			Topic:     &c.topic,
			Partition: int32(partition.ID),
		}
	}

	return partitions, nil
}

// Commit commits the specified offsets
func (c *Consumer) Commit() ([]kafka.TopicPartition, error) {
	return c.consumer.Commit()
}

// CommitMessage commits the specified message's offset
func (c *Consumer) CommitMessage(msg *Message) ([]kafka.TopicPartition, error) {
	return c.consumer.CommitMessage(messageToKafkaMessage(msg))
}

// StoreOffset stores the offset to be committed later
func (c *Consumer) StoreOffset(msg *Message) error {
	return c.consumer.StoreOffsets([]kafka.TopicPartition{
		{
			Topic:     &msg.Topic,
			Partition: msg.Partition,
			Offset:    kafka.Offset(msg.Offset + 1),
		},
	})
}

// Pause pauses consumption from the specified topic partitions
func (c *Consumer) Pause(partitions []kafka.TopicPartition) error {
	return c.consumer.Pause(partitions)
}

// Resume resumes consumption from the specified topic partitions
func (c *Consumer) Resume(partitions []kafka.TopicPartition) error {
	return c.consumer.Resume(partitions)
}

// GetAssignment returns the current topic partition assignments
func (c *Consumer) GetAssignment() ([]kafka.TopicPartition, error) {
	return c.consumer.Assignment()
}

// GetCommitted returns the current committed offsets
func (c *Consumer) GetCommitted(partitions []kafka.TopicPartition, timeout time.Duration) ([]kafka.TopicPartition, error) {
	return c.consumer.Committed(partitions, int(timeout.Milliseconds()))
}

// Seek seeks to the specified offset in the specified partition
func (c *Consumer) Seek(tp kafka.TopicPartition, timeout time.Duration) error {
	return c.consumer.Seek(tp, int(timeout.Milliseconds()))
}

// messageToKafkaMessage converts our Message type back to Kafka's Message type
func messageToKafkaMessage(msg *Message) *kafka.Message {
	// Convert headers if any
	var headers []kafka.Header
	if len(msg.Headers) > 0 {
		headers = make([]kafka.Header, len(msg.Headers))
		for i, h := range msg.Headers {
			headers[i] = kafka.Header{
				Key:   h.Key,
				Value: h.Value,
			}
		}
	}

	return &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &msg.Topic,
			Partition: msg.Partition,
			Offset:    kafka.Offset(msg.Offset),
		},
		Value:     msg.Value,
		Key:       msg.Key,
		Timestamp: msg.Timestamp,
		Headers:   headers,
	}
}
