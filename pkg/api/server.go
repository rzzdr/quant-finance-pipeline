package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/internal/risk"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Config holds the configuration for the API server
type Config struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Server represents the API server
type Server struct {
	config          Config
	router          *mux.Router
	httpServer      *http.Server
	marketProcessor market.Processor // Changed from pointer to interface to interface
	riskCalculator  *risk.Calculator
	metricsRecorder *metrics.Recorder
	log             *logger.Logger
}

// NewServer creates a new API server
func NewServer(config Config, marketProcessor market.Processor, riskCalculator *risk.Calculator, metricsRecorder *metrics.Recorder) *Server {
	// Apply defaults if needed
	if config.ReadTimeout <= 0 {
		config.ReadTimeout = 10 * time.Second
	}

	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 10 * time.Second
	}

	server := &Server{
		config:          config,
		router:          mux.NewRouter(),
		marketProcessor: marketProcessor,
		riskCalculator:  riskCalculator,
		metricsRecorder: metricsRecorder,
		log:             logger.GetLogger("api.server"),
	}

	// Setup routes
	server.setupRoutes()

	return server
}

// Start starts the API server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	s.log.Infof("Starting API server on %s", addr)

	return s.httpServer.ListenAndServe()
}

// Stop stops the API server gracefully
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		s.log.Info("Stopping API server")
		return s.httpServer.Shutdown(ctx)
	}

	return nil
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Apply common middleware
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
	s.router.Use(s.recoveryMiddleware)

	// API version prefix
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// Health check endpoint
	api.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Metrics endpoint for Prometheus
	s.router.Handle("/metrics", promhttp.Handler())

	// Market data endpoints
	market := api.PathPrefix("/market").Subrouter()
	market.HandleFunc("/symbols", s.handleGetSymbols).Methods("GET")
	market.HandleFunc("/data/{symbol}", s.handleGetMarketData).Methods("GET")
	market.HandleFunc("/orderbook/{symbol}", s.handleGetOrderBook).Methods("GET")
	market.HandleFunc("/orderbook/{symbol}/depth/{level}", s.handleGetOrderBookDepth).Methods("GET")

	// Risk endpoints
	risk := api.PathPrefix("/risk").Subrouter()
	risk.HandleFunc("/portfolios", s.handleGetPortfolios).Methods("GET")
	risk.HandleFunc("/portfolios", s.handleCreatePortfolio).Methods("POST")
	risk.HandleFunc("/portfolios/{id}", s.handleGetPortfolio).Methods("GET")
	risk.HandleFunc("/portfolios/{id}", s.handleUpdatePortfolio).Methods("PUT")
	risk.HandleFunc("/portfolios/{id}", s.handleDeletePortfolio).Methods("DELETE")
	risk.HandleFunc("/portfolios/{id}/risk", s.handleGetPortfolioRisk).Methods("GET")
	risk.HandleFunc("/portfolios/{id}/stresstest", s.handleRunStressTest).Methods("POST")
	risk.HandleFunc("/portfolios/{id}/hedge", s.handleGetHedgingStrategy).Methods("GET")

	// Orders endpoints
	orders := api.PathPrefix("/orders").Subrouter()
	orders.HandleFunc("", s.handleGetOrders).Methods("GET")
	orders.HandleFunc("", s.handlePlaceOrder).Methods("POST")
	orders.HandleFunc("/{id}", s.handleGetOrder).Methods("GET")
	orders.HandleFunc("/{id}", s.handleCancelOrder).Methods("DELETE")
	orders.HandleFunc("/{id}/modify", s.handleModifyOrder).Methods("PUT")

	// Documentation
	s.router.PathPrefix("/docs/").Handler(http.StripPrefix("/docs/", http.FileServer(http.Dir("./docs"))))

	// Add a catch-all route for 404s
	s.router.NotFoundHandler = http.HandlerFunc(s.handleNotFound)
}

// Middleware functions can be implemented here
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response wrapper to capture status code
		wrw := NewResponseWriter(w)

		// Call the next handler
		next.ServeHTTP(wrw, r)

		// Log the request after it's handled
		s.log.Infof(
			"%s %s %s %d %s",
			r.RemoteAddr,
			r.Method,
			r.URL.Path,
			wrw.statusCode,
			time.Since(start),
		)
	})
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response wrapper to capture status code
		wrw := NewResponseWriter(w)

		// Call the next handler
		next.ServeHTTP(wrw, r)

		// Record metrics
		s.metricsRecorder.RecordAPIRequest(r.Method, r.URL.Path, wrw.statusCode, time.Since(start))
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.log.Errorf("Panic in API handler: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// ResponseWriter is a wrapper around http.ResponseWriter that captures status code
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// NewResponseWriter creates a new ResponseWriter
func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default to 200 OK
	}
}

// WriteHeader captures the status code and calls the underlying WriteHeader
func (w *ResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Handler functions for endpoints are implemented in routes.go
