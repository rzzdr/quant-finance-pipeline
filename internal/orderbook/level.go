package orderbook

import (
	"sync"
)

// Level represents a price level in the order book
type Level struct {
	Price      float64
	TotalQty   float64
	OrderCount int
	Orders     *Order // Head of the linked list of orders
	LastOrder  *Order // Tail of the linked list (for fast appends)
	mu         sync.RWMutex
}

// NewLevel creates a new Level
func NewLevel(price float64) *Level {
	return &Level{
		Price:      price,
		TotalQty:   0,
		OrderCount: 0,
		Orders:     nil,
		LastOrder:  nil,
	}
}

// AddOrder adds an order to the level
func (l *Level) AddOrder(order *Order) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Update level statistics
	l.TotalQty += order.Quantity
	l.OrderCount++

	// Add order to the linked list
	if l.Orders == nil {
		l.Orders = order
		l.LastOrder = order
	} else {
		l.LastOrder.Next = order
		order.Prev = l.LastOrder
		l.LastOrder = order
	}
}

// RemoveOrder removes an order from the level
func (l *Level) RemoveOrder(order *Order) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if order == nil {
		return false
	}

	// Update level statistics
	l.TotalQty -= order.Quantity
	l.OrderCount--

	// Remove order from the linked list
	if order.Prev != nil {
		order.Prev.Next = order.Next
	} else {
		// This is the head of the list
		l.Orders = order.Next
	}

	if order.Next != nil {
		order.Next.Prev = order.Prev
	} else {
		// This is the tail of the list
		l.LastOrder = order.Prev
	}

	// Clear the order's links
	order.Next = nil
	order.Prev = nil

	return true
}

// UpdateOrder updates an order's quantity in the level
func (l *Level) UpdateOrder(order *Order, newQuantity float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if order == nil {
		return false
	}

	// Update level statistics
	l.TotalQty = l.TotalQty - order.Quantity + newQuantity

	// Update the order
	order.Quantity = newQuantity

	return true
}

// GetOrders returns a copy of all orders at this level
func (l *Level) GetOrders() []*Order {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.OrderCount == 0 {
		return nil
	}

	orders := make([]*Order, 0, l.OrderCount)
	for current := l.Orders; current != nil; current = current.Next {
		orders = append(orders, current.Clone())
	}

	return orders
}

// GetTopOrder returns the top order at this level (oldest/first)
func (l *Level) GetTopOrder() *Order {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.Orders == nil {
		return nil
	}

	return l.Orders.Clone()
}

// IsEmpty returns true if the level has no orders
func (l *Level) IsEmpty() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.OrderCount == 0
}

// Snapshot returns a snapshot of the level
func (l *Level) Snapshot() LevelSnapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return LevelSnapshot{
		Price:      l.Price,
		TotalQty:   l.TotalQty,
		OrderCount: l.OrderCount,
	}
}

// LevelSnapshot represents a snapshot of a price level
type LevelSnapshot struct {
	Price      float64
	TotalQty   float64
	OrderCount int
}
