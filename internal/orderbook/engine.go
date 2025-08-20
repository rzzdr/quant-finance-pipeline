package orderbook

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

type MatchingEngine struct {
	symbol          string
	bidTree         *RedBlackTree
	askTree         *RedBlackTree
	orderIndex      map[string]*OrderEntry // Maps order ID to order entry
	mutex           sync.RWMutex
	log             *logger.Logger
	eventChannel    chan OrderBookEvent
	metricsRecorder MetricsRecorder
}

type OrderEntry struct {
	Order      *models.Order
	PriceLevel *PriceLevel
	Element    *list.Element
}

type OrderBookEvent struct {
	Type      OrderBookEventType
	Order     *models.Order
	Execution *models.OrderExecution
	Timestamp time.Time
}

type OrderBookEventType int

const (
	OrderAdded OrderBookEventType = iota
	OrderCanceled
	OrderMatched
	OrderModified
)

type MetricsRecorder interface {
	RecordOrderBookUpdate(symbol string, side string, size int)
	RecordOrderBookImbalance(symbol string, imbalance float64)
	RecordOrderProcessed(symbol, orderType, side string)
	RecordOrderFilled(symbol, orderType, side string)
}

func NewMatchingEngine(symbol string, metricsRecorder MetricsRecorder) *MatchingEngine {
	return &MatchingEngine{
		symbol:          symbol,
		bidTree:         NewRedBlackTree(true),  // Bid tree (max heap)
		askTree:         NewRedBlackTree(false), // Ask tree (min heap)
		orderIndex:      make(map[string]*OrderEntry),
		log:             logger.GetLogger("orderbook.matching"),
		eventChannel:    make(chan OrderBookEvent, 1000),
		metricsRecorder: metricsRecorder,
	}
}

func (me *MatchingEngine) AddOrder(ctx context.Context, order *models.Order) ([]*models.OrderExecution, error) {
	if order.Symbol != me.symbol {
		return nil, fmt.Errorf("order symbol %s does not match engine symbol %s", order.Symbol, me.symbol)
	}

	me.mutex.Lock()
	defer me.mutex.Unlock()

	if order.Status == 0 {
		order.Status = models.OrderStatusNew
	}

	if order.OrderID == "" {
		order.OrderID = generateOrderID()
	}

	if order.CreationTime.IsZero() {
		order.CreationTime = time.Now()
	}
	order.UpdateTime = time.Now()

	var executions []*models.OrderExecution

	switch order.Type {
	case models.OrderTypeMarket:
		executions = me.processMarketOrder(order)
	case models.OrderTypeLimit:
		executions = me.processLimitOrder(order)
	default:
		return nil, fmt.Errorf("unsupported order type: %v", order.Type)
	}

	sideStr := "BUY"
	if order.Side == models.OrderSideSell {
		sideStr = "SELL"
	}
	me.metricsRecorder.RecordOrderProcessed(me.symbol, orderTypeToString(order.Type), sideStr)

	if len(executions) > 0 {
		me.metricsRecorder.RecordOrderFilled(me.symbol, orderTypeToString(order.Type), sideStr)
	}

	me.sendEvent(OrderBookEvent{
		Type:      OrderAdded,
		Order:     order,
		Timestamp: time.Now(),
	})

	for _, exec := range executions {
		me.sendEvent(OrderBookEvent{
			Type:      OrderMatched,
			Order:     order,
			Execution: exec,
			Timestamp: time.Now(),
		})
	}

	return executions, nil
}

func (me *MatchingEngine) processMarketOrder(order *models.Order) []*models.OrderExecution {
	var executions []*models.OrderExecution
	remainingQty := order.Quantity

	var oppositeTree *RedBlackTree
	if order.Side == models.OrderSideBuy {
		oppositeTree = me.askTree
	} else {
		oppositeTree = me.bidTree
	}

	for remainingQty > 0 {
		bestLevel := oppositeTree.GetBest()
		if bestLevel == nil || bestLevel.IsEmpty() {
			break // No more liquidity
		}

		levelExecutions, remaining := bestLevel.MatchQuantity(remainingQty)
		executions = append(executions, levelExecutions...)
		remainingQty = remaining

		if bestLevel.IsEmpty() {
			oppositeTree.Delete(bestLevel.Price)
		}
	}

	if remainingQty == 0 {
		order.Status = models.OrderStatusFilled
	} else if remainingQty < order.Quantity {
		order.Status = models.OrderStatusPartiallyFilled
	}

	order.Quantity = remainingQty
	return executions
}

func (me *MatchingEngine) processLimitOrder(order *models.Order) []*models.OrderExecution {
	var executions []*models.OrderExecution
	remainingQty := order.Quantity

	var oppositeTree *RedBlackTree
	if order.Side == models.OrderSideBuy {
		oppositeTree = me.askTree
	} else {
		oppositeTree = me.bidTree
	}

	for remainingQty > 0 {
		bestLevel := oppositeTree.GetBest()
		if bestLevel == nil || bestLevel.IsEmpty() {
			break
		}

		var crosses bool
		if order.Side == models.OrderSideBuy {
			crosses = order.Price >= bestLevel.Price
		} else {
			crosses = order.Price <= bestLevel.Price
		}

		if !crosses {
			break
		}

		levelExecutions, remaining := bestLevel.MatchQuantity(remainingQty)
		executions = append(executions, levelExecutions...)
		remainingQty = remaining

		if bestLevel.IsEmpty() {
			oppositeTree.Delete(bestLevel.Price)
		}
	}

	if remainingQty > 0 {
		order.Quantity = remainingQty
		me.addToBook(order)
	}

	if remainingQty == 0 {
		order.Status = models.OrderStatusFilled
	} else if remainingQty < order.Quantity {
		order.Status = models.OrderStatusPartiallyFilled
	} else {
		order.Status = models.OrderStatusNew
	}

	return executions
}

func (me *MatchingEngine) addToBook(order *models.Order) {
	var tree *RedBlackTree
	if order.Side == models.OrderSideBuy {
		tree = me.bidTree
	} else {
		tree = me.askTree
	}

	priceLevel := tree.Insert(order.Price)
	element := priceLevel.AddOrder(order)

	me.orderIndex[order.OrderID] = &OrderEntry{
		Order:      order,
		PriceLevel: priceLevel,
		Element:    element,
	}

	sideStr := "BUY"
	if order.Side == models.OrderSideSell {
		sideStr = "SELL"
	}
	me.metricsRecorder.RecordOrderBookUpdate(me.symbol, sideStr, tree.Size())
}

func (me *MatchingEngine) CancelOrder(orderID string) error {
	me.mutex.Lock()
	defer me.mutex.Unlock()

	entry, exists := me.orderIndex[orderID]
	if !exists {
		return fmt.Errorf("order %s not found", orderID)
	}

	entry.PriceLevel.RemoveOrder(entry.Element)
	entry.Order.Status = models.OrderStatusCanceled
	entry.Order.UpdateTime = time.Now()

	delete(me.orderIndex, orderID)

	if entry.PriceLevel.IsEmpty() {
		var tree *RedBlackTree
		if entry.Order.Side == models.OrderSideBuy {
			tree = me.bidTree
		} else {
			tree = me.askTree
		}
		tree.Delete(entry.PriceLevel.Price)
	}

	me.sendEvent(OrderBookEvent{
		Type:      OrderCanceled,
		Order:     entry.Order,
		Timestamp: time.Now(),
	})

	return nil
}

func (me *MatchingEngine) ModifyOrder(orderID string, newPrice float64, newQuantity float64) ([]*models.OrderExecution, error) {
	me.mutex.Lock()
	entry, exists := me.orderIndex[orderID]
	if !exists {
		me.mutex.Unlock()
		return nil, fmt.Errorf("order %s not found", orderID)
	}

	originalOrder := entry.Order
	me.mutex.Unlock()

	if err := me.CancelOrder(orderID); err != nil {
		return nil, err
	}

	newOrder := &models.Order{
		OrderID:       generateOrderID(),
		Symbol:        originalOrder.Symbol,
		Side:          originalOrder.Side,
		Type:          originalOrder.Type,
		Quantity:      newQuantity,
		Price:         newPrice,
		TimeInForce:   originalOrder.TimeInForce,
		Status:        models.OrderStatusNew,
		ClientID:      originalOrder.ClientID,
		CreationTime:  time.Now(),
		UpdateTime:    time.Now(),
		ParentOrderID: originalOrder.OrderID,
	}

	executions, err := me.AddOrder(context.Background(), newOrder)
	if err != nil {
		return nil, err
	}

	me.sendEvent(OrderBookEvent{
		Type:      OrderModified,
		Order:     newOrder,
		Timestamp: time.Now(),
	})

	return executions, nil
}

func (me *MatchingEngine) GetOrder(orderID string) (*models.Order, error) {
	me.mutex.RLock()
	defer me.mutex.RUnlock()

	entry, exists := me.orderIndex[orderID]
	if !exists {
		return nil, fmt.Errorf("order %s not found", orderID)
	}

	return entry.Order, nil
}

func (me *MatchingEngine) GetSnapshot(maxLevels int) *models.OrderBook {
	me.mutex.RLock()
	defer me.mutex.RUnlock()

	bidLevels := me.bidTree.GetLevels(maxLevels)
	askLevels := me.askTree.GetLevels(maxLevels)

	snapshot := &models.OrderBook{
		Symbol:    me.symbol,
		Timestamp: time.Now(),
		Bids:      make([]models.OrderBookLevel, 0, len(bidLevels)),
		Asks:      make([]models.OrderBookLevel, 0, len(askLevels)),
	}

	for _, level := range bidLevels {
		snapshot.Bids = append(snapshot.Bids, models.OrderBookLevel{
			Price:      level.Price,
			Quantity:   level.GetVolume(),
			OrderCount: int32(level.GetOrderCount()),
		})
	}

	for _, level := range askLevels {
		snapshot.Asks = append(snapshot.Asks, models.OrderBookLevel{
			Price:      level.Price,
			Quantity:   level.GetVolume(),
			OrderCount: int32(level.GetOrderCount()),
		})
	}

	return snapshot
}

func (me *MatchingEngine) GetMarketDepth() *MarketDepth {
	me.mutex.RLock()
	defer me.mutex.RUnlock()

	depth := &MarketDepth{
		Symbol:    me.symbol,
		Timestamp: time.Now(),
	}

	bidLevels := me.bidTree.GetLevels(10)
	for _, level := range bidLevels {
		depth.BidVolume += level.GetVolume()
		depth.BidOrderCount += level.GetOrderCount()
	}

	askLevels := me.askTree.GetLevels(10)
	for _, level := range askLevels {
		depth.AskVolume += level.GetVolume()
		depth.AskOrderCount += level.GetOrderCount()
	}

	if len(bidLevels) > 0 && len(askLevels) > 0 {
		depth.BestBid = bidLevels[0].Price
		depth.BestAsk = askLevels[0].Price
		depth.Spread = depth.BestAsk - depth.BestBid
		depth.MidPrice = (depth.BestBid + depth.BestAsk) / 2

		totalVolume := depth.BidVolume + depth.AskVolume
		if totalVolume > 0 {
			depth.Imbalance = (depth.BidVolume - depth.AskVolume) / totalVolume
			me.metricsRecorder.RecordOrderBookImbalance(me.symbol, depth.Imbalance)
		}
	}

	return depth
}

type MarketDepth struct {
	Symbol        string
	Timestamp     time.Time
	BestBid       float64
	BestAsk       float64
	Spread        float64
	MidPrice      float64
	BidVolume     float64
	AskVolume     float64
	BidOrderCount int
	AskOrderCount int
	Imbalance     float64 // (BidVolume - AskVolume) / (BidVolume + AskVolume)
}

func (me *MatchingEngine) GetEventChannel() <-chan OrderBookEvent {
	return me.eventChannel
}

func (me *MatchingEngine) sendEvent(event OrderBookEvent) {
	select {
	case me.eventChannel <- event:
	default:
		me.log.Warn("Event channel full, dropping event")
	}
}

func (me *MatchingEngine) Close() {
	close(me.eventChannel)
}

func generateOrderID() string {
	return "ord_" + uuid.New().String()
}

func orderTypeToString(ot models.OrderType) string {
	switch ot {
	case models.OrderTypeMarket:
		return "MARKET"
	case models.OrderTypeLimit:
		return "LIMIT"
	case models.OrderTypeStop:
		return "STOP"
	case models.OrderTypeStopLimit:
		return "STOP_LIMIT"
	case models.OrderTypeTrailingStop:
		return "TRAILING_STOP"
	default:
		return "UNKNOWN"
	}
}
