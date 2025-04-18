package orderbook

import (
	"time"

	"github.com/google/uuid"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// MatchResult represents the result of an order match
type MatchResult struct {
	Trades         []*Trade
	RemainingOrder *Order
	FullyMatched   bool
}

// OrderMatcher handles the matching of orders in the order book
type OrderMatcher struct {
	book *Book
	log  *logger.Logger
}

// NewOrderMatcher creates a new order matcher
func NewOrderMatcher(book *Book) *OrderMatcher {
	return &OrderMatcher{
		book: book,
		log:  logger.GetLogger("orderbook.matcher"),
	}
}

// MatchOrder matches an incoming order against the order book
func (m *OrderMatcher) MatchOrder(order *Order) ([]*Trade, *Order) {
	if order.OrderType == Market {
		return m.matchMarketOrder(order)
	} else {
		return m.matchLimitOrder(order)
	}
}

// matchMarketOrder matches a market order against the order book
func (m *OrderMatcher) matchMarketOrder(order *Order) ([]*Trade, *Order) {
	trades := make([]*Trade, 0)
	remainingQty := order.Quantity

	// Determine which side to match against
	var levels *PriceLevels
	if order.Side == Buy {
		levels = m.book.AskLevels
	} else {
		levels = m.book.BidLevels
	}

	// Keep matching until order is filled or no more liquidity
	for remainingQty > 0 {
		var bestLevel *Level

		// Find the best price level
		if order.Side == Buy {
			bestLevel = levels.FindMin() // Lowest ask
		} else {
			bestLevel = levels.FindMax() // Highest bid
		}

		// No more orders to match against
		if bestLevel == nil {
			m.log.Warnf("Market order %s could not be fully executed, %f remaining", order.ID, remainingQty)
			break
		}

		// Execute trades at this level
		levelTrades, levelRemaining := m.matchAgainstLevel(order, bestLevel, remainingQty)
		trades = append(trades, levelTrades...)
		remainingQty = levelRemaining

		// If level is empty, remove it
		if bestLevel.IsEmpty() {
			if order.Side == Buy {
				m.book.AskLevels.Remove(bestLevel.Price)
			} else {
				m.book.BidLevels.Remove(bestLevel.Price)
			}
		}
	}

	// For market orders, any remaining quantity is canceled
	if remainingQty > 0 {
		m.log.Infof("Market order %s partially filled: %f/%f", order.ID, order.Quantity-remainingQty, order.Quantity)
	} else {
		m.log.Infof("Market order %s fully filled: %f", order.ID, order.Quantity)
	}

	// No remaining order for market orders
	return trades, nil
}

// matchLimitOrder matches a limit order against the order book
func (m *OrderMatcher) matchLimitOrder(order *Order) ([]*Trade, *Order) {
	trades := make([]*Trade, 0)
	remainingQty := order.Quantity

	// Determine which side to match against
	var levels *PriceLevels
	if order.Side == Buy {
		levels = m.book.AskLevels
	} else {
		levels = m.book.BidLevels
	}

	// Keep matching while there's quantity and price is acceptable
	for remainingQty > 0 {
		var bestLevel *Level

		// Find the best price level
		if order.Side == Buy {
			bestLevel = levels.FindMin() // Lowest ask
		} else {
			bestLevel = levels.FindMax() // Highest bid
		}

		// No more orders to match against or price not acceptable
		if bestLevel == nil ||
			(order.Side == Buy && bestLevel.Price > order.Price) ||
			(order.Side == Sell && bestLevel.Price < order.Price) {
			break
		}

		// Execute trades at this level
		levelTrades, levelRemaining := m.matchAgainstLevel(order, bestLevel, remainingQty)
		trades = append(trades, levelTrades...)
		remainingQty = levelRemaining

		// If level is empty, remove it
		if bestLevel.IsEmpty() {
			if order.Side == Buy {
				m.book.AskLevels.Remove(bestLevel.Price)
			} else {
				m.book.BidLevels.Remove(bestLevel.Price)
			}
		}

		// If fully filled, no need to continue
		if remainingQty == 0 {
			m.log.Infof("Limit order %s fully filled: %f", order.ID, order.Quantity)
			break
		}
	}

	// If there's remaining quantity, create a remaining order
	var remainingOrder *Order
	if remainingQty > 0 {
		if remainingQty < order.Quantity {
			m.log.Infof("Limit order %s partially filled: %f/%f", order.ID, order.Quantity-remainingQty, order.Quantity)
		}

		remainingOrder = order.Clone()
		remainingOrder.Quantity = remainingQty
	} else {
		m.log.Infof("Limit order %s fully filled: %f", order.ID, order.Quantity)
	}

	return trades, remainingOrder
}

// matchAgainstLevel matches an order against a specific price level
func (m *OrderMatcher) matchAgainstLevel(order *Order, level *Level, quantity float64) ([]*Trade, float64) {
	trades := make([]*Trade, 0)
	remainingQty := quantity

	level.mu.Lock()
	defer level.mu.Unlock()

	// Get a copy of the orders at this level to avoid potential deadlock
	orders := level.GetOrders()

	// Match against each order in the level
	for _, restingOrder := range orders {
		if remainingQty <= 0 {
			break
		}

		// Calculate the matched quantity
		matchedQty := min(remainingQty, restingOrder.Quantity)

		// Create a trade
		trade := &Trade{
			ID:        uuid.New().String(),
			Price:     level.Price,
			Quantity:  matchedQty,
			Timestamp: time.Now(),
			Symbol:    m.book.Symbol,
			TakerSide: order.Side,
		}

		// Set buy/sell order IDs based on order side
		if order.Side == Buy {
			trade.BuyOrderID = order.ID
			trade.SellOrderID = restingOrder.ID
		} else {
			trade.BuyOrderID = restingOrder.ID
			trade.SellOrderID = order.ID
		}

		trades = append(trades, trade)

		// Update remaining quantities
		remainingQty -= matchedQty

		// Update the resting order and level
		level.TotalQty -= matchedQty
		restingOrder.Quantity -= matchedQty

		// If resting order is fully filled, remove it
		if restingOrder.Quantity <= 0 {
			level.RemoveOrder(restingOrder)
			delete(m.book.OrderMap, restingOrder.ID)
			level.OrderCount--
			m.book.orderPool.Put(restingOrder)
		}
	}

	return trades, remainingQty
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// MatchOrders is a utility function that matches two orders directly
// Returns a trade if the orders match, nil otherwise
func MatchOrders(buyOrder, sellOrder *Order) *Trade {
	// Only match if buy price >= sell price
	if buyOrder.Price < sellOrder.Price {
		return nil
	}

	// Calculate the matched quantity
	matchedQty := min(buyOrder.Quantity, sellOrder.Quantity)
	if matchedQty <= 0 {
		return nil
	}

	// Determine the trade price (usually the price of the resting order)
	var tradePrice float64
	if buyOrder.Timestamp.Before(sellOrder.Timestamp) {
		tradePrice = buyOrder.Price
	} else {
		tradePrice = sellOrder.Price
	}

	// Create the trade
	trade := &Trade{
		ID:          uuid.New().String(),
		Price:       tradePrice,
		Quantity:    matchedQty,
		Timestamp:   time.Now(),
		BuyOrderID:  buyOrder.ID,
		SellOrderID: sellOrder.ID,
	}

	// Update quantities
	buyOrder.Quantity -= matchedQty
	sellOrder.Quantity -= matchedQty

	return trade
}

// CalculateExposure calculates the notional exposure for an order
func CalculateExposure(order *Order) float64 {
	return order.Price * order.Quantity
}

// CalculateImbalance calculates the order book imbalance
// Returns a value between -1 and 1:
// -1 means all volume is on the sell side
// +1 means all volume is on the buy side
// 0 means perfect balance
func CalculateImbalance(bidVolume, askVolume float64) float64 {
	totalVolume := bidVolume + askVolume
	if totalVolume == 0 {
		return 0
	}

	return (bidVolume - askVolume) / totalVolume
}

// ValidateOrderPrice checks if an order's price is within acceptable bounds
func ValidateOrderPrice(price, lastTradePrice, maxDeviation float64) bool {
	if lastTradePrice <= 0 || maxDeviation <= 0 {
		return true
	}

	lowerBound := lastTradePrice * (1.0 - maxDeviation)
	upperBound := lastTradePrice * (1.0 + maxDeviation)

	return price >= lowerBound && price <= upperBound
}
