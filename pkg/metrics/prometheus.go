package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

var (
	// Metrics for market data processing
	marketDataProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "market_data_processed_total",
		Help: "The total number of processed market data items",
	}, []string{"symbol", "source"})

	marketDataProcessingTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "market_data_processing_duration_seconds",
		Help:    "The time taken to process market data",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // Start at 1ms with 10 buckets, each 2x the previous
	}, []string{"symbol", "source"})

	marketDataLag = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "market_data_lag_seconds",
		Help: "The lag between market data timestamp and processing time",
	}, []string{"symbol", "source"})

	// Metrics for order processing
	ordersProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_processed_total",
		Help: "The total number of processed orders",
	}, []string{"symbol", "type", "side"})

	orderProcessingTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "order_processing_duration_seconds",
		Help:    "The time taken to process an order",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
	}, []string{"symbol", "type", "side"})

	ordersFilled = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_filled_total",
		Help: "The total number of filled orders",
	}, []string{"symbol", "type", "side"})

	// Metrics for risk calculations
	riskCalculationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "risk_calculations_total",
		Help: "The total number of risk calculations performed",
	}, []string{"portfolio_id", "calculation_type"})

	riskCalculationTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "risk_calculation_duration_seconds",
		Help:    "The time taken to perform risk calculations",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
	}, []string{"portfolio_id", "calculation_type"})

	// System metrics
	goroutinesGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goroutines_total",
		Help: "The total number of goroutines",
	})

	memoryUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "memory_usage_bytes",
		Help: "Current memory usage of the application",
	})

	// API metrics
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests processed",
	}, []string{"method", "endpoint", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
	}, []string{"method", "endpoint"})
)

// PrometheusServer is a server that exposes Prometheus metrics
type PrometheusServer struct {
	server *http.Server
	log    *logger.Logger
}

// NewPrometheusServer creates a new Prometheus metrics server
func NewPrometheusServer(port int) *PrometheusServer {
	log := logger.GetLogger("metrics.prometheus")
	addr := fmt.Sprintf(":%d", port)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return &PrometheusServer{
		server: server,
		log:    log,
	}
}

// Start starts the Prometheus metrics server
func (p *PrometheusServer) Start() error {
	p.log.Infof("Starting Prometheus metrics server on %s", p.server.Addr)
	return p.server.ListenAndServe()
}

// Stop stops the Prometheus metrics server
func (p *PrometheusServer) Stop() error {
	p.log.Info("Stopping Prometheus metrics server")
	return p.server.Close()
}

// RecordMarketDataProcessed records a processed market data item
func RecordMarketDataProcessed(symbol, source string) {
	marketDataProcessed.WithLabelValues(symbol, source).Inc()
}

// RecordMarketDataProcessingTime records the time taken to process market data
func RecordMarketDataProcessingTime(symbol, source string, duration time.Duration) {
	marketDataProcessingTime.WithLabelValues(symbol, source).Observe(duration.Seconds())
}

// RecordMarketDataLag records the lag between market data timestamp and processing time
func RecordMarketDataLag(symbol, source string, dataTimestamp time.Time) {
	lag := time.Since(dataTimestamp).Seconds()
	marketDataLag.WithLabelValues(symbol, source).Set(lag)
}

// RecordOrderProcessed records a processed order
func RecordOrderProcessed(symbol, orderType, side string) {
	ordersProcessed.WithLabelValues(symbol, orderType, side).Inc()
}

// RecordOrderProcessingTime records the time taken to process an order
func RecordOrderProcessingTime(symbol, orderType, side string, duration time.Duration) {
	orderProcessingTime.WithLabelValues(symbol, orderType, side).Observe(duration.Seconds())
}

// RecordOrderFilled records a filled order
func RecordOrderFilled(symbol, orderType, side string) {
	ordersFilled.WithLabelValues(symbol, orderType, side).Inc()
}

// RecordRiskCalculation records a risk calculation
func RecordRiskCalculation(portfolioID, calculationType string) {
	riskCalculationsTotal.WithLabelValues(portfolioID, calculationType).Inc()
}

// RecordRiskCalculationTime records the time taken for a risk calculation
func RecordRiskCalculationTime(portfolioID, calculationType string, duration time.Duration) {
	riskCalculationTime.WithLabelValues(portfolioID, calculationType).Observe(duration.Seconds())
}

// RecordHTTPRequest records an HTTP request
func RecordHTTPRequest(method, endpoint string, status int, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, endpoint, fmt.Sprintf("%d", status)).Inc()
	httpRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// UpdateSystemMetrics updates system-level metrics
func UpdateSystemMetrics(numGoroutines int, memBytes float64) {
	goroutinesGauge.Set(float64(numGoroutines))
	memoryUsage.Set(memBytes)
}
