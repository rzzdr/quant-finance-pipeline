package models

import (
	"sync"
	"time"
)

// Defines the type of market data
type MarketDataType int

const (
	MarketDataTypeTrade MarketDataType = iota
	MarketDataTypeQuote
	MarketDataTypeDepth
	MarketDataTypeOHLC
	MarketDataTypeIndex
)

// Defines the type of derivative
type DerivativeType int

const (
	DerivativeTypeOption DerivativeType = iota
	DerivativeTypeFuture
	DerivativeTypeForward
	DerivativeTypeSwap
)

// Defines the type of option
type OptionType int

const (
	OptionTypeCall OptionType = iota
	OptionTypePut
)

// A single level in the order book
type PriceLevel struct {
	Price      float64
	Quantity   float64
	OrderCount int32
}

// Contains additional information for derivative instruments
type DerivativeInfo struct {
	Type              DerivativeType
	UnderlyingSymbol  string
	StrikePrice       float64
	ExpiryDate        time.Time
	OptionType        OptionType
	ImpliedVolatility float64
	OpenInterest      float64
	Delta             float64
	Gamma             float64
	Theta             float64
	Vega              float64
	Rho               float64
}

// Represents market data for a single instrument
type MarketData struct {
	Symbol         string
	Exchange       string
	Price          float64
	Volume         float64
	Timestamp      time.Time
	DataType       MarketDataType
	BidLevels      []PriceLevel
	AskLevels      []PriceLevel
	DerivativeInfo *DerivativeInfo
}

// A pool of MarketData objects
type MarketDataPool struct {
	pool sync.Pool
}

// Creates a new MarketDataPool
func NewMarketDataPool() *MarketDataPool {
	return &MarketDataPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &MarketData{
					BidLevels: make([]PriceLevel, 0, 10),
					AskLevels: make([]PriceLevel, 0, 10),
				}
			},
		},
	}
}

// Gets a MarketData object from the pool
func (p *MarketDataPool) Get() *MarketData {
	return p.pool.Get().(*MarketData)
}

// Puts a MarketData object back into the pool
func (p *MarketDataPool) Put(md *MarketData) {
	// Reset the object before putting it back in the pool
	md.Symbol = ""
	md.Exchange = ""
	md.Price = 0
	md.Volume = 0
	md.Timestamp = time.Time{}
	md.DataType = 0
	md.BidLevels = md.BidLevels[:0]
	md.AskLevels = md.AskLevels[:0]
	md.DerivativeInfo = nil
// ==============TO BE COMPLETED==============