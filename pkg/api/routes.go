package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
)

// SetupRoutes configures all API routes
func (s *Server) SetupRoutes() {
	// Initialize handlers
	handlers := CreateHandlers(
		s.marketProcessor,
		s.riskCalculator,
		s.hedgingCalc,
		s.bsPricer,
	)

	// Set up middleware to record metrics
	s.router.Use(func(c *gin.Context) {
		// Start timer
		start := time.Now()

		// Process request
		c.Next()

		// Record metrics after request is processed
		latency := time.Since(start)
		path := c.Request.URL.Path
		method := c.Request.Method
		status := c.Writer.Status()

		// Record API request metrics
		if s.metricsRecorder != nil {
			s.metricsRecorder.RecordAPIRequest(path, method, status, latency)

			if status >= 400 {
				errorType := "client_error"
				if status >= 500 {
					errorType = "server_error"
				}
				s.metricsRecorder.RecordAPIError(path, method, errorType)
			}
		}

		// Log request
		s.log.Infof("%s %s %d %v", method, path, status, latency)
	})

	// Health check
	s.router.GET("/health", handlers.HealthCheckHandler)

	// Metrics endpoint for Prometheus
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API version group
	v1 := s.router.Group("/api/v1")
	{
		// Market data endpoints
		market := v1.Group("/market")
		{
			market.GET("/data/:symbol", handlers.GetMarketDataHandler)
			market.GET("/orderbook/:symbol", handlers.GetOrderBookHandler)
		}

		// Order management endpoints
		orders := v1.Group("/orders")
		{
			orders.POST("", handlers.PlaceOrderHandler)
			orders.GET("/:id", handlers.GetOrderHandler)
			orders.DELETE("/:id", handlers.CancelOrderHandler)
		}

		// Risk analytics endpoints
		risk := v1.Group("/risk")
		{
			risk.POST("/calculate", handlers.CalculateRiskMetricsHandler)
			risk.POST("/stress-test", handlers.RunStressTestHandler)
			risk.POST("/hedging-strategy", handlers.GenerateHedgingStrategyHandler)
			risk.POST("/hedging-strategy/execute", handlers.CreateHedgingStrategyHandler)
		}

		// Derivative pricing endpoints
		derivatives := v1.Group("/derivatives")
		{
			derivatives.POST("/price", handlers.PriceDerivativeHandler)
			derivatives.POST("/greeks", handlers.CalculateGreeksHandler)
			derivatives.POST("/implied-vol", handlers.CalculateImpliedVolHandler)
		}
	}
}

// RegisterCustomHandler registers a custom handler for a specific route
func (s *Server) RegisterCustomHandler(method, path string, handler gin.HandlerFunc) {
	s.router.Handle(method, path, handler)
	s.log.Infof("Registered custom handler for %s %s", method, path)
}

// Health check handler
func (s *Server) healthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": "1.0.0",
	})
}

// RespondJSON sends a JSON response
func RespondJSON(w http.ResponseWriter, status int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(response)
}

// RespondError sends an error response
func RespondError(w http.ResponseWriter, code int, message string) {
	RespondJSON(w, code, map[string]string{"error": message})
}

// parseQueryParams parses common query parameters
func parseQueryParams(r *http.Request) (limit, offset int, sort string, err error) {
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid limit parameter: %v", err)
		}
	} else {
		limit = 100 // Default limit
	}

	offsetStr := r.URL.Query().Get("offset")
	if offsetStr != "" {
		offset, err = strconv.Atoi(offsetStr)
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid offset parameter: %v", err)
		}
	}

	sort = r.URL.Query().Get("sort")

	return limit, offset, sort, nil
}

// Market Data Route Handlers

// handleGetSymbols returns all available market symbols
func (s *Server) handleGetSymbols(w http.ResponseWriter, r *http.Request) {
	symbols := s.marketProcessor.GetAvailableSymbols()

	// Extract query for filtering
	query := r.URL.Query().Get("q")
	if query != "" {
		query = strings.ToUpper(query)
		filtered := make([]string, 0)
		for _, symbol := range symbols {
			if strings.Contains(strings.ToUpper(symbol), query) {
				filtered = append(filtered, symbol)
			}
		}
		symbols = filtered
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"symbols": symbols,
		"count":   len(symbols),
	})
}

// handleGetMarketData returns market data for a specific symbol
func (s *Server) handleGetMarketData(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	symbol := vars["symbol"]

	data, found := s.marketProcessor.GetMarketData(symbol)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Market data for symbol %s not found", symbol))
		return
	}

	RespondJSON(w, http.StatusOK, data)
}

// handleGetOrderBook returns the order book for a symbol
func (s *Server) handleGetOrderBook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	symbol := vars["symbol"]

	book, found := s.marketProcessor.GetOrderBook(symbol)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Order book for symbol %s not found", symbol))
		return
	}

	// Get a snapshot of the order book
	snapshot := book.Snapshot()

	RespondJSON(w, http.StatusOK, snapshot)
}

// handleGetOrderBookDepth returns the order book depth up to a specific level
func (s *Server) handleGetOrderBookDepth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	symbol := vars["symbol"]
	levelStr := vars["level"]

	level, err := strconv.Atoi(levelStr)
	if err != nil || level <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid level parameter")
		return
	}

	book, found := s.marketProcessor.GetOrderBook(symbol)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Order book for symbol %s not found", symbol))
		return
	}

	// Get order book depth
	depth := book.GetDepth(level)

	RespondJSON(w, http.StatusOK, depth)
}

// Risk Route Handlers

// handleGetPortfolios returns all portfolios
func (s *Server) handleGetPortfolios(w http.ResponseWriter, r *http.Request) {
	// This would typically fetch from a database
	// For now, we'll return what we have in memory from the risk calculator
	portfolios := make([]*models.Portfolio, 0)

	// Extract query params
	limit, offset, sort, err := parseQueryParams(r)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// In a real implementation, we'd use these parameters
	_ = limit
	_ = offset
	_ = sort

	// For demo purposes, we'll return the first 100 portfolios from the risk calculator
	// This should be replaced with proper pagination and sorting from a database
	count := 0
	for _, portfolio := range s.riskCalculator.GetAllPortfolios() {
		if count >= offset && count < offset+limit {
			portfolios = append(portfolios, portfolio)
		}
		count++
		if len(portfolios) >= limit {
			break
		}
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"portfolios": portfolios,
		"count":      len(portfolios),
		"total":      count,
		"offset":     offset,
		"limit":      limit,
	})
}

// handleCreatePortfolio creates a new portfolio
func (s *Server) handleCreatePortfolio(w http.ResponseWriter, r *http.Request) {
	var portfolio models.Portfolio

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&portfolio); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Validate the portfolio
	if portfolio.Name == "" {
		RespondError(w, http.StatusBadRequest, "Portfolio name is required")
		return
	}

	// Generate an ID if one is not provided
	if portfolio.ID == "" {
		portfolio.ID = generateID()
	}

	// In a real implementation, we'd save to a database
	// For now, we'll just add it to the risk calculator
	s.riskCalculator.UpdatePortfolio(&portfolio)

	// Calculate initial risk for the portfolio
	_, err := s.riskCalculator.CalculateRisk(portfolio.ID)
	if err != nil {
		s.log.Warnf("Failed to calculate risk for new portfolio: %v", err)
	}

	RespondJSON(w, http.StatusCreated, portfolio)
}

// handleGetPortfolio returns a specific portfolio
func (s *Server) handleGetPortfolio(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	portfolio, found := s.riskCalculator.GetPortfolio(id)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	RespondJSON(w, http.StatusOK, portfolio)
}

// handleUpdatePortfolio updates a portfolio
func (s *Server) handleUpdatePortfolio(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if portfolio exists
	_, found := s.riskCalculator.GetPortfolio(id)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	var portfolio models.Portfolio

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&portfolio); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Ensure ID matches path parameter
	portfolio.ID = id

	// Update portfolio in risk calculator
	s.riskCalculator.UpdatePortfolio(&portfolio)

	// Recalculate risk
	_, err := s.riskCalculator.CalculateRisk(id)
	if err != nil {
		s.log.Warnf("Failed to calculate risk for updated portfolio: %v", err)
	}

	RespondJSON(w, http.StatusOK, portfolio)
}

// handleDeletePortfolio deletes a portfolio
func (s *Server) handleDeletePortfolio(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// In a real implementation, we'd delete from a database
	// For now, we'll just remove it from the risk calculator
	if removed := s.riskCalculator.RemovePortfolio(id); !removed {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"message": "Portfolio deleted"})
}

// handleGetPortfolioRisk returns risk metrics for a portfolio
func (s *Server) handleGetPortfolioRisk(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if portfolio exists
	_, found := s.riskCalculator.GetPortfolio(id)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	// Get risk data (check if we have cached results first)
	result, found := s.riskCalculator.GetRiskResult(id)
	if !found {
		// No cached result, calculate risk now
		var err error
		result, err = s.riskCalculator.CalculateRisk(id)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Error calculating risk: %v", err))
			return
		}
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleRunStressTest runs a stress test on a portfolio
func (s *Server) handleRunStressTest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if portfolio exists
	_, found := s.riskCalculator.GetPortfolio(id)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	// Parse stress test scenarios from request
	var requestBody struct {
		Scenarios map[string]map[string]float64 `json:"scenarios"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&requestBody); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Validate scenarios
	if len(requestBody.Scenarios) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one scenario is required")
		return
	}

	// Run stress test
	result, err := s.riskCalculator.RunStressTest(id, requestBody.Scenarios)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Error running stress test: %v", err))
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleGetHedgingStrategy returns hedging strategies for a portfolio
func (s *Server) handleGetHedgingStrategy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if portfolio exists
	_, found := s.riskCalculator.GetPortfolio(id)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	// Parse hedging instruments from query params
	instruments := r.URL.Query()["instrument"]
	if len(instruments) == 0 {
		// Default to some common hedging instruments
		instruments = []string{"SPY", "VIX", "TLT", "GLD"}
	}

	// Calculate hedging strategy
	strategy, err := s.riskCalculator.CalculateHedgingStrategy(id, instruments)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Error calculating hedging strategy: %v", err))
		return
	}

	RespondJSON(w, http.StatusOK, strategy)
}

// Order Route Handlers

// handleGetOrders returns orders filtered by various criteria
func (s *Server) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	// Extract query params for filtering
	limit, offset, sort, err := parseQueryParams(r)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Extract additional filters
	symbol := r.URL.Query().Get("symbol")
	status := r.URL.Query().Get("status")
	side := r.URL.Query().Get("side")

	// In a real implementation, we'd fetch from a database with these filters
	// For now, return a simpler response
	orders := s.marketProcessor.GetOrders(symbol, status, side, limit, offset, sort)

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"orders": orders,
		"count":  len(orders),
	})
}

// handlePlaceOrder places a new order
func (s *Server) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var order models.Order

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&order); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Validate order
	if err := validateOrder(&order); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generate order ID if not provided
	if order.ID == "" {
		order.ID = generateID()
	}

	// Submit order to processor
	result, err := s.marketProcessor.PlaceOrder(&order)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusCreated, result)
}

// handleGetOrder retrieves a specific order
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	order, found := s.marketProcessor.GetOrder(id)
	if !found {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Order with ID %s not found", id))
		return
	}

	RespondJSON(w, http.StatusOK, order)
}

// handleCancelOrder cancels an order
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	err := s.marketProcessor.CancelOrder(id)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"message": "Order canceled"})
}

// handleModifyOrder modifies an existing order
func (s *Server) handleModifyOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var orderUpdate models.OrderUpdate

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&orderUpdate); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Set the ID from the URL
	orderUpdate.OrderID = id

	result, err := s.marketProcessor.ModifyOrder(&orderUpdate)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// Metrics Route Handlers

// handleGetMetrics returns all metrics
func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := s.metricsRecorder.GetAllMetrics()
	RespondJSON(w, http.StatusOK, metrics)
}

// handleGetLatencyMetrics returns latency metrics
func (s *Server) handleGetLatencyMetrics(w http.ResponseWriter, r *http.Request) {
	latencyMetrics := s.metricsRecorder.GetLatencyMetrics()
	RespondJSON(w, http.StatusOK, latencyMetrics)
}

// handleGetThroughputMetrics returns throughput metrics
func (s *Server) handleGetThroughputMetrics(w http.ResponseWriter, r *http.Request) {
	throughputMetrics := s.metricsRecorder.GetThroughputMetrics()
	RespondJSON(w, http.StatusOK, throughputMetrics)
}

// Helper functions

// validateOrder validates an order
func validateOrder(order *models.Order) error {
	if order.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}

	if order.Side != "buy" && order.Side != "sell" {
		return fmt.Errorf("side must be 'buy' or 'sell'")
	}

	if order.Quantity <= 0 {
		return fmt.Errorf("quantity must be greater than 0")
	}

	if order.Type == "limit" && order.Price <= 0 {
		return fmt.Errorf("limit orders must have a price greater than 0")
	}

	return nil
}

// generateID generates a simple ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Route placeholders - these would be implemented with the appropriate logic

func (s *Server) getMarketDataHandler(c *gin.Context) {
	symbol := c.Param("symbol")
	s.log.Infof("Getting market data for %s", symbol)

	// TODO: Implement fetching market data for symbol

	c.JSON(http.StatusOK, gin.H{
		"symbol": symbol,
		"status": "not implemented yet",
	})
}

func (s *Server) getOrderBookHandler(c *gin.Context) {
	symbol := c.Param("symbol")
	s.log.Infof("Getting order book for %s", symbol)

	// TODO: Implement fetching order book

	c.JSON(http.StatusOK, gin.H{
		"symbol": symbol,
		"status": "not implemented yet",
	})
}

func (s *Server) getTradesHandler(c *gin.Context) {
	symbol := c.Param("symbol")
	s.log.Infof("Getting trades for %s", symbol)

	// TODO: Implement fetching trades

	c.JSON(http.StatusOK, gin.H{
		"symbol": symbol,
		"status": "not implemented yet",
	})
}

func (s *Server) getMarketSnapshotHandler(c *gin.Context) {
	s.log.Info("Getting market snapshot")

	// TODO: Implement fetching market snapshot

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) listOrdersHandler(c *gin.Context) {
	s.log.Info("Listing orders")

	// TODO: Implement listing orders

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) getOrderHandler(c *gin.Context) {
	id := c.Param("id")
	s.log.Infof("Getting order %s", id)

	// TODO: Implement fetching order

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": "not implemented yet",
	})
}

func (s *Server) createOrderHandler(c *gin.Context) {
	s.log.Info("Creating order")

	// TODO: Implement creating order

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) updateOrderHandler(c *gin.Context) {
	id := c.Param("id")
	s.log.Infof("Updating order %s", id)

	// TODO: Implement updating order

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": "not implemented yet",
	})
}

func (s *Server) cancelOrderHandler(c *gin.Context) {
	id := c.Param("id")
	s.log.Infof("Cancelling order %s", id)

	// TODO: Implement cancelling order

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": "not implemented yet",
	})
}

func (s *Server) getRiskMetricsHandler(c *gin.Context) {
	portfolioId := c.Param("portfolioId")
	s.log.Infof("Getting risk metrics for portfolio %s", portfolioId)

	// TODO: Implement fetching risk metrics

	c.JSON(http.StatusOK, gin.H{
		"portfolioId": portfolioId,
		"status":      "not implemented yet",
	})
}

func (s *Server) calculateRiskHandler(c *gin.Context) {
	s.log.Info("Calculating risk")

	// TODO: Implement risk calculation

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) getVaRHandler(c *gin.Context) {
	portfolioId := c.Param("portfolioId")
	s.log.Infof("Getting VaR for portfolio %s", portfolioId)

	// TODO: Implement fetching VaR

	c.JSON(http.StatusOK, gin.H{
		"portfolioId": portfolioId,
		"status":      "not implemented yet",
	})
}

func (s *Server) getESHandler(c *gin.Context) {
	portfolioId := c.Param("portfolioId")
	s.log.Infof("Getting ES for portfolio %s", portfolioId)

	// TODO: Implement fetching ES

	c.JSON(http.StatusOK, gin.H{
		"portfolioId": portfolioId,
		"status":      "not implemented yet",
	})
}

func (s *Server) runStressTestHandler(c *gin.Context) {
	s.log.Info("Running stress test")

	// TODO: Implement stress testing

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) createHedgingStrategyHandler(c *gin.Context) {
	s.log.Info("Creating hedging strategy")

	// TODO: Implement hedging strategy creation

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) priceDerivativeHandler(c *gin.Context) {
	s.log.Info("Pricing derivative")

	// TODO: Implement derivative pricing

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) calculateGreeksHandler(c *gin.Context) {
	s.log.Info("Calculating Greeks")

	// TODO: Implement Greeks calculation

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) calculateImpliedVolHandler(c *gin.Context) {
	s.log.Info("Calculating implied volatility")

	// TODO: Implement implied volatility calculation

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}

func (s *Server) websocketHandler(c *gin.Context) {
	s.log.Info("WebSocket connection requested")

	// TODO: Implement WebSocket handling

	c.JSON(http.StatusOK, gin.H{
		"status": "not implemented yet",
	})
}
