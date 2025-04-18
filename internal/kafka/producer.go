package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Producer is a wrapper around the Kafka producer
type Producer struct {
	producer           *kafka.Producer
	topic              string
	timeout            time.Duration
	deliveryReportChan chan kafka.Event
	log                *logger.Logger
}

// ProduceMessage produces a message to the topic
func (p *Producer) ProduceMessage(ctx context.Context, key []byte, value []byte, headers []MessageHeader) error {
	// Convert headers if any
	var kafkaHeaders []kafka.Header
	if len(headers) > 0 {
		kafkaHeaders = make([]kafka.Header, len(headers))
		for i, h := range headers {
			kafkaHeaders[i] = kafka.Header{
				Key:   h.Key,
				Value: h.Value,
			}
		}
	}

	// Create message
	message := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &p.topic,
			Partition: kafka.PartitionAny, // Use the partitioner to determine the partition
		},
		Value:   value,
		Key:     key,
		Headers: kafkaHeaders,
	}

	// Produce message
	err := p.producer.Produce(message, nil)
	if err != nil {
		p.log.Errorf("Failed to produce message: %v", err)
		return fmt.Errorf("failed to produce message: %w", err)
	}

	return nil
}

// ProduceMessageSync produces a message to the topic and waits for delivery confirmation
func (p *Producer) ProduceMessageSync(ctx context.Context, key []byte, value []byte, headers []MessageHeader) error {
	// Create delivery report channel
	deliveryChan := make(chan kafka.Event, 1)
	defer close(deliveryChan)

	// Convert headers if any
	var kafkaHeaders []kafka.Header
	if len(headers) > 0 {
		kafkaHeaders = make([]kafka.Header, len(headers))
		for i, h := range headers {
			kafkaHeaders[i] = kafka.Header{
				Key:   h.Key,
				Value: h.Value,
			}
		}
	}

	// Create message
	message := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &p.topic,
			Partition: kafka.PartitionAny, // Use the partitioner to determine the partition
		},
		Value:   value,
		Key:     key,
		Headers: kafkaHeaders,
	}

	// Produce message with delivery channel
	err := p.producer.Produce(message, deliveryChan)
	if err != nil {
		p.log.Errorf("Failed to produce message: %v", err)
		return fmt.Errorf("failed to produce message: %w", err)
	}

	// Wait for delivery report or timeout
	select {
	case e := <-deliveryChan:
		// Check delivery status
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				p.log.Errorf("Failed to deliver message: %v", ev.TopicPartition.Error)
				return fmt.Errorf("failed to deliver message: %w", ev.TopicPartition.Error)
			}
			p.log.Debugf("Message delivered to topic %s partition %d at offset %d",
				*ev.TopicPartition.Topic, ev.TopicPartition.Partition, ev.TopicPartition.Offset)
			return nil
		default:
			p.log.Errorf("Unexpected delivery report event type: %T", e)
			return fmt.Errorf("unexpected delivery report event type: %T", e)
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.timeout):
		return fmt.Errorf("timeout waiting for delivery confirmation")
	}
}

// ProduceJSON produces a JSON-serialized message to the topic
func (p *Producer) ProduceJSON(ctx context.Context, key []byte, value interface{}, headers []MessageHeader) error {
	// Serialize value to JSON
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to serialize message to JSON: %w", err)
	}

	// Add content-type header
	contentTypeHeader := MessageHeader{Key: "content-type", Value: []byte("application/json")}
	allHeaders := append(headers, contentTypeHeader)

	// Produce message
	return p.ProduceMessage(ctx, key, jsonValue, allHeaders)
}

// ProduceJSONSync produces a JSON-serialized message to the topic and waits for delivery confirmation
func (p *Producer) ProduceJSONSync(ctx context.Context, key []byte, value interface{}, headers []MessageHeader) error {
	// Serialize value to JSON
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to serialize message to JSON: %w", err)
	}

	// Add content-type header
	contentTypeHeader := MessageHeader{Key: "content-type", Value: []byte("application/json")}
	allHeaders := append(headers, contentTypeHeader)

	// Produce message
	return p.ProduceMessageSync(ctx, key, jsonValue, allHeaders)
}

// ProduceBatch produces a batch of messages to the topic
func (p *Producer) ProduceBatch(ctx context.Context, messages []*Message) error {
	for _, msg := range messages {
		if err := p.ProduceMessage(ctx, msg.Key, msg.Value, msg.Headers); err != nil {
			return err
		}
	}

	// Flush messages
	remaining := p.producer.Flush(int(p.timeout.Milliseconds()))
	if remaining > 0 {
		return fmt.Errorf("failed to flush all messages, %d messages remaining", remaining)
	}

	return nil
}

// Close closes the producer
func (p *Producer) Close() {
	p.log.Info("Closing producer")

	// Flush any remaining messages
	p.log.Info("Flushing producer")
	remaining := p.producer.Flush(int(p.timeout.Milliseconds()))
	if remaining > 0 {
		p.log.Warnf("Failed to flush all messages, %d messages remaining", remaining)
	}

	// Close producer
	p.producer.Close()
}

// Flush flushes any buffered messages
func (p *Producer) Flush() error {
	p.log.Info("Flushing producer")
	remaining := p.producer.Flush(int(p.timeout.Milliseconds()))
	if remaining > 0 {
		return fmt.Errorf("failed to flush all messages, %d messages remaining", remaining)
	}
	return nil
}

// handleDeliveryReports handles delivery reports from the producer
func (p *Producer) handleDeliveryReports() {
	for e := range p.producer.Events() {
		switch ev := e.(type) {
		case *kafka.Message:
			// If message delivery failed, log the error
			if ev.TopicPartition.Error != nil {
				p.log.Errorf("Failed to deliver message: %v", ev.TopicPartition.Error)
			} else {
				p.log.Debugf("Message delivered to topic %s partition %d at offset %d",
					*ev.TopicPartition.Topic, ev.TopicPartition.Partition, ev.TopicPartition.Offset)
			}
			// Forward the event to the delivery report channel if set
			if p.deliveryReportChan != nil {
				p.deliveryReportChan <- ev
			}
		default:
			p.log.Debugf("Ignored event: %v", ev)
		}
	}
}

// BeginTransaction begins a new transaction
func (p *Producer) BeginTransaction() error {
	return p.producer.BeginTransaction()
}

// CommitTransaction commits the current transaction
func (p *Producer) CommitTransaction(ctx context.Context) error {
	timeout := int(p.timeout.Milliseconds())
	return p.producer.CommitTransaction(timeout)
}

// AbortTransaction aborts the current transaction
func (p *Producer) AbortTransaction(ctx context.Context) error {
	timeout := int(p.timeout.Milliseconds())
	return p.producer.AbortTransaction(timeout)
}

// SendOffsetsToTransaction sends the consumer offsets to the transaction
func (p *Producer) SendOffsetsToTransaction(ctx context.Context, offsets []kafka.TopicPartition, consumer *kafka.Consumer) error {
	timeout := int(p.timeout.Milliseconds())
	return p.producer.SendOffsetsToTransaction(offsets, consumer, timeout)
}
