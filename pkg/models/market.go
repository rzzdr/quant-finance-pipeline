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

func (md *MarketData) UnmarshalJSON(value []byte) error {
	panic("unimplemented")
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

	p.pool.Put(md)
}

// Creates a new DerivativeInfo object
func NewDerivativeInfo(
	derivType DerivativeType,
	underlyingSymbol string,
	strikePrice float64,
	expiryDate time.Time,
	optionType OptionType,
) *DerivativeInfo {
	return &DerivativeInfo{
		Type:              derivType,
		UnderlyingSymbol:  underlyingSymbol,
		StrikePrice:       strikePrice,
		ExpiryDate:        expiryDate,
		OptionType:        optionType,
		ImpliedVolatility: 0,
		OpenInterest:      0,
		Delta:             0,
		Gamma:             0,
		Theta:             0,
		Vega:              0,
		Rho:               0,
	}
}

// Creates a deep copy of the MarketData
func (md *MarketData) DeepCopy() *MarketData {
	newMD := &MarketData{
		Symbol:    md.Symbol,
		Exchange:  md.Exchange,
		Price:     md.Price,
		Volume:    md.Volume,
		Timestamp: md.Timestamp,
		DataType:  md.DataType,
	}

	if len(md.BidLevels) > 0 {
		newMD.BidLevels = make([]PriceLevel, len(md.BidLevels))
		copy(newMD.BidLevels, md.BidLevels)
	}

	if len(md.AskLevels) > 0 {
		newMD.AskLevels = make([]PriceLevel, len(md.AskLevels))
		copy(newMD.AskLevels, md.AskLevels)
	}

	if md.DerivativeInfo != nil {
		newMD.DerivativeInfo = &DerivativeInfo{
			Type:              md.DerivativeInfo.Type,
			UnderlyingSymbol:  md.DerivativeInfo.UnderlyingSymbol,
			StrikePrice:       md.DerivativeInfo.StrikePrice,
			ExpiryDate:        md.DerivativeInfo.ExpiryDate,
			OptionType:        md.DerivativeInfo.OptionType,
			ImpliedVolatility: md.DerivativeInfo.ImpliedVolatility,
			OpenInterest:      md.DerivativeInfo.OpenInterest,
			Delta:             md.DerivativeInfo.Delta,
			Gamma:             md.DerivativeInfo.Gamma,
			Theta:             md.DerivativeInfo.Theta,
			Vega:              md.DerivativeInfo.Vega,
			Rho:               md.DerivativeInfo.Rho,
		}
	}

	return newMD
}

// Gets the mid price from the top of the order book
func (md *MarketData) MidPrice() float64 {
	if len(md.BidLevels) > 0 && len(md.AskLevels) > 0 {
		return (md.BidLevels[0].Price + md.AskLevels[0].Price) / 2.0
	}

	// Fall back to the last trade price if we don't have both bid and ask
	return md.Price
}

// Gets the spread from the top of the order book
func (md *MarketData) Spread() float64 {
	if len(md.BidLevels) > 0 && len(md.AskLevels) > 0 {
		return md.AskLevels[0].Price - md.BidLevels[0].Price
	}
	return 0
}

// Gets the spread in basis points
func (md *MarketData) SpreadBPS() float64 {
	mid := md.MidPrice()
	if mid > 0 {
		return (md.Spread() / mid) * 10000.0
	}
	return 0
}

// Gets the total bid quantity across all levels
func (md *MarketData) TotalBidQuantity() float64 {
	var total float64
	for _, level := range md.BidLevels {
		total += level.Quantity
	}
	return total
}

// Gets the total ask quantity across all levels
func (md *MarketData) TotalAskQuantity() float64 {
	var total float64
	for _, level := range md.AskLevels {
		total += level.Quantity
	}
	return total
}

// Checks if the market data is for a derivative instrument
func (md *MarketData) IsDerivative() bool {
	return md.DerivativeInfo != nil
}

// Gets the weighted average price of the bid side
func (md *MarketData) WeightedBidPrice() float64 {
	var weightedTotal, quantityTotal float64
	for _, level := range md.BidLevels {
		weightedTotal += level.Price * level.Quantity
		quantityTotal += level.Quantity
	}
	if quantityTotal > 0 {
		return weightedTotal / quantityTotal
	}
	return 0
}

// Gets the weighted average price of the ask side
func (md *MarketData) WeightedAskPrice() float64 {
	var weightedTotal, quantityTotal float64
	for _, level := range md.AskLevels {
		weightedTotal += level.Price * level.Quantity
		quantityTotal += level.Quantity
	}
	if quantityTotal > 0 {
		return weightedTotal / quantityTotal
	}
	return 0
}

// Creates a batch of market data
type MarketDataBatch struct {
	Data            []*MarketData
	BatchTimestamp  time.Time
	Source          string
	ProcessingStats *BatchProcessingStats
}

// Statistics about batch processing
type BatchProcessingStats struct {
	StartTime       time.Time
	EndTime         time.Time
	ProcessingTime  time.Duration
	ItemCount       int
	DroppedItems    int
	ProcessingDelay time.Duration
}

// Creates a new market data batch
func NewMarketDataBatch(source string) *MarketDataBatch {
	return &MarketDataBatch{
		Data:           make([]*MarketData, 0, 100),
		BatchTimestamp: time.Now(),
		Source:         source,
		ProcessingStats: &BatchProcessingStats{
			StartTime:    time.Now(),
			ItemCount:    0,
			DroppedItems: 0,
		},
	}
}

// Adds a market data item to the batch
func (b *MarketDataBatch) Add(md *MarketData) {
	b.Data = append(b.Data, md)
	b.ProcessingStats.ItemCount++
}

// Completes batch processing and records statistics
func (b *MarketDataBatch) Complete() {
	b.ProcessingStats.EndTime = time.Now()
	b.ProcessingStats.ProcessingTime = b.ProcessingStats.EndTime.Sub(b.ProcessingStats.StartTime)
	if len(b.Data) > 0 {
		latestTimestamp := b.Data[0].Timestamp
		for _, md := range b.Data {
			if md.Timestamp.After(latestTimestamp) {
				latestTimestamp = md.Timestamp
			}
		}
		b.ProcessingStats.ProcessingDelay = b.ProcessingStats.EndTime.Sub(latestTimestamp)
	}
}
