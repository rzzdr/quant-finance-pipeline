package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Recorder handles metrics recording and exposure
type Recorder struct {
	// API metrics
	apiRequestCounter   *prometheus.CounterVec
	apiLatencyHistogram *prometheus.HistogramVec

	// Market data metrics
	marketDataUpdatesCounter *prometheus.CounterVec
	marketDataLatency        *prometheus.HistogramVec

	// Order book metrics
	orderBookUpdateCounter  *prometheus.CounterVec
	orderBookSizeGauge      *prometheus.GaugeVec
	orderBookImbalanceGauge *prometheus.GaugeVec

	// Trade metrics
	tradeCounter       *prometheus.CounterVec
	tradeVolumeCounter *prometheus.CounterVec
	tradeSizeHistogram *prometheus.HistogramVec

	// Risk metrics
	riskCalcCounter *prometheus.CounterVec
	riskCalcLatency *prometheus.HistogramVec
	varGauge        *prometheus.GaugeVec
	esGauge         *prometheus.GaugeVec

	// System metrics
	kafkaLagGauge       *prometheus.GaugeVec
	memoryUsageGauge    prometheus.Gauge
	goroutineCountGauge prometheus.Gauge
}

// NewRecorder creates a new metrics recorder
func NewRecorder() *Recorder {
	// Create and register all metrics
	return &Recorder{
		// API metrics
		apiRequestCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "qf_api_requests_total",
				Help: "The total number of API requests",
			},
			[]string{"method", "path", "status"},
		),
		apiLatencyHistogram: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "qf_api_latency_seconds",
				Help:    "API request latency distribution",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // From 1ms to ~16s
			},
			[]string{"method", "path"},
		),

		// Market data metrics
		marketDataUpdatesCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "qf_market_data_updates_total",
				Help: "The total number of market data updates",
			},
			[]string{"symbol", "type"},
		),
		marketDataLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "qf_market_data_latency_milliseconds",
				Help:    "Market data processing latency in milliseconds",
				Buckets: prometheus.ExponentialBuckets(0.1, 2, 15), // From 0.1ms to ~1.6s
			},
			[]string{"symbol", "type"},
		),

		// Order book metrics
		orderBookUpdateCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "qf_orderbook_updates_total",
				Help: "The total number of order book updates",
			},
			[]string{"symbol", "side"},
		),
		orderBookSizeGauge: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "qf_orderbook_size",
				Help: "Number of orders in the order book",
			},
			[]string{"symbol", "side"},
		),
		orderBookImbalanceGauge: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "qf_orderbook_imbalance",
				Help: "Order book imbalance (bid vs ask volume)",
			},
			[]string{"symbol"},
		),

		// Trade metrics
		tradeCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "qf_trades_total",
				Help: "The total number of trades",
			},
			[]string{"symbol"},
		),
		tradeVolumeCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "qf_trade_volume_total",
				Help: "The total volume of trades",
			},
			[]string{"symbol"},
		),
		tradeSizeHistogram: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "qf_trade_size",
				Help:    "Trade size distribution",
				Buckets: prometheus.ExponentialBuckets(1, 10, 10), // From 1 to 1B
			},
			[]string{"symbol"},
		),

		// Risk metrics
		riskCalcCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "qf_risk_calculations_total",
				Help: "The total number of risk calculations",
			},
			[]string{"type", "portfolio_id"},
		),
		riskCalcLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "qf_risk_calc_latency_seconds",
				Help:    "Risk calculation latency in seconds",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // From 10ms to ~40s
			},
			[]string{"type", "portfolio_id"},
		),
		varGauge: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "qf_var_value",
				Help: "Value at Risk (VaR)",
			},
			[]string{"portfolio_id", "confidence_level", "horizon"},
		),
		esGauge: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "qf_es_value",
				Help: "Expected Shortfall (ES)",
			},
			[]string{"portfolio_id", "confidence_level", "horizon"},
		),

		// System metrics
		kafkaLagGauge: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "qf_kafka_consumer_lag",
				Help: "Kafka consumer lag (messages)",
			},
			[]string{"topic", "group_id"},
		),
		memoryUsageGauge: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "qf_memory_usage_bytes",
				Help: "Memory usage of the application in bytes",
			},
		),
		goroutineCountGauge: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "qf_goroutine_count",
				Help: "Number of goroutines",
			},
		),
	}
}

// RecordAPIRequest records metrics for an API request
func (r *Recorder) RecordAPIRequest(method, path string, status int, latency time.Duration) {
	statusStr := string(status)
	r.apiRequestCounter.WithLabelValues(method, path, statusStr).Inc()
	r.apiLatencyHistogram.WithLabelValues(method, path).Observe(latency.Seconds())
}

// RecordMarketDataUpdate records metrics for a market data update
func (r *Recorder) RecordMarketDataUpdate(symbol, dataType string, latency time.Duration) {
	r.marketDataUpdatesCounter.WithLabelValues(symbol, dataType).Inc()
	r.marketDataLatency.WithLabelValues(symbol, dataType).Observe(float64(latency.Milliseconds()))
}

// RecordOrderBookUpdate records metrics for an order book update
func (r *Recorder) RecordOrderBookUpdate(symbol string, side string, size int) {
	r.orderBookUpdateCounter.WithLabelValues(symbol, side).Inc()
	r.orderBookSizeGauge.WithLabelValues(symbol, side).Set(float64(size))
}

// RecordOrderBookImbalance records the current order book imbalance
func (r *Recorder) RecordOrderBookImbalance(symbol string, imbalance float64) {
	r.orderBookImbalanceGauge.WithLabelValues(symbol).Set(imbalance)
}

// RecordTrade records metrics for a trade
func (r *Recorder) RecordTrade(symbol string, size, price float64) {
	r.tradeCounter.WithLabelValues(symbol).Inc()
	r.tradeVolumeCounter.WithLabelValues(symbol).Add(size)
	r.tradeSizeHistogram.WithLabelValues(symbol).Observe(size)
}

// RecordRiskCalculation records metrics for a risk calculation
func (r *Recorder) RecordRiskCalculation(calcType, portfolioID string, latency time.Duration) {
	r.riskCalcCounter.WithLabelValues(calcType, portfolioID).Inc()
	r.riskCalcLatency.WithLabelValues(calcType, portfolioID).Observe(latency.Seconds())
}

// RecordVaR records the current VaR value
func (r *Recorder) RecordVaR(portfolioID string, confidenceLevel, horizon string, value float64) {
	r.varGauge.WithLabelValues(portfolioID, confidenceLevel, horizon).Set(value)
}

// RecordES records the current ES value
func (r *Recorder) RecordES(portfolioID string, confidenceLevel, horizon string, value float64) {
	r.esGauge.WithLabelValues(portfolioID, confidenceLevel, horizon).Set(value)
}

// RecordKafkaLag records the current consumer lag for a topic
func (r *Recorder) RecordKafkaLag(topic, groupID string, lag int64) {
	r.kafkaLagGauge.WithLabelValues(topic, groupID).Set(float64(lag))
}

// RecordMemoryUsage records the current memory usage
func (r *Recorder) RecordMemoryUsage(bytesUsed uint64) {
	r.memoryUsageGauge.Set(float64(bytesUsed))
}

// RecordGoroutineCount records the current number of goroutines
func (r *Recorder) RecordGoroutineCount(count int) {
	r.goroutineCountGauge.Set(float64(count))
}
