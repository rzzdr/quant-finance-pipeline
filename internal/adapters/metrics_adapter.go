package adapters

import (
	"time"

	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
)

// MetricsAdapter adapts *metrics.Recorder to satisfy the MetricsRecorder interface
// that the market processor expects
type MetricsAdapter struct {
	recorder *metrics.Recorder
}

var _ market.MetricsRecorder = (*MetricsAdapter)(nil) // Ensure MetricsAdapter implements market.MetricsRecorder

// NewMetricsAdapter creates a new MetricsAdapter
func NewMetricsAdapter(recorder *metrics.Recorder) *MetricsAdapter {
	return &MetricsAdapter{
		recorder: recorder,
	}
}

// RecordAPIRequest implements metrics.Recorder interface
func (a *MetricsAdapter) RecordAPIRequest(method, path string, status int, latency time.Duration) {
	if a.recorder != nil {
		a.recorder.RecordAPIRequest(method, path, status, latency)
	}
}

// RecordMarketDataUpdate implements metrics.Recorder interface
func (a *MetricsAdapter) RecordMarketDataUpdate(symbol, dataType string, latency time.Duration) {
	if a.recorder != nil {
		a.recorder.RecordMarketDataUpdate(symbol, dataType, latency)
	}
}

// RecordOrderBookUpdate implements metrics.Recorder interface
func (a *MetricsAdapter) RecordOrderBookUpdate(symbol string, side string, size int) {
	if a.recorder != nil {
		a.recorder.RecordOrderBookUpdate(symbol, side, size)
	}
}

// RecordOrderBookImbalance implements metrics.Recorder interface
func (a *MetricsAdapter) RecordOrderBookImbalance(symbol string, imbalance float64) {
	if a.recorder != nil {
		a.recorder.RecordOrderBookImbalance(symbol, imbalance)
	}
}

// RecordTrade implements metrics.Recorder interface
func (a *MetricsAdapter) RecordTrade(symbol string, size, price float64) {
	if a.recorder != nil {
		a.recorder.RecordTrade(symbol, size, price)
	}
}

// RecordRiskCalculation implements metrics.Recorder interface
func (a *MetricsAdapter) RecordRiskCalculation(calcType, portfolioID string, latency time.Duration) {
	if a.recorder != nil {
		a.recorder.RecordRiskCalculation(calcType, portfolioID, latency)
	}
}

// RecordVaR implements metrics.Recorder interface
func (a *MetricsAdapter) RecordVaR(portfolioID string, confidenceLevel, horizon string, value float64) {
	if a.recorder != nil {
		a.recorder.RecordVaR(portfolioID, confidenceLevel, horizon, value)
	}
}

// RecordES implements metrics.Recorder interface
func (a *MetricsAdapter) RecordES(portfolioID string, confidenceLevel, horizon string, value float64) {
	if a.recorder != nil {
		a.recorder.RecordES(portfolioID, confidenceLevel, horizon, value)
	}
}

// RecordKafkaLag implements metrics.Recorder interface
func (a *MetricsAdapter) RecordKafkaLag(topic, groupID string, lag int64) {
	if a.recorder != nil {
		a.recorder.RecordKafkaLag(topic, groupID, lag)
	}
}

// RecordMemoryUsage implements metrics.Recorder interface
func (a *MetricsAdapter) RecordMemoryUsage(bytesUsed uint64) {
	if a.recorder != nil {
		a.recorder.RecordMemoryUsage(bytesUsed)
	}
}

// RecordGoroutineCount implements metrics.Recorder interface
func (a *MetricsAdapter) RecordGoroutineCount(count int) {
	if a.recorder != nil {
		a.recorder.RecordGoroutineCount(count)
	}
}

// RecordOrderProcessed implements market.MetricsRecorder interface
func (a *MetricsAdapter) RecordOrderProcessed(symbol, orderType, side string) {
	if a.recorder != nil {
		a.recorder.RecordOrderProcessed(symbol, orderType, side)
	}
}

// RecordOrderFilled implements market.MetricsRecorder interface
func (a *MetricsAdapter) RecordOrderFilled(symbol, orderType, side string) {
	if a.recorder != nil {
		a.recorder.RecordOrderFilled(symbol, orderType, side)
	}
}
