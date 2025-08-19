package market

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/errors"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// DataProcessor processes market data
type DataProcessor struct {
	aggregator  *MarketDataAggregator
	bookManager *BookManager
	pool        *models.MarketDataPool
	log         *logger.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// BookManager manages order books for instruments
type BookManager struct {
	books     map[string]*OrderBook
	mu        sync.RWMutex
	bookCache *sync.Map
	log       *logger.Logger
}

// OrderBook represents a consolidated order book from various sources
type OrderBook struct {
	Symbol     string
	Timestamp  time.Time
	BidLevels  []models.PriceLevel
	AskLevels  []models.PriceLevel
	LastUpdate time.Time
	Sources    map[string]time.Time
	mu         sync.RWMutex
}

// NewDataProcessor creates a new DataProcessor
func NewDataProcessor(aggregator *MarketDataAggregator) *DataProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	return &DataProcessor{
		aggregator:  aggregator,
		bookManager: NewBookManager(),
		pool:        models.NewMarketDataPool(),
		log:         logger.GetLogger("market.processor"),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// NewBookManager creates a new BookManager
func NewBookManager() *BookManager {
	return &BookManager{
		books:     make(map[string]*OrderBook),
		bookCache: &sync.Map{},
		log:       logger.GetLogger("market.books"),
	}
}

// Start starts the data processor
func (p *DataProcessor) Start() {
	p.wg.Add(1)
	go p.processData()
}

// Stop stops the data processor
func (p *DataProcessor) Stop() {
	p.cancel()
	p.wg.Wait()
}

// processData processes market data from the aggregator
func (p *DataProcessor) processData() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			p.log.Info("Data processor stopping")
			return
		case data := <-p.aggregator.ConsolidatedFeed:
			p.processMarketData(data)
		}
	}
}

// processMarketData processes a market data update
func (p *DataProcessor) processMarketData(data *models.MarketData) {
	if data == nil {
		return
	}

	// Update order book if data contains depth information
	if data.DataType == models.MarketDataTypeDepth || data.DataType == models.MarketDataTypeQuote {
		p.bookManager.UpdateBook(data.Symbol, data.BidLevels, data.AskLevels, data.Timestamp, data.Exchange)
	}

	// Here you would implement additional processing logic,
	// such as calculating derived metrics, applying filters, etc.

	// Return the market data to the pool when done processing
	p.pool.Put(data)
}

// Subscribe creates a new market data subscription
func (p *DataProcessor) Subscribe(symbols []string, types []models.MarketDataType, bufferSize int) (*MarketDataSubscription, error) {
	if len(symbols) == 0 {
		return nil, errors.InvalidArgument("at least one symbol must be specified")
	}

	if len(types) == 0 {
		return nil, errors.InvalidArgument("at least one data type must be specified")
	}

	subscription := &MarketDataSubscription{
		ID:           uuid.New().String(),
		Symbols:      symbols,
		Types:        types,
		Channel:      make(chan *models.MarketData, bufferSize),
		ErrorChannel: make(chan error, 10),
		Active:       true,
	}

	// Register the subscription
	for _, provider := range p.aggregator.Providers {
		_, err := provider.Subscribe(symbols, types)
		if err != nil {
			return nil, errors.Wrap(err, "failed to subscribe to market data")
		}
	}

	// Store the subscription
	for _, symbol := range symbols {
		if p.aggregator.SymbolSubscriptions[symbol] == nil {
			p.aggregator.SymbolSubscriptions[symbol] = []*MarketDataSubscription{}
		}
		p.aggregator.SymbolSubscriptions[symbol] = append(p.aggregator.SymbolSubscriptions[symbol], subscription)
	}

	p.aggregator.SubscriptionsByID[subscription.ID] = subscription

	return subscription, nil
}

// Unsubscribe cancels a market data subscription
func (p *DataProcessor) Unsubscribe(subscriptionID string) error {
	subscription, ok := p.aggregator.SubscriptionsByID[subscriptionID]
	if !ok {
		return errors.NotFound("subscription not found")
	}

	subscription.Active = false
	close(subscription.Channel)
	close(subscription.ErrorChannel)

	// Remove from subscription maps
	delete(p.aggregator.SubscriptionsByID, subscriptionID)

	for _, symbol := range subscription.Symbols {
		subs := p.aggregator.SymbolSubscriptions[symbol]
		for i, sub := range subs {
			if sub.ID == subscriptionID {
				// Remove subscription from slice
				p.aggregator.SymbolSubscriptions[symbol] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}

	return nil
}

// GetOrderBook returns the current order book for a symbol
func (b *BookManager) GetOrderBook(symbol string) (*OrderBook, error) {
	b.mu.RLock()
	book, ok := b.books[symbol]
	b.mu.RUnlock()

	if !ok {
		return nil, errors.NotFound("order book not found for symbol: " + symbol)
	}

	return book, nil
}

// UpdateBook updates the order book for a symbol
func (b *BookManager) UpdateBook(symbol string, bidLevels, askLevels []models.PriceLevel, timestamp time.Time, source string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	book, ok := b.books[symbol]
	if !ok {
		book = &OrderBook{
			Symbol:    symbol,
			Timestamp: timestamp,
			BidLevels: make([]models.PriceLevel, 0, len(bidLevels)),
			AskLevels: make([]models.PriceLevel, 0, len(askLevels)),
			Sources:   make(map[string]time.Time),
		}
		b.books[symbol] = book
	}

	// Update the order book
	book.mu.Lock()
	defer book.mu.Unlock()

	book.LastUpdate = time.Now()
	book.Timestamp = timestamp
	book.Sources[source] = timestamp

	// Replace or merge bid levels based on your consolidation strategy
	// Here we're doing a simple replace - in a real system you might merge quotes from different sources
	if len(bidLevels) > 0 {
		book.BidLevels = make([]models.PriceLevel, len(bidLevels))
		copy(book.BidLevels, bidLevels)
	}

	// Replace or merge ask levels
	if len(askLevels) > 0 {
		book.AskLevels = make([]models.PriceLevel, len(askLevels))
		copy(book.AskLevels, askLevels)
	}

	// Cache the latest version
	b.bookCache.Store(symbol, book)
}

// GetSnapshot returns a snapshot of the order book for a symbol
func (b *OrderBook) GetSnapshot() models.OrderBook {
	b.mu.RLock()
	defer b.mu.RUnlock()

	snapshot := models.OrderBook{
		Symbol:    b.Symbol,
		Timestamp: b.Timestamp,
		Bids:      make([]models.OrderBookLevel, len(b.BidLevels)),
		Asks:      make([]models.OrderBookLevel, len(b.AskLevels)),
	}

	// Convert bid levels to order book levels
	for i, level := range b.BidLevels {
		snapshot.Bids[i] = models.OrderBookLevel{
			Price:      level.Price,
			Quantity:   level.Quantity,
			OrderCount: level.OrderCount,
		}
	}

	// Convert ask levels to order book levels
	for i, level := range b.AskLevels {
		snapshot.Asks[i] = models.OrderBookLevel{
			Price:      level.Price,
			Quantity:   level.Quantity,
			OrderCount: level.OrderCount,
		}
	}

	return snapshot
}

// DataManager manages market data for the system
type DataManager struct {
	marketData     map[string]*models.MarketData
	derivativeInfo map[string]*models.DerivativeInfo
	undilyingMap   map[string][]string // Map of underlying -> derivatives
	mu             sync.RWMutex
	log            *logger.Logger
	pool           *models.MarketDataPool
}

// NewDataManager creates a new market data manager
func NewDataManager() *DataManager {
	return &DataManager{
		marketData:     make(map[string]*models.MarketData),
		derivativeInfo: make(map[string]*models.DerivativeInfo),
		undilyingMap:   make(map[string][]string),
		log:            logger.GetLogger("market.data"),
		pool:           models.NewMarketDataPool(),
	}
}

// UpdateMarketData updates the market data for a symbol
func (m *DataManager) UpdateMarketData(data *models.MarketData) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store reference to the existing data if any
	existingData, exists := m.marketData[data.Symbol]

	// Make a deep copy of the input data
	if exists {
		// Update existing data
		existingData.Price = data.Price
		existingData.Volume = data.Volume
		existingData.Timestamp = data.Timestamp
		existingData.Exchange = data.Exchange
		existingData.DataType = data.DataType

		// Clear and update price levels if provided
		if len(data.BidLevels) > 0 {
			existingData.BidLevels = existingData.BidLevels[:0]
			for _, level := range data.BidLevels {
				existingData.BidLevels = append(existingData.BidLevels, level)
			}
		}

		if len(data.AskLevels) > 0 {
			existingData.AskLevels = existingData.AskLevels[:0]
			for _, level := range data.AskLevels {
				existingData.AskLevels = append(existingData.AskLevels, level)
			}
		}

		// Update derivative info if available
		if data.DerivativeInfo != nil {
			if existingData.DerivativeInfo == nil {
				existingData.DerivativeInfo = &models.DerivativeInfo{
					Type:              data.DerivativeInfo.Type,
					UnderlyingSymbol:  data.DerivativeInfo.UnderlyingSymbol,
					StrikePrice:       data.DerivativeInfo.StrikePrice,
					ExpiryDate:        data.DerivativeInfo.ExpiryDate,
					OptionType:        data.DerivativeInfo.OptionType,
					ImpliedVolatility: data.DerivativeInfo.ImpliedVolatility,
					OpenInterest:      data.DerivativeInfo.OpenInterest,
					Delta:             data.DerivativeInfo.Delta,
					Gamma:             data.DerivativeInfo.Gamma,
					Theta:             data.DerivativeInfo.Theta,
					Vega:              data.DerivativeInfo.Vega,
					Rho:               data.DerivativeInfo.Rho,
				}

				// Update the underlying mapping
				if data.DerivativeInfo.UnderlyingSymbol != "" {
					m.addToUnderlyingMap(data.DerivativeInfo.UnderlyingSymbol, data.Symbol)
				}
			} else {
				// Update existing derivative info
				existingData.DerivativeInfo.ImpliedVolatility = data.DerivativeInfo.ImpliedVolatility
				existingData.DerivativeInfo.OpenInterest = data.DerivativeInfo.OpenInterest
				existingData.DerivativeInfo.Delta = data.DerivativeInfo.Delta
				existingData.DerivativeInfo.Gamma = data.DerivativeInfo.Gamma
				existingData.DerivativeInfo.Theta = data.DerivativeInfo.Theta
				existingData.DerivativeInfo.Vega = data.DerivativeInfo.Vega
				existingData.DerivativeInfo.Rho = data.DerivativeInfo.Rho
			}
		}
	} else {
		// Create a new entry
		newData := data.DeepCopy()
		m.marketData[data.Symbol] = newData

		// If it's a derivative, update the mapping
		if data.DerivativeInfo != nil && data.DerivativeInfo.UnderlyingSymbol != "" {
			m.addToUnderlyingMap(data.DerivativeInfo.UnderlyingSymbol, data.Symbol)
		}
	}
}

// Start initializes the data manager
func (m *DataManager) Start(ctx context.Context) error {
	m.log.Info("Data manager started")
	return nil
}

// GetMarketData retrieves market data for a symbol
func (m *DataManager) GetMarketData(symbol string) (*models.MarketData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.marketData[symbol]
	if !exists {
		return nil, fmt.Errorf("market data not found for symbol: %s", symbol)
	}
	return data.DeepCopy(), nil
}

// GetAllMarketData retrieves all market data
func (m *DataManager) GetAllMarketData() map[string]*models.MarketData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*models.MarketData, len(m.marketData))
	for symbol, data := range m.marketData {
		result[symbol] = data.DeepCopy()
	}

	return result
}

// GetDerivatives returns all derivatives for an underlying
func (m *DataManager) GetDerivatives(underlying string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	derivatives, exists := m.undilyingMap[underlying]
	if !exists {
		return []string{}
	}

	result := make([]string, len(derivatives))
	copy(result, derivatives)
	return result
}

// addToUnderlyingMap adds a derivative to the underlying mapping
func (m *DataManager) addToUnderlyingMap(underlying, derivative string) {
	derivatives, exists := m.undilyingMap[underlying]
	if !exists {
		m.undilyingMap[underlying] = []string{derivative}
		return
	}

	// Check if it's already in the list
	for _, d := range derivatives {
		if d == derivative {
			return
		}
	}

	// Add to the list
	m.undilyingMap[underlying] = append(derivatives, derivative)
}

// RemoveOldData removes data that hasn't been updated for a while
func (m *DataManager) RemoveOldData(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for symbol, data := range m.marketData {
		if now.Sub(data.Timestamp) > maxAge {
			m.log.Infof("Removing stale market data for %s, last update: %v", symbol, data.Timestamp)
			delete(m.marketData, symbol)

			// Remove from underlying map if it's a derivative
			if data.DerivativeInfo != nil && data.DerivativeInfo.UnderlyingSymbol != "" {
				m.removeFromUnderlyingMap(data.DerivativeInfo.UnderlyingSymbol, symbol)
			}
		}
	}
}

// removeFromUnderlyingMap removes a derivative from the underlying mapping
func (m *DataManager) removeFromUnderlyingMap(underlying, derivative string) {
	derivatives, exists := m.undilyingMap[underlying]
	if !exists {
		return
	}

	// Find and remove the derivative
	for i, d := range derivatives {
		if d == derivative {
			// Remove by replacing with last element and truncating
			derivatives[i] = derivatives[len(derivatives)-1]
			m.undilyingMap[underlying] = derivatives[:len(derivatives)-1]
			return
		}
	}
}

// SerializeMarketData serializes market data to JSON
func SerializeMarketData(data *models.MarketData) ([]byte, error) {
	return json.Marshal(data)
}

// DeserializeMarketData deserializes market data from JSON
func DeserializeMarketData(data []byte) (*models.MarketData, error) {
	var marketData models.MarketData
	err := json.Unmarshal(data, &marketData)
	if err != nil {
		return nil, err
	}
	return &marketData, nil
}

// GetSpreadBPS calculates the spread in basis points for a symbol
func (m *DataManager) GetSpreadBPS(symbol string) (float64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.marketData[symbol]
	if !exists || len(data.BidLevels) == 0 || len(data.AskLevels) == 0 {
		return 0, false
	}

	bestBid := data.BidLevels[0].Price
	bestAsk := data.AskLevels[0].Price
	midPrice := (bestBid + bestAsk) / 2

	if midPrice <= 0 {
		return 0, false
	}

	return (bestAsk - bestBid) / midPrice * 10000, true // Multiply by 10000 for basis points
}
