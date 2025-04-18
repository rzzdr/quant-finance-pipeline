package orderbook

import (
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
)

// Order represents an order in the order book
type Order struct {
	ID        string
	Side      Side
	Price     float64
	Quantity  float64
	Timestamp time.Time
	UserID    string
	OrderType OrderType
	Next      *Order // For efficient list traversal
	Prev      *Order // For efficient list traversal
}

// Side represents the side of an order
type Side int

const (
	Buy Side = iota
	Sell
)

// OrderType represents the type of an order
type OrderType int

const (
	Limit OrderType = iota
	Market
	Stop
	StopLimit
)

// OrderPool manages a pool of Order objects to reduce allocations
type OrderPool struct {
	pool sync.Pool
}

// NewOrderPool creates a new OrderPool
func NewOrderPool(size int) *OrderPool {
	return &OrderPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &Order{}
			},
		},
	}
}

// Get gets an Order from the pool
func (p *OrderPool) Get() *Order {
	return p.pool.Get().(*Order)
}

// Put returns an Order to the pool
func (p *OrderPool) Put(o *Order) {
	// Reset the order before returning it to the pool
	o.ID = ""
	o.Side = 0
	o.Price = 0
	o.Quantity = 0
	o.Timestamp = time.Time{}
	o.UserID = ""
	o.OrderType = 0
	o.Next = nil
	o.Prev = nil
	p.pool.Put(o)
}

// FromModelOrder converts a models.Order to an Order
func FromModelOrder(modelOrder *models.Order) *Order {
	if modelOrder == nil {
		return nil
	}

	order := &Order{
		ID:        modelOrder.OrderID,
		Price:     modelOrder.Price,
		Quantity:  modelOrder.Quantity,
		Timestamp: modelOrder.CreationTime,
		UserID:    modelOrder.ClientID,
	}

	// Convert the side
	switch modelOrder.Side {
	case models.OrderSideBuy:
		order.Side = Buy
	case models.OrderSideSell:
		order.Side = Sell
	}

	// Convert the order type
	switch modelOrder.Type {
	case models.OrderTypeLimit:
		order.OrderType = Limit
	case models.OrderTypeMarket:
		order.OrderType = Market
	case models.OrderTypeStop:
		order.OrderType = Stop
	case models.OrderTypeStopLimit:
		order.OrderType = StopLimit
	}

	return order
}

// ToModelOrder converts an Order to a models.Order
func (o *Order) ToModelOrder() *models.Order {
	if o == nil {
		return nil
	}

	modelOrder := &models.Order{
		OrderID:      o.ID,
		Price:        o.Price,
		Quantity:     o.Quantity,
		CreationTime: o.Timestamp,
		UpdateTime:   time.Now(),
		ClientID:     o.UserID,
	}

	// Convert the side
	switch o.Side {
	case Buy:
		modelOrder.Side = models.OrderSideBuy
	case Sell:
		modelOrder.Side = models.OrderSideSell
	}

	// Convert the order type
	switch o.OrderType {
	case Limit:
		modelOrder.Type = models.OrderTypeLimit
	case Market:
		modelOrder.Type = models.OrderTypeMarket
	case Stop:
		modelOrder.Type = models.OrderTypeStop
	case StopLimit:
		modelOrder.Type = models.OrderTypeStopLimit
	}

	return modelOrder
}

// NewOrder creates a new Order
func NewOrder(id string, side Side, price, quantity float64, userID string, orderType OrderType) *Order {
	return &Order{
		ID:        id,
		Side:      side,
		Price:     price,
		Quantity:  quantity,
		Timestamp: time.Now(),
		UserID:    userID,
		OrderType: orderType,
	}
}

// Clone creates a deep copy of the Order
func (o *Order) Clone() *Order {
	if o == nil {
		return nil
	}

	clone := &Order{
		ID:        o.ID,
		Side:      o.Side,
		Price:     o.Price,
		Quantity:  o.Quantity,
		Timestamp: o.Timestamp,
		UserID:    o.UserID,
		OrderType: o.OrderType,
	}

	return clone
}
