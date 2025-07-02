package store

import (
	"math"
	"sync"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// InMemoryHistoricalDataStore implements an in-memory historical data store
type InMemoryHistoricalDataStore struct {
	priceData     map[string][]float64
	returnsData   map[string][]float64
	defaultReturn float64
	defaultPrice  float64
	mu            sync.RWMutex
	log           *logger.Logger
}

// NewInMemoryHistoricalDataStore creates a new in-memory historical data store
func NewInMemoryHistoricalDataStore() *InMemoryHistoricalDataStore {
	store := &InMemoryHistoricalDataStore{
		priceData:     make(map[string][]float64),
		returnsData:   make(map[string][]float64),
		defaultReturn: 0.0001, // 0.01% daily return
		defaultPrice:  100.0,  // Default price
		log:           logger.GetLogger("store.historical"),
	}

	// Add some sample data
	store.initializeSampleData()
	
	return store
}

// initializeSampleData adds some sample data to the store
func (s *InMemoryHistoricalDataStore) initializeSampleData() {
	// Sample symbols
	symbols := []string{"AAPL", "MSFT", "GOOGL", "AMZN", "SPY", "QQQ"}
	
	for _, symbol := range symbols {
		// Generate synthetic historical prices (252 trading days)
		prices := make([]float64, 252)
		basePrice := 100.0 + float64(len(symbol))*10.0 // Different base price per symbol
		
		for i := 0; i < 252; i++ {
			// Simple random walk with slight upward bias
			randomFactor := 1.0 + (math.Sin(float64(i)*0.1)*0.01) + ((float64(i) / 500.0) * 0.01)
			prices[251-i] = basePrice * randomFactor
		}
		
		// Generate returns from prices
		returns := make([]float64, 251)
		for i := 0; i < 251; i++ {
			returns[i] = (prices[i] / prices[i+1]) - 1.0
		}
		
		s.priceData[symbol] = prices
		s.returnsData[symbol] = returns
	}
}

// GetHistoricalReturns retrieves historical returns for a symbol
func (s *InMemoryHistoricalDataStore) GetHistoricalReturns(symbol string, days int) ([]float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	returns, exists := s.returnsData[symbol]
	if !exists {
		// For symbols we don't have data for, generate synthetic data
		s.log.Infof("Generating synthetic returns data for %s", symbol)
		returns = make([]float64, days)
		for i := 0; i < days; i++ {
			// Random returns with small mean and volatility
			returns[i] = s.defaultReturn + (0.01 * (math.Sin(float64(i)*0.2) * 0.5))
		}
		return returns, nil
	}

	// Return the requested number of days, or all available if we have fewer
	resultDays := int(math.Min(float64(days), float64(len(returns))))
	result := make([]float64, resultDays)
	copy(result, returns[:resultDays])

	return result, nil
}

// GetHistoricalPrices retrieves historical prices for a symbol
func (s *InMemoryHistoricalDataStore) GetHistoricalPrices(symbol string, days int) ([]float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prices, exists := s.priceData[symbol]
	if !exists {
		// For symbols we don't have data for, generate synthetic data
		s.log.Infof("Generating synthetic price data for %s", symbol)
		prices = make([]float64, days)
		basePrice := s.defaultPrice
		
		for i := 0; i < days; i++ {
			// Simple random walk
			randomFactor := 1.0 + (math.Sin(float64(i)*0.1)*0.01)
			prices[days-1-i] = basePrice * randomFactor
		}
		return prices, nil
	}

	// Return the requested number of days, or all available if we have fewer
	resultDays := int(math.Min(float64(days), float64(len(prices))))
	result := make([]float64, resultDays)
	copy(result, prices[:resultDays])

	return result, nil
}