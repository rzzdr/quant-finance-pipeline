package market

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/internal/kafka"
	"github.com/rzzdr/quant-finance-pipeline/internal/orderbook"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// EnhancedProcessor implements advanced market data processing with order matching
type EnhancedProcessor struct {
	kafkaClient     *kafka.Client
	config          ProcessorConfig
	dataManager     *DataManager
	aggregator      *MarketDataAggregator
	matchingEngines map[string]*orderbook.MatchingEngine
	metricsRecorder MetricsRecorder
	log             *logger.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	mu              sync.RWMutex
	eventProcessor  *OrderBookEventProcessor
}

// OrderBookEventProcessor processes order book events for real-time updates
type OrderBookEventProcessor struct {
	kafkaProducer *kafka.Producer
	log           *logger.Logger
	eventChannels map[string]<-chan orderbook.OrderBookEvent
	mu            sync.RWMutex
}

// NewEnhancedProcessor creates a new enhanced market data processor
func NewEnhancedProcessor(
	kafkaClient *kafka.Client,
	config ProcessorConfig,
	metricsRecorder MetricsRecorder,
) *EnhancedProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	processor := &EnhancedProcessor{
		kafkaClient:     kafkaClient,
		config:          config,
		dataManager:     NewDataManager(),
		aggregator:      NewMarketDataAggregator(),
		matchingEngines: make(map[string]*orderbook.MatchingEngine),
		metricsRecorder: metricsRecorder,
		log:             logger.GetLogger("market.enhanced_processor"),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Initialize event processor
	processor.eventProcessor = &OrderBookEventProcessor{
		log:           logger.GetLogger("market.event_processor"),
		eventChannels: make(map[string]<-chan orderbook.OrderBookEvent),
	}

	return processor
}

// Start starts the enhanced processor
func (p *EnhancedProcessor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.log.Info("Starting enhanced market data processor")

	// Create Kafka producer for order book events
	producer, err := p.kafkaClient.NewProducer("orderbook.events")
	if err != nil {
		return fmt.Errorf("failed to create Kafka producer: %w", err)
	}
	p.eventProcessor.kafkaProducer = producer

	// Start the data manager
	if err := p.dataManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start data manager: %w", err)
	}

	// Start market data consumer
	p.wg.Add(1)
	go p.consumeMarketData(ctx)

	// Start event processing
	p.wg.Add(1)
	go p.eventProcessor.processEvents(ctx)

	p.log.Info("Enhanced market data processor started")
	return nil
}

// Stop stops the enhanced processor
func (p *EnhancedProcessor) Stop() error {
	p.log.Info("Stopping enhanced market data processor")

	p.cancel()
	p.wg.Wait()

	// Close all matching engines
	p.mu.Lock()
	for symbol, engine := range p.matchingEngines {
		engine.Close()
		p.log.Infof("Closed matching engine for %s", symbol)
	}
	p.mu.Unlock()

	// Close event processor
	if p.eventProcessor.kafkaProducer != nil {
		p.eventProcessor.kafkaProducer.Close()
	}

	p.log.Info("Enhanced market data processor stopped")
	return nil
}

// GetMarketData retrieves market data for a symbol
func (p *EnhancedProcessor) GetMarketData(symbol string) (*models.MarketData, error) {
	return p.dataManager.GetMarketData(symbol)
}

// GetOrderBook retrieves order book snapshot for a symbol
func (p *EnhancedProcessor) GetOrderBook(symbol string) (*models.OrderBook, error) {
	p.mu.RLock()
	engine, exists := p.matchingEngines[symbol]
	p.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no order book found for symbol %s", symbol)
	}

	snapshot := engine.GetSnapshot(p.config.OrderBookConfig.MaxLevels)
	return snapshot, nil
}

// PlaceOrder places an order in the order book
func (p *EnhancedProcessor) PlaceOrder(order *models.Order) (interface{}, error) {
	if order == nil {
		return nil, fmt.Errorf("order cannot be nil")
	}

	// Get or create matching engine for the symbol
	engine := p.getOrCreateMatchingEngine(order.Symbol)

	// Place the order
	executions, err := engine.AddOrder(p.ctx, order)
	if err != nil {
		return nil, fmt.Errorf("failed to place order: %w", err)
	}

	// Prepare response
	result := map[string]interface{}{
		"orderId":    order.OrderID,
		"status":     orderStatusToString(order.Status),
		"executions": executions,
		"timestamp":  time.Now(),
	}

	return result, nil
}

// GetOrder retrieves an order by ID
func (p *EnhancedProcessor) GetOrder(orderID string) (*models.Order, error) {
	// Search across all matching engines
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, engine := range p.matchingEngines {
		if order, err := engine.GetOrder(orderID); err == nil {
			return order, nil
		}
	}

	return nil, fmt.Errorf("order %s not found", orderID)
}

// CancelOrder cancels an order
func (p *EnhancedProcessor) CancelOrder(orderID string) error {
	// Search across all matching engines
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, engine := range p.matchingEngines {
		if err := engine.CancelOrder(orderID); err == nil {
			return nil
		}
	}

	return fmt.Errorf("order %s not found", orderID)
}

// ModifyOrder modifies an existing order
func (p *EnhancedProcessor) ModifyOrder(orderID string, newPrice float64, newQuantity float64) (interface{}, error) {
	// Search across all matching engines
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, engine := range p.matchingEngines {
		if _, err := engine.GetOrder(orderID); err == nil {
			executions, err := engine.ModifyOrder(orderID, newPrice, newQuantity)
			if err != nil {
				return nil, err
			}

			result := map[string]interface{}{
				"orderId":     orderID,
				"status":      "modified",
				"executions":  executions,
				"newPrice":    newPrice,
				"newQuantity": newQuantity,
				"timestamp":   time.Now(),
			}

			return result, nil
		}
	}

	return nil, fmt.Errorf("order %s not found", orderID)
}

// GetMarketDepth returns market depth for a symbol
func (p *EnhancedProcessor) GetMarketDepth(symbol string) (*orderbook.MarketDepth, error) {
	p.mu.RLock()
	engine, exists := p.matchingEngines[symbol]
	p.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no order book found for symbol %s", symbol)
	}

	return engine.GetMarketDepth(), nil
}

// getOrCreateMatchingEngine gets or creates a matching engine for a symbol
func (p *EnhancedProcessor) getOrCreateMatchingEngine(symbol string) *orderbook.MatchingEngine {
	p.mu.Lock()
	defer p.mu.Unlock()

	engine, exists := p.matchingEngines[symbol]
	if !exists {
		engine = orderbook.NewMatchingEngine(symbol, p.metricsRecorder)
		p.matchingEngines[symbol] = engine

		// Register event channel with event processor
		p.eventProcessor.mu.Lock()
		p.eventProcessor.eventChannels[symbol] = engine.GetEventChannel()
		p.eventProcessor.mu.Unlock()

		p.log.Infof("Created matching engine for symbol %s", symbol)
	}

	return engine
}

// consumeMarketData consumes market data from Kafka and updates order books
func (p *EnhancedProcessor) consumeMarketData(ctx context.Context) {
	defer p.wg.Done()

	consumer, err := p.kafkaClient.NewConsumer(
		[]string{p.config.KafkaTopic},
		&kafka.ConsumerConfig{
			GroupID:         p.config.KafkaGroupID,
			AutoOffsetReset: "latest",
		},
	)
	if err != nil {
		p.log.Errorf("Failed to create market data consumer: %v", err)
		return
	}
	defer consumer.Close()

	p.log.Info("Started consuming market data")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			message, err := consumer.ConsumeMessage(ctx, 5*time.Second)
			if err != nil {
				if err.Error() != "context deadline exceeded" {
					p.log.Errorf("Error consuming message: %v", err)
				}
				continue
			}

			// Process market data message
			p.processMarketDataMessage(message)
		}
	}
}

// processMarketDataMessage processes a market data message
func (p *EnhancedProcessor) processMarketDataMessage(message *kafka.Message) {
	// Parse market data from message
	marketData, err := p.parseMarketData(message.Value)
	if err != nil {
		p.log.Errorf("Failed to parse market data: %v", err)
		return
	}

	// Update data manager
	p.dataManager.UpdateMarketData(marketData)

	// Update order book if this is depth data
	if marketData.DataType == models.MarketDataTypeDepth {
		p.updateOrderBookFromMarketData(marketData)
	}

	// Record metrics
	latency := time.Since(marketData.Timestamp)
	p.metricsRecorder.RecordMarketDataUpdate(
		marketData.Symbol,
		marketDataTypeToString(marketData.DataType),
		latency,
	)
}

// parseMarketData parses market data from byte array
func (p *EnhancedProcessor) parseMarketData(data []byte) (*models.MarketData, error) {
	// This would typically use Protocol Buffers or JSON unmarshaling
	// For now, return a simple mock implementation
	return &models.MarketData{
		Symbol:    "AAPL",
		Price:     150.0,
		Volume:    1000,
		Timestamp: time.Now(),
		DataType:  models.MarketDataTypeDepth,
	}, nil
}

// updateOrderBookFromMarketData updates order book from market data
func (p *EnhancedProcessor) updateOrderBookFromMarketData(marketData *models.MarketData) {
	// This would extract bid/ask levels from market data and update the order book
	// For now, this is a placeholder
	p.log.Debugf("Processing market data update for %s", marketData.Symbol)
}

// processEvents processes order book events
func (ep *OrderBookEventProcessor) processEvents(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ep.processAllChannels()
		}
	}
}

// processAllChannels processes events from all registered channels
func (ep *OrderBookEventProcessor) processAllChannels() {
	ep.mu.RLock()
	channels := make(map[string]<-chan orderbook.OrderBookEvent)
	for symbol, ch := range ep.eventChannels {
		channels[symbol] = ch
	}
	ep.mu.RUnlock()

	for symbol, ch := range channels {
		ep.processChannelEvents(symbol, ch)
	}
}

// processChannelEvents processes events from a specific channel
func (ep *OrderBookEventProcessor) processChannelEvents(symbol string, ch <-chan orderbook.OrderBookEvent) {
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				// Channel closed
				ep.mu.Lock()
				delete(ep.eventChannels, symbol)
				ep.mu.Unlock()
				return
			}
			ep.handleOrderBookEvent(symbol, event)
		default:
			return // No more events
		}
	}
}

// handleOrderBookEvent handles a single order book event
func (ep *OrderBookEventProcessor) handleOrderBookEvent(symbol string, event orderbook.OrderBookEvent) {
	if ep.kafkaProducer == nil {
		return
	}

	// Convert event to message
	eventData := map[string]interface{}{
		"symbol":    symbol,
		"type":      eventTypeToString(event.Type),
		"timestamp": event.Timestamp,
	}

	if event.Order != nil {
		eventData["order"] = event.Order
	}

	if event.Execution != nil {
		eventData["execution"] = event.Execution
	}

	// Send to Kafka (this would use proper serialization in production)
	err := ep.kafkaProducer.ProduceJSON(context.Background(), []byte(symbol), eventData, nil)
	if err != nil {
		ep.log.Errorf("Failed to send order book event: %v", err)
	}
}

// Helper functions
func orderStatusToString(status models.OrderStatus) string {
	switch status {
	case models.OrderStatusNew:
		return "NEW"
	case models.OrderStatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case models.OrderStatusFilled:
		return "FILLED"
	case models.OrderStatusCanceled:
		return "CANCELED"
	case models.OrderStatusRejected:
		return "REJECTED"
	case models.OrderStatusPending:
		return "PENDING"
	default:
		return "UNKNOWN"
	}
}

func marketDataTypeToString(dataType models.MarketDataType) string {
	switch dataType {
	case models.MarketDataTypeTrade:
		return "TRADE"
	case models.MarketDataTypeQuote:
		return "QUOTE"
	case models.MarketDataTypeDepth:
		return "DEPTH"
	case models.MarketDataTypeOHLC:
		return "OHLC"
	case models.MarketDataTypeIndex:
		return "INDEX"
	default:
		return "UNKNOWN"
	}
}

func eventTypeToString(eventType orderbook.OrderBookEventType) string {
	switch eventType {
	case orderbook.OrderAdded:
		return "ORDER_ADDED"
	case orderbook.OrderCanceled:
		return "ORDER_CANCELED"
	case orderbook.OrderMatched:
		return "ORDER_MATCHED"
	case orderbook.OrderModified:
		return "ORDER_MODIFIED"
	default:
		return "UNKNOWN"
	}
}
