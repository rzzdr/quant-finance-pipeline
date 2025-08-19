package orderbook

import (
	"container/list"
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
)

// PriceLevel represents a price level in the order book with orders at that price
type PriceLevel struct {
	Price  float64
	Orders *list.List // Linked list of orders at this price level
	Volume float64    // Total volume at this price level
	mutex  sync.RWMutex
}

// NewPriceLevel creates a new price level
func NewPriceLevel(price float64) *PriceLevel {
	return &PriceLevel{
		Price:  price,
		Orders: list.New(),
		Volume: 0,
	}
}

// AddOrder adds an order to this price level
func (pl *PriceLevel) AddOrder(order *models.Order) *list.Element {
	pl.mutex.Lock()
	defer pl.mutex.Unlock()

	pl.Volume += order.Quantity
	return pl.Orders.PushBack(order)
}

// RemoveOrder removes an order from this price level
func (pl *PriceLevel) RemoveOrder(element *list.Element) {
	pl.mutex.Lock()
	defer pl.mutex.Unlock()

	if order, ok := element.Value.(*models.Order); ok {
		pl.Volume -= order.Quantity
	}
	pl.Orders.Remove(element)
}

// GetVolume returns the total volume at this price level
func (pl *PriceLevel) GetVolume() float64 {
	pl.mutex.RLock()
	defer pl.mutex.RUnlock()
	return pl.Volume
}

// GetOrderCount returns the number of orders at this price level
func (pl *PriceLevel) GetOrderCount() int {
	pl.mutex.RLock()
	defer pl.mutex.RUnlock()
	return pl.Orders.Len()
}

// GetFirstOrder returns the first order in the queue (FIFO)
func (pl *PriceLevel) GetFirstOrder() *models.Order {
	pl.mutex.RLock()
	defer pl.mutex.RUnlock()

	if pl.Orders.Len() == 0 {
		return nil
	}

	if order, ok := pl.Orders.Front().Value.(*models.Order); ok {
		return order
	}
	return nil
}

// IsEmpty returns true if this price level has no orders
func (pl *PriceLevel) IsEmpty() bool {
	pl.mutex.RLock()
	defer pl.mutex.RUnlock()
	return pl.Orders.Len() == 0
}

// MatchQuantity tries to match the given quantity against orders at this price level
// Returns matched orders and remaining quantity
func (pl *PriceLevel) MatchQuantity(quantity float64) ([]*models.OrderExecution, float64) {
	pl.mutex.Lock()
	defer pl.mutex.Unlock()

	var executions []*models.OrderExecution
	remainingQty := quantity

	for element := pl.Orders.Front(); element != nil && remainingQty > 0; {
		order := element.Value.(*models.Order)
		next := element.Next()

		var matchedQty float64
		if order.Quantity <= remainingQty {
			// Full fill
			matchedQty = order.Quantity
			pl.Volume -= order.Quantity
			pl.Orders.Remove(element)
			order.Status = models.OrderStatusFilled
		} else {
			// Partial fill
			matchedQty = remainingQty
			order.Quantity -= matchedQty
			pl.Volume -= matchedQty
			order.Status = models.OrderStatusPartiallyFilled
		}

		execution := &models.OrderExecution{
			ExecutionID:   generateExecutionID(),
			Quantity:      matchedQty,
			Price:         pl.Price,
			ExecutionTime: time.Now(),
			Venue:         "INTERNAL",
		}

		order.Executions = append(order.Executions, *execution)
		order.UpdateTime = time.Now()

		executions = append(executions, execution)
		remainingQty -= matchedQty

		element = next
	}

	return executions, remainingQty
}

func generateExecutionID() string {
	return "exec_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
