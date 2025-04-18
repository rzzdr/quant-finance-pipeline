package orderbook

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Book represents a limit order book for a single instrument
type Book struct {
	Symbol     string
	BidLevels  *PriceLevels      // Red-black tree for bid levels (buy orders)
	AskLevels  *PriceLevels      // Red-black tree for ask levels (sell orders)
	OrderMap   map[string]*Order // For fast order lookup by ID
	orderPool  *OrderPool
	lastTrade  *Trade
	lastUpdate time.Time
	mutex      sync.RWMutex
	log        *logger.Logger
	snapshots  *list.List // Circular buffer of snapshots
	maxLevels  int        // Maximum levels to keep in depth
}

// Trade represents a trade in the order book
type Trade struct {
	ID          string
	Price       float64
	Quantity    float64
	Timestamp   time.Time
	TakerSide   Side
	BuyOrderID  string
	SellOrderID string
	Symbol      string // Adding symbol field for identification
}

// BookSnapshot represents a snapshot of the order book
type BookSnapshot struct {
	Symbol    string
	Timestamp time.Time
	BidLevels []LevelSnapshot
	AskLevels []LevelSnapshot
	LastTrade *Trade
}

// NewBook creates a new order book
func NewBook(symbol string, maxLevels int, poolSize int) *Book {
	return &Book{
		Symbol:     symbol,
		BidLevels:  NewPriceLevels(false), // Descending order for bids
		AskLevels:  NewPriceLevels(true),  // Ascending order for asks
		OrderMap:   make(map[string]*Order),
		orderPool:  NewOrderPool(poolSize),
		lastUpdate: time.Now(),
		log:        logger.GetLogger("orderbook"),
		snapshots:  list.New(),
		maxLevels:  maxLevels,
	}
}

// AddOrder adds an order to the book
func (b *Book) AddOrder(order *models.Order) (*Order, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Validate the order
	if order.OrderID == "" {
		return nil, fmt.Errorf("order ID cannot be empty")
	}

	if order.Quantity <= 0 {
		return nil, fmt.Errorf("order quantity must be positive")
	}

	if order.Type == models.OrderTypeLimit && order.Price <= 0 {
		return nil, fmt.Errorf("limit order price must be positive")
	}

	// Create a new internal order
	internalOrder := b.orderPool.Get()
	internalOrder.ID = order.OrderID
	internalOrder.Quantity = order.Quantity
	internalOrder.Timestamp = order.CreationTime
	internalOrder.UserID = order.ClientID

	// Convert the side
	if order.Side == models.OrderSideBuy {
		internalOrder.Side = Buy
	} else {
		internalOrder.Side = Sell
	}

	// Convert the order type
	switch order.Type {
	case models.OrderTypeLimit:
		internalOrder.OrderType = Limit
		internalOrder.Price = order.Price
	case models.OrderTypeMarket:
		internalOrder.OrderType = Market
		// For market orders, use the best price on the opposite side
		if internalOrder.Side == Buy {
			bestAsk := b.GetBestAsk()
			if bestAsk == nil {
				b.orderPool.Put(internalOrder)
				return nil, fmt.Errorf("no ask orders available for market buy")
			}
			internalOrder.Price = bestAsk.Price
		} else {
			bestBid := b.GetBestBid()
			if bestBid == nil {
				b.orderPool.Put(internalOrder)
				return nil, fmt.Errorf("no bid orders available for market sell")
			}
			internalOrder.Price = bestBid.Price
		}
	default:
		b.orderPool.Put(internalOrder)
		return nil, fmt.Errorf("unsupported order type: %v", order.Type)
	}

	// Check for matching orders (for market and limit orders)
	trades, remaining := b.matchOrder(internalOrder)

	// If we have trades, process them
	if len(trades) > 0 {
		for _, trade := range trades {
			b.lastTrade = trade
			b.log.Infof("Trade executed: %s, Price: %.2f, Quantity: %.2f", trade.ID, trade.Price, trade.Quantity)
		}
	}

	// If we have a remaining order quantity, add it to the book (only for limit orders)
	if remaining > 0 && internalOrder.OrderType == Limit {
		internalOrder.Quantity = remaining
		b.addToBook(internalOrder)
		b.OrderMap[internalOrder.ID] = internalOrder
		b.log.Debugf("Added order to book: %s, Side: %v, Price: %.2f, Quantity: %.2f",
			internalOrder.ID, internalOrder.Side, internalOrder.Price, internalOrder.Quantity)
	} else if internalOrder.OrderType == Limit {
		// For fully executed limit orders, return to pool
		b.orderPool.Put(internalOrder)
	}

	b.lastUpdate = time.Now()

	// Create a snapshot periodically (every 100 orders)
	if len(b.OrderMap)%100 == 0 {
		b.TakeSnapshot()
	}

	return internalOrder, nil
}

// CancelOrder cancels an order in the book
func (b *Book) CancelOrder(orderID string) (*Order, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Find the order
	order, exists := b.OrderMap[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	// Remove from the price level
	var levels *PriceLevels
	if order.Side == Buy {
		levels = b.BidLevels
	} else {
		levels = b.AskLevels
	}

	level := levels.Find(order.Price)
	if level == nil {
		return nil, fmt.Errorf("price level not found for order: %s", orderID)
	}

	success := level.RemoveOrder(order)
	if !success {
		return nil, fmt.Errorf("failed to remove order from level: %s", orderID)
	}

	// If the level is now empty, remove it
	if level.IsEmpty() {
		levels.Remove(order.Price)
	}

	// Remove from the order map
	delete(b.OrderMap, orderID)

	// Log and update timestamp
	b.log.Debugf("Canceled order: %s", orderID)
	b.lastUpdate = time.Now()

	// Make a copy to return
	result := order.Clone()

	// Return the order to the pool
	b.orderPool.Put(order)

	return result, nil
}

// ModifyOrder modifies an existing order in the book
func (b *Book) ModifyOrder(orderID string, newQuantity float64, newPrice float64) (*Order, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Find the order
	order, exists := b.OrderMap[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	// If price is unchanged, just update the quantity
	if newPrice == order.Price {
		// Update the price level
		var levels *PriceLevels
		if order.Side == Buy {
			levels = b.BidLevels
		} else {
			levels = b.AskLevels
		}

		level := levels.Find(order.Price)
		if level == nil {
			return nil, fmt.Errorf("price level not found for order: %s", orderID)
		}

		success := level.UpdateOrder(order, newQuantity)
		if !success {
			return nil, fmt.Errorf("failed to update order: %s", orderID)
		}

		b.log.Debugf("Modified order quantity: %s, new quantity: %.2f", orderID, newQuantity)
		b.lastUpdate = time.Now()

		return order.Clone(), nil
	}

	// If price changed, treat as cancel and new order
	_, err := b.CancelOrder(orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel order for modification: %w", err)
	}

	// Create a new model order
	newOrder := &models.Order{
		OrderID:      uuid.New().String(),
		Symbol:       b.Symbol,
		Quantity:     newQuantity,
		Price:        newPrice,
		CreationTime: time.Now(),
		ClientID:     order.UserID,
	}

	// Set the side
	if order.Side == Buy {
		newOrder.Side = models.OrderSideBuy
	} else {
		newOrder.Side = models.OrderSideSell
	}

	// Set the order type
	if order.OrderType == Limit {
		newOrder.Type = models.OrderTypeLimit
	} else {
		newOrder.Type = models.OrderTypeMarket
	}

	// Add the new order
	newInternalOrder, err := b.AddOrder(newOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to add modified order: %w", err)
	}

	return newInternalOrder, nil
}

// GetBestBid returns the best bid level
func (b *Book) GetBestBid() *Level {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.BidLevels.IsEmpty() {
		return nil
	}

	return b.BidLevels.FindMax()
}

// GetBestAsk returns the best ask level
func (b *Book) GetBestAsk() *Level {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.AskLevels.IsEmpty() {
		return nil
	}

	return b.AskLevels.FindMin()
}

// GetMidPrice returns the mid price (average of best bid and ask)
func (b *Book) GetMidPrice() (float64, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	bestBid := b.BidLevels.FindMax()
	bestAsk := b.AskLevels.FindMin()

	if bestBid == nil || bestAsk == nil {
		return 0, fmt.Errorf("cannot calculate mid price, missing bid or ask")
	}

	return (bestBid.Price + bestAsk.Price) / 2, nil
}

// Spread returns the bid-ask spread
func (b *Book) Spread() (float64, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	bestBid := b.BidLevels.FindMax()
	bestAsk := b.AskLevels.FindMin()

	if bestBid == nil || bestAsk == nil {
		return 0, fmt.Errorf("cannot calculate spread, missing bid or ask")
	}

	return bestAsk.Price - bestBid.Price, nil
}

// Depth returns the order book depth
func (b *Book) Depth(levels int) ([]LevelSnapshot, []LevelSnapshot) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	bids := b.BidLevels.TopN(levels)
	asks := b.AskLevels.TopN(levels)

	bidSnapshots := make([]LevelSnapshot, len(bids))
	askSnapshots := make([]LevelSnapshot, len(asks))

	for i, level := range bids {
		bidSnapshots[i] = level.Snapshot()
	}

	for i, level := range asks {
		askSnapshots[i] = level.Snapshot()
	}

	return bidSnapshots, askSnapshots
}

// GetLastTrade returns the last trade executed in the order book
func (b *Book) GetLastTrade() *Trade {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.lastTrade == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	tradeCopy := *b.lastTrade
	return &tradeCopy
}

// matchOrder matches an incoming order against the order book
// Returns a list of trades executed and the remaining quantity
func (b *Book) matchOrder(order *Order) ([]*Trade, float64) {
	trades := make([]*Trade, 0)
	remainingQty := order.Quantity

	var matchSide Side
	var priceLevels *PriceLevels

	// Determine which side to match against
	if order.Side == Buy {
		matchSide = Sell
		priceLevels = b.AskLevels
	} else {
		matchSide = Buy
		priceLevels = b.BidLevels
	}

	// For market orders or if the price is acceptable
	for remainingQty > 0 {
		var bestLevel *Level

		// Get the best price level to match against
		if order.Side == Buy {
			bestLevel = b.AskLevels.FindMin() // Buy orders match against lowest ask
		} else {
			bestLevel = b.BidLevels.FindMax() // Sell orders match against highest bid
		}

		// No more orders to match against
		if bestLevel == nil {
			break
		}

		// For limit orders, check if the price is acceptable
		if order.OrderType == Limit {
			if (order.Side == Buy && bestLevel.Price > order.Price) ||
				(order.Side == Sell && bestLevel.Price < order.Price) {
				// Price not acceptable, stop matching
				break
			}
		}

		// Match against orders at this level
		levelTrades, levelRemaining := b.matchAgainstLevel(order, bestLevel, remainingQty, matchSide)
		trades = append(trades, levelTrades...)
		remainingQty = levelRemaining

		// If the level is now empty, remove it
		if bestLevel.IsEmpty() {
			priceLevels.Remove(bestLevel.Price)
		}

		// For market orders, if we can't match everything, we're done
		if order.OrderType == Market && remainingQty > 0 {
			b.log.Debugf("Market order %s partially filled: %f/%f", order.ID, order.Quantity-remainingQty, order.Quantity)
			remainingQty = 0 // No remaining quantity for market orders
			break
		}
	}

	return trades, remainingQty
}

// matchAgainstLevel matches an order against orders at a given price level
func (b *Book) matchAgainstLevel(order *Order, level *Level, quantity float64, matchSide Side) ([]*Trade, float64) {
	level.mu.Lock()
	defer level.mu.Unlock()

	trades := make([]*Trade, 0)
	remainingQty := quantity

	// Get a reference to the first order at this level
	currentOrder := level.Orders

	// Match against orders while we still have quantity to fill
	for currentOrder != nil && remainingQty > 0 {
		// Calculate the matched quantity
		matchedQty := min(remainingQty, currentOrder.Quantity)

		// Create a trade
		trade := &Trade{
			ID:        uuid.New().String(),
			Price:     level.Price,
			Quantity:  matchedQty,
			Timestamp: time.Now(),
			TakerSide: order.Side,
			Symbol:    b.Symbol,
		}

		// Set the buy/sell order IDs based on side
		if order.Side == Buy {
			trade.BuyOrderID = order.ID
			trade.SellOrderID = currentOrder.ID
		} else {
			trade.BuyOrderID = currentOrder.ID
			trade.SellOrderID = order.ID
		}

		trades = append(trades, trade)

		// Update the level and remaining quantities
		remainingQty -= matchedQty
		level.TotalQty -= matchedQty

		// Update the matched order's quantity
		currentOrder.Quantity -= matchedQty

		// Get the next order before potentially removing the current one
		nextOrder := currentOrder.Next

		// If the matched order is now fully executed, remove it
		if currentOrder.Quantity <= 0 {
			delete(b.OrderMap, currentOrder.ID)
			level.RemoveOrder(currentOrder)
			level.OrderCount--
			b.orderPool.Put(currentOrder)
		}

		currentOrder = nextOrder
	}

	return trades, remainingQty
}

// addToBook adds an order to the appropriate side of the book
func (b *Book) addToBook(order *Order) {
	var priceLevels *PriceLevels

	// Determine which side to add to
	if order.Side == Buy {
		priceLevels = b.BidLevels
	} else {
		priceLevels = b.AskLevels
	}

	// Find or create the price level
	level := priceLevels.Find(order.Price)
	if level == nil {
		level = NewLevel(order.Price)
		priceLevels.Insert(order.Price, level)
	}

	// Add the order to the level
	level.AddOrder(order)
}

// TakeSnapshot creates a snapshot of the current order book state
func (b *Book) TakeSnapshot() *BookSnapshot {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	// Get top N levels from each side
	bidLevels, askLevels := b.Depth(b.maxLevels)

	snapshot := &BookSnapshot{
		Symbol:    b.Symbol,
		Timestamp: time.Now(),
		BidLevels: bidLevels,
		AskLevels: askLevels,
	}

	// Copy the last trade if it exists
	if b.lastTrade != nil {
		tradeCopy := *b.lastTrade
		snapshot.LastTrade = &tradeCopy
	}

	// Store the snapshot in the circular buffer
	if b.snapshots.Len() >= 100 { // Keep only the last 100 snapshots
		b.snapshots.Remove(b.snapshots.Front())
	}
	b.snapshots.PushBack(snapshot)

	return snapshot
}

// GetSnapshot returns the most recent snapshot of the order book
func (b *Book) GetSnapshot() *BookSnapshot {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.snapshots.Len() == 0 {
		return b.TakeSnapshot()
	}

	return b.snapshots.Back().Value.(*BookSnapshot)
}

// GetVolume returns the total volume at the specified price level and side
func (b *Book) GetVolume(price float64, side Side) float64 {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var priceLevels *PriceLevels
	if side == Buy {
		priceLevels = b.BidLevels
	} else {
		priceLevels = b.AskLevels
	}

	level := priceLevels.Find(price)
	if level == nil {
		return 0
	}

	return level.TotalQty
}

// GetOrder returns an order by its ID
func (b *Book) GetOrder(orderID string) (*Order, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	order, exists := b.OrderMap[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	return order.Clone(), nil
}

// GetOrderCount returns the total number of orders in the book
func (b *Book) GetOrderCount() int {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	return len(b.OrderMap)
}

// GetTotalBidVolume returns the total volume on the bid side
func (b *Book) GetTotalBidVolume() float64 {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var total float64
	b.BidLevels.Traverse(func(level *Level) bool {
		total += level.TotalQty
		return true
	})

	return total
}

// GetTotalAskVolume returns the total volume on the ask side
func (b *Book) GetTotalAskVolume() float64 {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var total float64
	b.AskLevels.Traverse(func(level *Level) bool {
		total += level.TotalQty
		return true
	})

	return total
}

// ToOrderBook converts the internal book to a models.OrderBook
func (b *Book) ToOrderBook() *models.OrderBook {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	bidLevels, askLevels := b.Depth(b.maxLevels)

	bids := make([]models.OrderBookLevel, len(bidLevels))
	for i, level := range bidLevels {
		bids[i] = models.OrderBookLevel{
			Price:      level.Price,
			Quantity:   level.TotalQty,
			OrderCount: int32(level.OrderCount),
		}
	}

	asks := make([]models.OrderBookLevel, len(askLevels))
	for i, level := range askLevels {
		asks[i] = models.OrderBookLevel{
			Price:      level.Price,
			Quantity:   level.TotalQty,
			OrderCount: int32(level.OrderCount),
		}
	}

	return &models.OrderBook{
		Symbol:    b.Symbol,
		Timestamp: time.Now(),
		Bids:      bids,
		Asks:      asks,
	}
}

// Clear removes all orders from the book
func (b *Book) Clear() {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Return all orders to the pool
	for _, order := range b.OrderMap {
		b.orderPool.Put(order)
	}

	// Create new empty trees
	b.BidLevels = NewPriceLevels(false)
	b.AskLevels = NewPriceLevels(true)
	b.OrderMap = make(map[string]*Order)
	b.lastTrade = nil
	b.lastUpdate = time.Now()
}

// helper function to get minimum of two values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
