package models

import (
	"sync"
	"time"
)

// The side of an order
type OrderSide int

const (
	OrderSideBuy OrderSide = iota
	OrderSideSell
)

// The type of an order
type OrderType int

const (
	OrderTypeMarket OrderType = iota
	OrderTypeLimit
	OrderTypeStop
	OrderTypeStopLimit
	OrderTypeTrailingStop
)

// The time in force of an order
type OrderTimeInForce int

const (
	OrderTimeInForceGTC OrderTimeInForce = iota
	OrderTimeInForceIOC
	OrderTimeInForceFOK
	OrderTimeInForceDAY
)

// The status of an order
type OrderStatus int

const (
	OrderStatusNew OrderStatus = iota
	OrderStatusPartiallyFilled
	OrderStatusFilled
	OrderStatusCanceled
	OrderStatusRejected
	OrderStatusPending
)

// The limits for an order
type OrderLimits struct {
	MaxQuantity       float64
	MaxNotional       float64
	MaxPriceDeviation float64
}

// Execution of an order
type OrderExecution struct {
	ExecutionID   string
	Quantity      float64
	Price         float64
	ExecutionTime time.Time
	Venue         string
}

// A trading order
type Order struct {
	OrderID       string
	Symbol        string
	Side          OrderSide
	Type          OrderType
	Quantity      float64
	Price         float64
	TimeInForce   OrderTimeInForce
	Status        OrderStatus
	ClientID      string
	CreationTime  time.Time
	UpdateTime    time.Time
	ParentOrderID string
	Limits        *OrderLimits
	Executions    []OrderExecution
}

// A price level in an order book
type OrderBookLevel struct {
	Price      float64
	Quantity   float64
	OrderCount int32
}

// A snapshot of an order book
type OrderBook struct {
	Symbol    string
	Timestamp time.Time
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
}

// A pool of Order objects
type OrderPool struct {
	pool sync.Pool
}

// Creates a new OrderPool
func NewOrderPool() *OrderPool {
	return &OrderPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &Order{
					Executions: make([]OrderExecution, 0, 5),
				}
			},
		},
	}
}

// Gets an Order object from the pool
func (p *OrderPool) Get() *Order {
	return p.pool.Get().(*Order)
}

// Puts an Order object back into the pool
func (p *OrderPool) Put(order *Order) {
	// Reset the object before putting it back in the pool
	order.OrderID = ""
	order.Symbol = ""
	order.Side = 0
	order.Type = 0
	order.Quantity = 0
	order.Price = 0
	order.TimeInForce = 0
	order.Status = 0
	order.ClientID = ""
	order.CreationTime = time.Time{}
	order.UpdateTime = time.Time{}
	order.ParentOrderID = ""
	order.Limits = nil
	order.Executions = order.Executions[:0]

	p.pool.Put(order)
}

// Creates a deep copy of the Order
func (o *Order) DeepCopy() *Order {
	newOrder := &Order{
		OrderID:       o.OrderID,
		Symbol:        o.Symbol,
		Side:          o.Side,
		Type:          o.Type,
		Quantity:      o.Quantity,
		Price:         o.Price,
		TimeInForce:   o.TimeInForce,
		Status:        o.Status,
		ClientID:      o.ClientID,
		CreationTime:  o.CreationTime,
		UpdateTime:    o.UpdateTime,
		ParentOrderID: o.ParentOrderID,
	}

	if o.Limits != nil {
		newOrder.Limits = &OrderLimits{
			MaxQuantity:       o.Limits.MaxQuantity,
			MaxNotional:       o.Limits.MaxNotional,
			MaxPriceDeviation: o.Limits.MaxPriceDeviation,
		}
	}

	if len(o.Executions) > 0 {
		newOrder.Executions = make([]OrderExecution, len(o.Executions))
		copy(newOrder.Executions, o.Executions)
	}

	return newOrder
}
