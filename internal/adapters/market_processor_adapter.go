package adapters

import (
	"errors"

	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
)

// MarketProcessorAdapter adapts *market.MarketDataProcessor to be used with API server
type MarketProcessorAdapter struct {
	processor *market.MarketDataProcessor
}

// NewMarketProcessorAdapter creates a new MarketProcessorAdapter
func NewMarketProcessorAdapter(processor *market.MarketDataProcessor) market.Processor {
	return &MarketProcessorAdapter{
		processor: processor,
	}
}

// GetMarketData implements the market.Processor interface
func (a *MarketProcessorAdapter) GetMarketData(symbol string) (*models.MarketData, error) {
	data, ok := a.processor.GetMarketData(symbol)
	if !ok {
		return nil, errors.New("market data not found for symbol: " + symbol)
	}
	return data, nil
}

// GetOrderBook implements the market.Processor interface
func (a *MarketProcessorAdapter) GetOrderBook(symbol string) (*models.OrderBook, error) {
	book, ok := a.processor.GetOrderBook(symbol)
	if !ok {
		return nil, errors.New("order book not found for symbol: " + symbol)
	}
	bookCopy := &models.OrderBook{
		Symbol:    book.Symbol,
		Timestamp: book.Timestamp,
		Asks:      book.Asks,
		Bids:      book.Bids,
	}
	return bookCopy, nil
}

// PlaceOrder implements the market.Processor interface
func (a *MarketProcessorAdapter) PlaceOrder(order *models.Order) (interface{}, error) {
	// Since MarketDataProcessor doesn't have this method, we'll provide a stub implementation
	return nil, errors.New("place order operation not supported")
}

// GetOrder implements the market.Processor interface
func (a *MarketProcessorAdapter) GetOrder(orderID string) (*models.Order, error) {
	// Since MarketDataProcessor doesn't have this method, we'll provide a stub implementation
	return nil, errors.New("get order operation not supported")
}

// CancelOrder implements the market.Processor interface
func (a *MarketProcessorAdapter) CancelOrder(orderID string) error {
	// Since MarketDataProcessor doesn't have this method, we'll provide a stub implementation
	return errors.New("cancel order operation not supported")
}
