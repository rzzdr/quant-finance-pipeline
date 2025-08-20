package market

import (
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
)

// DataFeed represents a market data feed
type DataFeed struct {
	ID          string
	Name        string
	Description string
	Type        FeedType
	Status      FeedStatus
	Config      map[string]string
}

// FeedType represents the type of a data feed
type FeedType string

const (
	// FeedTypeHistorical represents a historical data feed
	FeedTypeHistorical FeedType = "historical"
	// FeedTypeRealtime represents a real-time data feed
	FeedTypeRealtime FeedType = "realtime"
	// FeedTypeSimulated represents a simulated data feed
	FeedTypeSimulated FeedType = "simulated"
)

// FeedStatus represents the status of a data feed
type FeedStatus string

const (
	// FeedStatusActive represents an active data feed
	FeedStatusActive FeedStatus = "active"
	// FeedStatusInactive represents an inactive data feed
	FeedStatusInactive FeedStatus = "inactive"
	// FeedStatusError represents a data feed in error state
	FeedStatusError FeedStatus = "error"
)

// MarketDataUpdate represents an update to market data
type MarketDataUpdate struct {
	Symbol    string
	Timestamp time.Time
	Type      models.MarketDataType
	Price     float64
	Volume    float64
	BidLevels []models.PriceLevel
	AskLevels []models.PriceLevel
	Source    string
}

// ExtendedMarketDataProvider defines the extended interface for market data providers
// This adds to the MarketDataProvider interface defined in processor.go
type ExtendedMarketDataProvider interface {
	MarketDataProvider
	// GetSnapshot returns a snapshot of market data for the given symbol and type
	GetSnapshot(symbol string, dataType models.MarketDataType) (*models.MarketData, error)
}

// HistoricalDataProvider defines the interface for historical data providers
type HistoricalDataProvider interface {
	// GetData retrieves historical market data for the given parameters
	GetData(symbol string, dataType models.MarketDataType, startTime, endTime time.Time, interval time.Duration) ([]*models.MarketData, error)
	// GetDataBatch retrieves historical market data for multiple symbols
	GetDataBatch(symbols []string, dataType models.MarketDataType, startTime, endTime time.Time, interval time.Duration) (map[string][]*models.MarketData, error)
}

// MarketDataAggregatorConfig is configuration for MarketDataAggregator
type MarketDataAggregatorConfig struct {
	BufferSize int
	LogLevel   string
}

// OrderSide represents the side of an order
type OrderSide int

const (
	// Buy represents a buy order
	Buy OrderSide = iota
	// Sell represents a sell order
	Sell
)

// OrderType represents the type of order
type OrderType int

const (
	// Market represents a market order
	Market OrderType = iota
	// Limit represents a limit order
	Limit
	// Stop represents a stop order
	Stop
	// StopLimit represents a stop-limit order
	StopLimit
)

// DataSource represents a source of market data
type DataSource struct {
	Name        string
	Description string
	Priority    int
	Enabled     bool
	Connected   bool
	mu          sync.RWMutex
}

// Exchange represents a trading exchange
type Exchange struct {
	Name          string
	MakerFee      float64
	TakerFee      float64
	MinOrderSize  float64
	MaxOrderSize  float64
	PriceDecimals int
	SizeDecimals  int
	Connected     bool
	mu            sync.RWMutex
}

// InstrumentInfo contains information about a financial instrument
type InstrumentInfo struct {
	Symbol           string
	Name             string
	Type             string
	BaseCurrency     string
	QuoteCurrency    string
	PriceIncrement   float64
	SizeIncrement    float64
	MinOrderSize     float64
	MaxOrderSize     float64
	ExpiryDate       *time.Time
	UnderlyingSymbol string
}

// MarketStatsUpdate represents an update to market statistics
type MarketStatsUpdate struct {
	Symbol          string
	Timestamp       time.Time
	OpenPrice       float64
	HighPrice       float64
	LowPrice        float64
	ClosePrice      float64
	Volume          float64
	QuoteVolume     float64
	TradeCount      int64
	VWAP            float64
	OpenInterest    float64
	FundingRate     float64
	NextFundingTime time.Time
}

// TimeBarPeriod represents a time period for time bars
type TimeBarPeriod string

const (
	// Period1Min represents a 1-minute time bar
	Period1Min TimeBarPeriod = "1m"
	// Period5Min represents a 5-minute time bar
	Period5Min TimeBarPeriod = "5m"
	// Period15Min represents a 15-minute time bar
	Period15Min TimeBarPeriod = "15m"
	// Period30Min represents a 30-minute time bar
	Period30Min TimeBarPeriod = "30m"
	// Period1H represents a 1-hour time bar
	Period1H TimeBarPeriod = "1h"
	// Period4H represents a 4-hour time bar
	Period4H TimeBarPeriod = "4h"
	// Period1D represents a 1-day time bar
	Period1D TimeBarPeriod = "1d"
	// Period1W represents a 1-week time bar
	Period1W TimeBarPeriod = "1w"
)

// TimeBar represents a time bar (OHLCV)
type TimeBar struct {
	Symbol     string
	Period     TimeBarPeriod
	Timestamp  time.Time
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Volume     float64
	TradeCount int64
	VWAP       float64
}

// MarketEvent represents a market event
type MarketEvent struct {
	Type      MarketEventType
	Timestamp time.Time
	Symbol    string
	Data      interface{}
}

// MarketEventType represents the type of market event
type MarketEventType int

const (
	// EventPriceChange represents a price change event
	EventPriceChange MarketEventType = iota
	// EventTrade represents a trade event
	EventTrade
	// EventOrderBookUpdate represents an order book update event
	EventOrderBookUpdate
	// EventMarketStats represents a market statistics event
	EventMarketStats
	// EventLiquidation represents a liquidation event
	EventLiquidation
	// EventFunding represents a funding event
	EventFunding
	// EventPriceAlert represents a price alert event
	EventPriceAlert
)

// Trade represents a trade in the market
type Trade struct {
	ID          string
	Symbol      string
	Price       float64
	Size        float64
	Side        OrderSide
	Timestamp   time.Time
	BuyOrderID  string
	SellOrderID string
	Exchange    string
	IsMaker     bool
	Fee         float64
	FeeCurrency string
}

// TradesUpdate contains a batch of trades
type TradesUpdate struct {
	Symbol    string
	Trades    []*Trade
	Timestamp time.Time
}

// NewTrade creates a new trade
func NewTrade() *Trade {
	return &Trade{
		Timestamp: time.Now(),
	}
}

// NewTradesUpdate creates a new trades update
func NewTradesUpdate(symbol string) *TradesUpdate {
	return &TradesUpdate{
		Symbol:    symbol,
		Trades:    make([]*Trade, 0),
		Timestamp: time.Now(),
	}
}

// Add adds a trade to the trades update
func (tu *TradesUpdate) Add(trade *Trade) {
	tu.Trades = append(tu.Trades, trade)
}

// MarketDataTypeToString converts a market data type to a string
func MarketDataTypeToString(dataType models.MarketDataType) string {
	switch dataType {
	case models.MarketDataTypeTrade:
		return "trade"
	case models.MarketDataTypeQuote:
		return "quote"
	case models.MarketDataTypeDepth:
		return "depth"
	case models.MarketDataTypeOHLC:
		return "ohlc"
	case models.MarketDataTypeIndex:
		return "index"
	case models.MarketDataTypeBar:
		return "bar"
	case models.MarketDataTypeStats:
		return "stats"
	case models.MarketDataTypeNews:
		return "news"
	default:
		return "unknown"
	}
}

// StringToMarketDataType converts a string to a market data type
func StringToMarketDataType(dataType string) models.MarketDataType {
	switch dataType {
	case "trade":
		return models.MarketDataTypeTrade
	case "quote":
		return models.MarketDataTypeQuote
	case "depth":
		return models.MarketDataTypeDepth
	case "ohlc":
		return models.MarketDataTypeOHLC
	case "index":
		return models.MarketDataTypeIndex
	case "bar":
		return models.MarketDataTypeBar
	case "stats":
		return models.MarketDataTypeStats
	case "news":
		return models.MarketDataTypeNews
	default:
		return models.MarketDataTypeUnknown
	}
}

// OrderSideToString converts an order side to a string
func OrderSideToString(side OrderSide) string {
	switch side {
	case Buy:
		return "buy"
	case Sell:
		return "sell"
	default:
		return "unknown"
	}
}

// StringToOrderSide converts a string to an order side
func StringToOrderSide(side string) OrderSide {
	switch side {
	case "buy":
		return Buy
	case "sell":
		return Sell
	default:
		return Buy // Default to buy
	}
}

// OrderTypeToString converts an order type to a string
func OrderTypeToString(orderType OrderType) string {
	switch orderType {
	case Market:
		return "market"
	case Limit:
		return "limit"
	case Stop:
		return "stop"
	case StopLimit:
		return "stop_limit"
	default:
		return "unknown"
	}
}

// StringToOrderType converts a string to an order type
func StringToOrderType(orderType string) OrderType {
	switch orderType {
	case "market":
		return Market
	case "limit":
		return Limit
	case "stop":
		return Stop
	case "stop_limit":
		return StopLimit
	default:
		return Limit // Default to limit
	}
}
