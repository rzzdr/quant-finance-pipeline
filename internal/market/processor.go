package market

import (
	"context"
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/internal/kafka"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Processor defines the interface for market data processing
type Processor interface {
	GetMarketData(symbol string) (*models.MarketData, error)
	GetOrderBook(symbol string) (*models.OrderBook, error)
	PlaceOrder(order *models.Order) (interface{}, error)
	GetOrder(orderID string) (*models.Order, error)
	CancelOrder(orderID string) error
}

// ProcessorConfig contains configuration for the market processor
type ProcessorConfig struct {
	KafkaTopic      string
	KafkaGroupID    string
	OrderBookConfig OrderBookConfig
}

// OrderBookConfig contains configuration for order books
type OrderBookConfig struct {
	MaxLevels int
	PoolSize  int
}

// MetricsRecorder defines the interface for recording metrics
type MetricsRecorder interface {
	RecordMarketDataUpdate(symbol, dataType string, latency time.Duration)
	RecordOrderBookUpdate(symbol string, side string, size int)
	RecordOrderBookImbalance(symbol string, imbalance float64)
	RecordKafkaLag(topic, groupID string, lag int64)
	RecordOrderProcessed(symbol, orderType, side string)
	RecordOrderFilled(symbol, orderType, side string)
}

// MarketDataProcessor processes market data
type MarketDataProcessor struct {
	kafkaClient     *kafka.Client
	config          ProcessorConfig
	dataManager     *DataManager
	aggregator      *MarketDataAggregator
	bookManager     map[string]*OrderBook
	metricsRecorder MetricsRecorder
	log             *logger.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	mu              sync.RWMutex
}

// MarketDataSubscription represents a subscription to market data
type MarketDataSubscription struct {
	ID           string
	Symbols      []string
	Types        []models.MarketDataType
	Channel      chan *models.MarketData
	ErrorChannel chan error
	Active       bool
}

// MarketDataProvider is an interface for market data sources
type MarketDataProvider interface {
	Connect() error
	Disconnect() error
	Subscribe(symbols []string, types []models.MarketDataType) (string, error)
	Unsubscribe(subscriptionID string) error
}

// MarketDataAggregator aggregates data from multiple providers
type MarketDataAggregator struct {
	Providers           []MarketDataProvider
	ConsolidatedFeed    chan *models.MarketData
	SymbolSubscriptions map[string][]*MarketDataSubscription
	SubscriptionsByID   map[string]*MarketDataSubscription
	mu                  sync.RWMutex
	log                 *logger.Logger
}

// NewProcessor creates a new MarketDataProcessor
func NewProcessor(
	kafkaClient *kafka.Client,
	config ProcessorConfig,
	metricsRecorder MetricsRecorder,
) *MarketDataProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &MarketDataProcessor{
		kafkaClient:     kafkaClient,
		config:          config,
		dataManager:     NewDataManager(),
		aggregator:      NewMarketDataAggregator(),
		bookManager:     make(map[string]*OrderBook),
		metricsRecorder: metricsRecorder,
		log:             logger.GetLogger("market.processor"),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// NewMarketDataAggregator creates a new market data aggregator
func NewMarketDataAggregator() *MarketDataAggregator {
	return &MarketDataAggregator{
		Providers:           make([]MarketDataProvider, 0),
		ConsolidatedFeed:    make(chan *models.MarketData, 10000),
		SymbolSubscriptions: make(map[string][]*MarketDataSubscription),
		SubscriptionsByID:   make(map[string]*MarketDataSubscription),
		log:                 logger.GetLogger("market.aggregator"),
	}
}

// Start starts the market processor
func (p *MarketDataProcessor) Start(ctx context.Context) error {
	p.log.Info("Starting market data processor")

	// Create a consumer for market data
	consumer, err := p.kafkaClient.NewConsumer(
		[]string{p.config.KafkaTopic},
		&kafka.ConsumerConfig{
			GroupID:         p.config.KafkaGroupID,
			AutoOffsetReset: "earliest",
		},
	)

	if err != nil {
		p.log.Errorf("Failed to create Kafka consumer: %v", err)
		return err
	}

	// Start consuming market data
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.consumeMarketData(consumer)
	}()

	// Start the data cleanup routine
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.cleanupStaleData(ctx)
	}()

	return nil
}

// Stop stops the market processor
func (p *MarketDataProcessor) Stop() error {
	p.log.Info("Stopping market data processor")
	p.cancel()
	p.wg.Wait()
	return nil
}

// consumeMarketData consumes market data from Kafka
func (p *MarketDataProcessor) consumeMarketData(consumer *kafka.Consumer) {
	for {
		select {
		case <-p.ctx.Done():
			p.log.Info("Market data consumer stopping")
			return
		default:
			// Consume messages
			msg, err := consumer.ConsumeMessage(p.ctx, 100*time.Millisecond)
			if err != nil {
				if p.ctx.Err() != nil {
					// Context canceled, exit gracefully
					return
				}
				p.log.Errorf("Error consuming message: %v", err)
				continue
			}

			// Deserialize market data
			marketData, err := DeserializeMarketData(msg.Value)
			if err != nil {
				p.log.Errorf("Error deserializing market data: %v", err)
				continue
			}

			// Process the market data
			p.processMarketData(marketData)
		}
	}
}

// processMarketData processes market data
func (p *MarketDataProcessor) processMarketData(data *models.MarketData) {
	if data == nil {
		return
	}

	// Record the time when data was received for latency metrics
	receiveTime := time.Now()
	latency := receiveTime.Sub(data.Timestamp)

	// Record metrics
	p.metricsRecorder.RecordMarketDataUpdate(data.Symbol, MarketDataTypeToString(data.DataType), latency)

	// Update our internal market data store
	p.dataManager.UpdateMarketData(data)

	// Forward the data to the aggregator
	select {
	case p.aggregator.ConsolidatedFeed <- data:
		// Successfully sent to the aggregator
	default:
		// Channel is full, log warning and drop the message
		p.log.Warnf("Aggregator feed channel full, dropping market data for %s", data.Symbol)
	}

	// Distribute the data to subscribers
	p.distributeToSubscribers(data)
}

// distributeToSubscribers sends market data to subscribers
func (p *MarketDataProcessor) distributeToSubscribers(data *models.MarketData) {
	p.aggregator.mu.RLock()
	defer p.aggregator.mu.RUnlock()

	// Find subscribers for this symbol
	subscribers, ok := p.aggregator.SymbolSubscriptions[data.Symbol]
	if !ok {
		return // No subscribers for this symbol
	}

	// Send data to each subscriber
	for _, sub := range subscribers {
		if !sub.Active {
			continue
		}

		// Check if subscriber is interested in this data type
		interested := false
		for _, t := range sub.Types {
			if t == data.DataType {
				interested = true
				break
			}
		}

		if !interested {
			continue
		}

		// Non-blocking send to avoid slow subscribers blocking the processor
		select {
		case sub.Channel <- data.DeepCopy():
			// Successfully sent to subscriber
		default:
			// Subscriber's channel is full, send error
			select {
			case sub.ErrorChannel <- ErrSubscriberChannelFull:
				// Error sent
			default:
				// Error channel is also full, log warning
				p.log.Warnf("Subscriber %s has full channels, dropping data", sub.ID)
			}
		}
	}
}

// Subscribe creates a subscription for market data
func (p *MarketDataProcessor) Subscribe(symbols []string, types []models.MarketDataType, bufferSize int) (*MarketDataSubscription, error) {
	p.aggregator.mu.Lock()
	defer p.aggregator.mu.Unlock()

	// Create a new subscription
	subscription := &MarketDataSubscription{
		ID:           GenerateSubscriptionID(),
		Symbols:      symbols,
		Types:        types,
		Channel:      make(chan *models.MarketData, bufferSize),
		ErrorChannel: make(chan error, 10),
		Active:       true,
	}

	// Register the subscription
	p.aggregator.SubscriptionsByID[subscription.ID] = subscription

	for _, symbol := range symbols {
		if p.aggregator.SymbolSubscriptions[symbol] == nil {
			p.aggregator.SymbolSubscriptions[symbol] = []*MarketDataSubscription{}
		}
		p.aggregator.SymbolSubscriptions[symbol] = append(p.aggregator.SymbolSubscriptions[symbol], subscription)
	}

	return subscription, nil
}

// Unsubscribe cancels a subscription
func (p *MarketDataProcessor) Unsubscribe(subscriptionID string) error {
	p.aggregator.mu.Lock()
	defer p.aggregator.mu.Unlock()

	subscription, ok := p.aggregator.SubscriptionsByID[subscriptionID]
	if !ok {
		return ErrSubscriptionNotFound
	}

	subscription.Active = false

	// Close channels
	close(subscription.Channel)
	close(subscription.ErrorChannel)

	// Remove from aggregator
	delete(p.aggregator.SubscriptionsByID, subscriptionID)

	// Remove from symbol subscriptions
	for _, symbol := range subscription.Symbols {
		subs := p.aggregator.SymbolSubscriptions[symbol]
		for i, sub := range subs {
			if sub.ID == subscriptionID {
				// Remove by replacing with last element and truncating
				lastIdx := len(subs) - 1
				subs[i] = subs[lastIdx]
				subs = subs[:lastIdx]
				p.aggregator.SymbolSubscriptions[symbol] = subs
				break
			}
		}
	}

	return nil
}

// cleanupStaleData periodically removes old data
func (p *MarketDataProcessor) cleanupStaleData(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.dataManager.RemoveOldData(30 * time.Minute) // Remove data older than 30 minutes
		}
	}
}

// AddProvider adds a market data provider to the aggregator
func (p *MarketDataProcessor) AddProvider(provider MarketDataProvider) {
	p.aggregator.mu.Lock()
	defer p.aggregator.mu.Unlock()

	p.aggregator.Providers = append(p.aggregator.Providers, provider)
}

// GetMarketData returns market data for a symbol
func (p *MarketDataProcessor) GetMarketData(symbol string) (*models.MarketData, bool) {
	data, err := p.dataManager.GetMarketData(symbol)
	if err != nil {
		return nil, false
	}
	return data, true
}

// GetOrderBook returns the order book for a symbol
func (p *MarketDataProcessor) GetOrderBook(symbol string) (models.OrderBook, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	book, ok := p.bookManager[symbol]
	if !ok {
		return models.OrderBook{}, false
	}

	return book.GetSnapshot(), true
}

// GenerateSubscriptionID generates a unique ID for subscriptions
func GenerateSubscriptionID() string {
	return "sub_" + time.Now().Format("20060102150405") + "_" + RandomString(6)
}

// RandomString generates a random string of the specified length
func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

// Error types
var (
	ErrSubscriberChannelFull = &SubscriberError{message: "subscriber channel full"}
	ErrSubscriptionNotFound  = &SubscriberError{message: "subscription not found"}
)

// SubscriberError represents an error related to subscribers
type SubscriberError struct {
	message string
}

func (e *SubscriberError) Error() string {
	return e.message
}
