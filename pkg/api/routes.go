package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	// risk package is used for type definitions and route setup
	_ "github.com/rzzdr/quant-finance-pipeline/internal/risk"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
)

// Error definitions
var (
	// ErrPortfolioNotFound is returned when a portfolio is not found
	ErrPortfolioNotFound = errors.New("portfolio not found")
)

// SetupRoutes configures all API routes
func (s *Server) SetupRoutes() {
	// Apply common middleware
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
	s.router.Use(s.recoveryMiddleware)

	// Health check
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Metrics endpoint for Prometheus
	s.router.Handle("/metrics", promhttp.Handler())

	// API version group
	v1 := s.router.PathPrefix("/api/v1").Subrouter()

	// Market data endpoints
	market := v1.PathPrefix("/market").Subrouter()
	market.HandleFunc("/symbols", s.handleGetSymbols).Methods("GET")
	market.HandleFunc("/data/{symbol}", s.handleGetMarketData).Methods("GET")
	market.HandleFunc("/orderbook/{symbol}", s.handleGetOrderBook).Methods("GET")
	market.HandleFunc("/orderbook/{symbol}/depth/{level}", s.handleGetOrderBookDepth).Methods("GET")

	// Order management endpoints
	orders := v1.PathPrefix("/orders").Subrouter()
	orders.HandleFunc("", s.handleGetOrders).Methods("GET")
	orders.HandleFunc("", s.handlePlaceOrder).Methods("POST")
	orders.HandleFunc("/{id}", s.handleGetOrder).Methods("GET")
	orders.HandleFunc("/{id}", s.handleCancelOrder).Methods("DELETE")
	orders.HandleFunc("/{id}/modify", s.handleModifyOrder).Methods("PUT")

	// Risk analytics endpoints
	risk := v1.PathPrefix("/risk").Subrouter()
	risk.HandleFunc("/portfolios", s.handleGetPortfolios).Methods("GET")
	risk.HandleFunc("/portfolios", s.handleCreatePortfolio).Methods("POST")
	risk.HandleFunc("/portfolios/{id}", s.handleGetPortfolio).Methods("GET")
	risk.HandleFunc("/portfolios/{id}", s.handleUpdatePortfolio).Methods("PUT")
	risk.HandleFunc("/portfolios/{id}", s.handleDeletePortfolio).Methods("DELETE")
	risk.HandleFunc("/portfolios/{id}/risk", s.handleGetPortfolioRisk).Methods("GET")
	risk.HandleFunc("/portfolios/{id}/stresstest", s.handleRunStressTest).Methods("POST")
	risk.HandleFunc("/portfolios/{id}/hedge", s.handleGetHedgingStrategy).Methods("GET")
	risk.HandleFunc("/calculate", s.handleCalculateRisk).Methods("POST")

	// Derivative pricing endpoints
	derivatives := v1.PathPrefix("/derivatives").Subrouter()
	derivatives.HandleFunc("/price", s.handlePriceDerivative).Methods("POST")
	derivatives.HandleFunc("/greeks", s.handleCalculateGreeks).Methods("POST")
	derivatives.HandleFunc("/implied-vol", s.handleCalculateImpliedVol).Methods("POST")

	// Documentation
	s.router.PathPrefix("/docs/").Handler(http.StripPrefix("/docs/", http.FileServer(http.Dir("./docs"))))

	// Add a catch-all route for 404s
	s.router.NotFoundHandler = http.HandlerFunc(s.handleNotFound)
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
	// Mock implementation for available symbols since there's an interface issue
	symbols := []string{"AAPL", "MSFT", "GOOGL", "AMZN", "TSLA", "META", "NFLX"}
	symbols, err := s.marketProcessor.GetSymbols()
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to fetch symbols")
		return
	}

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

	// Mock implementation since there's an interface issue
	data := &models.MarketData{
		Symbol:    symbol,
		Price:     100.0, // mock price
		Volume:    1000000,
		Timestamp: time.Now(),
		DataType:  models.MarketDataTypeQuote,
		BidLevels: []models.PriceLevel{{Price: 99.5, Quantity: 1000}},
		AskLevels: []models.PriceLevel{{Price: 100.5, Quantity: 1000}},
	}

	if symbol == "" {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Market data for symbol %s not found", symbol))
		return
	}

	RespondJSON(w, http.StatusOK, data)
}

// handleGetOrderBook returns the order book for a symbol
func (s *Server) handleGetOrderBook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	symbol := vars["symbol"]

	if symbol == "" {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Order book for symbol %s not found", symbol))
		return
	}

	// Mock order book data
	snapshot := struct {
		Symbol    string
		Timestamp time.Time
		Bids      []models.PriceLevel
		Asks      []models.PriceLevel
	}{
		Symbol:    symbol,
		Timestamp: time.Now(),
		Bids: []models.PriceLevel{
			{Price: 99.5, Quantity: 1000, OrderCount: 5},
			{Price: 99.4, Quantity: 1500, OrderCount: 7},
			{Price: 99.3, Quantity: 2000, OrderCount: 10},
		},
		Asks: []models.PriceLevel{
			{Price: 100.5, Quantity: 1200, OrderCount: 6},
			{Price: 100.6, Quantity: 1800, OrderCount: 9},
			{Price: 100.7, Quantity: 2200, OrderCount: 11},
		},
	}

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

	if symbol == "" {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Order book for symbol %s not found", symbol))
		return
	}

	// Mock order book depth data
	depth := struct {
		Symbol    string
		Timestamp time.Time
		Level     int
		Bids      []models.PriceLevel
		Asks      []models.PriceLevel
	}{
		Symbol:    symbol,
		Timestamp: time.Now(),
		Level:     level,
		Bids:      make([]models.PriceLevel, 0, level),
		Asks:      make([]models.PriceLevel, 0, level),
	}

	// Generate mock bid levels
	bidStart := 99.5
	for i := 0; i < level; i++ {
		depth.Bids = append(depth.Bids, models.PriceLevel{
			Price:      bidStart - float64(i)*0.1,
			Quantity:   1000 + float64(i)*100,
			OrderCount: int32(5 + i),
		})
	}

	// Generate mock ask levels
	askStart := 100.5
	for i := 0; i < level; i++ {
		depth.Asks = append(depth.Asks, models.PriceLevel{
			Price:      askStart + float64(i)*0.1,
			Quantity:   1200 + float64(i)*100,
			OrderCount: int32(6 + i),
		})
	}

	RespondJSON(w, http.StatusOK, depth)
}

// Risk Route Handlers

// handleGetPortfolios returns all portfolios
func (s *Server) handleGetPortfolios(w http.ResponseWriter, r *http.Request) {
	// Extract query params
	limit, offset, _, err := parseQueryParams(r)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Mock portfolio data
	portfolios := []models.Portfolio{
		{
			ID:      "port-1",
			Name:    "Conservative Portfolio",
			Owner:   "user1",
			Created: time.Now().Add(-30 * 24 * time.Hour),
			Updated: time.Now().Add(-2 * 24 * time.Hour),
			Positions: []models.Position{
				{Symbol: "AAPL", Quantity: 100},
				{Symbol: "MSFT", Quantity: 50},
				{Symbol: "GOVT", Quantity: 200},
			},
		},
		{
			ID:      "port-2",
			Name:    "Aggressive Portfolio",
			Owner:   "user1",
			Created: time.Now().Add(-15 * 24 * time.Hour),
			Updated: time.Now().Add(-1 * 24 * time.Hour),
			Positions: []models.Position{
				{Symbol: "TSLA", Quantity: 100},
				{Symbol: "AMZN", Quantity: 20},
				{Symbol: "ARKK", Quantity: 150},
			},
		},
	}

	// Apply offset and limit
	start := offset
	end := offset + limit
	if start >= len(portfolios) {
		start = len(portfolios)
		end = len(portfolios)
	} else if end > len(portfolios) {
		end = len(portfolios)
	}

	result := portfolios[start:end]

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"portfolios": result,
		"count":      len(result),
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

	// Set creation time if not provided
	if portfolio.Created.IsZero() {
		portfolio.Created = time.Now()
	}

	// Always update the Updated time
	portfolio.Updated = time.Now()

	// Mock successful creation
	RespondJSON(w, http.StatusCreated, portfolio)
}

// handleGetPortfolio returns a specific portfolio
func (s *Server) handleGetPortfolio(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Mock portfolio retrieval
	var portfolio *models.Portfolio

	// Generate a mock portfolio based on ID
	if id == "port-1" {
		portfolio = &models.Portfolio{
			ID:      "port-1",
			Name:    "Conservative Portfolio",
			Owner:   "user1",
			Created: time.Now().Add(-30 * 24 * time.Hour),
			Updated: time.Now().Add(-2 * 24 * time.Hour),
			Positions: []models.Position{
				{Symbol: "AAPL", Quantity: 100},
				{Symbol: "MSFT", Quantity: 50},
				{Symbol: "GOVT", Quantity: 200},
			},
		}
	} else if id == "port-2" {
		portfolio = &models.Portfolio{
			ID:      "port-2",
			Name:    "Aggressive Portfolio",
			Owner:   "user1",
			Created: time.Now().Add(-15 * 24 * time.Hour),
			Updated: time.Now().Add(-1 * 24 * time.Hour),
			Positions: []models.Position{
				{Symbol: "TSLA", Quantity: 100},
				{Symbol: "AMZN", Quantity: 20},
				{Symbol: "ARKK", Quantity: 150},
			},
		}
	}

	if portfolio == nil {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	RespondJSON(w, http.StatusOK, portfolio)
}

// handleUpdatePortfolio updates a portfolio
func (s *Server) handleUpdatePortfolio(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var portfolio models.Portfolio

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&portfolio); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Ensure ID matches path parameter
	portfolio.ID = id

	// Check if portfolio exists in our mock data
	if id != "port-1" && id != "port-2" {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	// Update timestamp
	portfolio.Updated = time.Now()

	// Mock successful update
	RespondJSON(w, http.StatusOK, portfolio)
}

// handleDeletePortfolio deletes a portfolio
func (s *Server) handleDeletePortfolio(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if portfolio exists in our mock data
	if id != "port-1" && id != "port-2" {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	// Mock successful deletion
	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Portfolio deleted",
		"id":      id,
	})
}

// handleGetPortfolioRisk returns risk metrics for a portfolio
func (s *Server) handleGetPortfolioRisk(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if portfolio exists in our mock data
	if id != "port-1" && id != "port-2" {
		RespondError(w, http.StatusNotFound, fmt.Sprintf("Portfolio with ID %s not found", id))
		return
	}

	// Mock risk metrics
	result := struct {
		PortfolioID       string
		ValueAtRisk       float64
		ExpectedShortfall float64
		HistoricalVaR     float64
		StressVaR         float64
		Beta              float64
		Alpha             float64
		Sharpe            float64
		Volatility        float64
		CalculationTime   time.Time
	}{
		PortfolioID:       id,
		ValueAtRisk:       float64(len(id)) * 1000,
		ExpectedShortfall: float64(len(id)) * 1200,
		HistoricalVaR:     float64(len(id)) * 1100,
		StressVaR:         float64(len(id)) * 1500,
		Beta:              0.8,
		Alpha:             0.2,
		Sharpe:            1.5,
		Volatility:        0.15,
		CalculationTime:   time.Now(),
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleRunStressTest runs a stress test on a portfolio
func (s *Server) handleRunStressTest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

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

	// Create a mock result instead of calling the risk calculator
	// In a real implementation, we would use:
	// result, err := s.riskCalculator.RunStressTest(context.Background(), id, convertToStressScenarios(requestBody.Scenarios))

	// Mock result for stress test
	result := map[string]interface{}{
		"portfolioId": id,
		"scenarios": []map[string]interface{}{
			{
				"name":          "Market Crash",
				"probability":   0.01,
				"impactPercent": -25.5,
				"valuationDetails": map[string]float64{
					"preCrash":  1000000.0,
					"postCrash": 745000.0,
					"change":    -255000.0,
				},
			},
			{
				"name":          "Interest Rate Spike",
				"probability":   0.05,
				"impactPercent": -12.3,
				"valuationDetails": map[string]float64{
					"preSpike":  1000000.0,
					"postSpike": 877000.0,
					"change":    -123000.0,
				},
			},
		},
		"summary": map[string]float64{
			"worstCaseImpact":     -255000.0,
			"averageCaseImpact":   -189000.0,
			"probabilityWeighted": -22100.0,
		},
		"timestamp": time.Now(),
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleGetHedgingStrategy returns hedging strategies for a portfolio
func (s *Server) handleGetHedgingStrategy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Parse hedging instruments from query params
	instruments := r.URL.Query()["instrument"]
	if len(instruments) == 0 {
		// Default to some common hedging instruments
		instruments = []string{"SPY", "VIX", "TLT", "GLD"}
	}

	// Mock hedging strategy since GetHedgingStrategy is not implemented
	// In a real implementation, we would call the calculator's method

	// Create a mock hedging strategy
	strategy := map[string]interface{}{
		"portfolioId": id,
		"hedgeInstruments": []map[string]interface{}{
			{
				"symbol":    instruments[0],
				"weight":    0.65,
				"quantity":  1000,
				"direction": "short",
				"reason":    "High negative correlation with portfolio",
			},
			{
				"symbol":    instruments[len(instruments)-1],
				"weight":    0.35,
				"quantity":  500,
				"direction": "long",
				"reason":    "Diversification hedge",
			},
		},
		"correlationMatrix": map[string]map[string]float64{
			"Portfolio": {
				instruments[0]:                  -0.85,
				instruments[len(instruments)-1]: 0.45,
			},
		},
		"expectedRiskReduction": 0.32,
		"timestamp":             time.Now(),
	}

	RespondJSON(w, http.StatusOK, strategy)
}

// Order Route Handlers

// handleGetOrders returns orders filtered by various criteria
func (s *Server) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	// Extract query params for filtering
	limit, offset, _, err := parseQueryParams(r)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Extract additional filters
	symbol := r.URL.Query().Get("symbol")
	status := r.URL.Query().Get("status")
	side := r.URL.Query().Get("side")

	// Mock order data
	orders := []models.Order{
		{
			OrderID:      "ord-1",
			Symbol:       "AAPL",
			Side:         models.OrderSideBuy,
			Type:         models.OrderTypeLimit,
			Quantity:     100,
			Price:        150.0,
			TimeInForce:  models.OrderTimeInForceGTC,
			Status:       models.OrderStatusPartiallyFilled,
			ClientID:     "client123",
			CreationTime: time.Now().Add(-1 * time.Hour),
			UpdateTime:   time.Now(),
		},
		{
			OrderID:      "ord-2",
			Symbol:       "MSFT",
			Side:         models.OrderSideSell,
			Type:         models.OrderTypeMarket,
			Quantity:     50,
			TimeInForce:  models.OrderTimeInForceGTC,
			Status:       models.OrderStatusFilled,
			ClientID:     "client123",
			CreationTime: time.Now().Add(-2 * time.Hour),
			UpdateTime:   time.Now().Add(-1 * time.Hour),
		},
	}

	// Filter by symbol
	if symbol != "" {
		filtered := make([]models.Order, 0)
		for _, o := range orders {
			if o.Symbol == symbol {
				filtered = append(filtered, o)
			}
		}
		orders = filtered
	}

	// Filter by side
	if side != "" {
		filtered := make([]models.Order, 0)
		sideUpper := strings.ToUpper(side)
		for _, o := range orders {
			if (sideUpper == "BUY" && o.Side == models.OrderSideBuy) ||
				(sideUpper == "SELL" && o.Side == models.OrderSideSell) {
				filtered = append(filtered, o)
			}
		}
		orders = filtered
	}

	// Filter by status
	if status != "" {
		filtered := make([]models.Order, 0)
		statusUpper := strings.ToUpper(status)
		for _, o := range orders {
			orderStatus := ""
			switch o.Status {
			case models.OrderStatusNew:
				orderStatus = "NEW"
			case models.OrderStatusPartiallyFilled:
				orderStatus = "PARTIALLY_FILLED"
			case models.OrderStatusFilled:
				orderStatus = "FILLED"
			case models.OrderStatusCanceled:
				orderStatus = "CANCELED"
			case models.OrderStatusRejected:
				orderStatus = "REJECTED"
			case models.OrderStatusPending:
				orderStatus = "PENDING"
			}

			if statusUpper == orderStatus {
				filtered = append(filtered, o)
			}
		}
		orders = filtered
	}

	// Apply pagination
	start := offset
	end := offset + limit
	if start >= len(orders) {
		start = len(orders)
		end = len(orders)
	} else if end > len(orders) {
		end = len(orders)
	}

	result := orders[start:end]

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"orders": result,
		"count":  len(result),
	})
}

// OrderUpdate represents an update to an existing order
type OrderUpdate struct {
	OrderID     string
	NewPrice    float64
	NewQuantity float64
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
	if order.OrderID == "" {
		order.OrderID = generateID()
	}

	// Mock order placement response
	result := struct {
		OrderID     string
		Status      string
		Message     string
		ExecutionID string
		Timestamp   time.Time
	}{
		OrderID:     order.OrderID,
		Status:      "accepted",
		Message:     "Order accepted and processed",
		ExecutionID: generateID(),
		Timestamp:   time.Now(),
	}

	RespondJSON(w, http.StatusCreated, result)
}

// handleGetOrder retrieves a specific order
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		RespondError(w, http.StatusNotFound, "Order ID is required")
		return
	}

	// Mock order data
	order := models.Order{
		OrderID:      id,
		Symbol:       "AAPL",
		Side:         models.OrderSideBuy,
		Type:         models.OrderTypeLimit,
		Quantity:     100,
		Price:        150.0,
		TimeInForce:  models.OrderTimeInForceGTC,
		Status:       models.OrderStatusPartiallyFilled,
		ClientID:     "client123",
		CreationTime: time.Now().Add(-1 * time.Hour),
		UpdateTime:   time.Now(),
		Executions: []models.OrderExecution{
			{
				ExecutionID:   generateID(),
				Price:         150.0,
				Quantity:      50,
				ExecutionTime: time.Now().Add(-30 * time.Minute),
				Venue:         "NASDAQ",
			},
		},
	}

	RespondJSON(w, http.StatusOK, order)
}

// handleCancelOrder cancels an order
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		RespondError(w, http.StatusBadRequest, "Order ID is required")
		return
	}

	// Mock successful cancellation
	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Order canceled",
		"orderId": id,
		"status":  "canceled",
	})
}

// handleModifyOrder modifies an existing order
func (s *Server) handleModifyOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var orderUpdate OrderUpdate

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&orderUpdate); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Set the ID from the URL
	orderUpdate.OrderID = id

	// Mock successful modification
	result := struct {
		OrderID    string
		Status     string
		Message    string
		Price      float64
		Quantity   float64
		ModifiedAt time.Time
	}{
		OrderID:    id,
		Status:     "modified",
		Message:    "Order successfully modified",
		Price:      orderUpdate.NewPrice,
		Quantity:   orderUpdate.NewQuantity,
		ModifiedAt: time.Now(),
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleCalculateRisk calculates risk metrics
func (s *Server) handleCalculateRisk(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		PortfolioID string `json:"portfolioId"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&requestBody); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Mock risk calculation
	if requestBody.PortfolioID == "" {
		RespondError(w, http.StatusNotFound, "Portfolio ID is required")
		return
	}

	// Create a mock risk metrics result
	result := struct {
		PortfolioID       string
		ValueAtRisk       float64
		ExpectedShortfall float64
		HistoricalVaR     float64
		StressVaR         float64
		Beta              float64
		Alpha             float64
		Sharpe            float64
		Volatility        float64
		CalculationTime   time.Time
	}{
		PortfolioID:       requestBody.PortfolioID,
		ValueAtRisk:       float64(len(requestBody.PortfolioID)) * 1000,
		ExpectedShortfall: float64(len(requestBody.PortfolioID)) * 1200,
		HistoricalVaR:     float64(len(requestBody.PortfolioID)) * 1100,
		StressVaR:         float64(len(requestBody.PortfolioID)) * 1500,
		Beta:              0.8,
		Alpha:             0.2,
		Sharpe:            1.5,
		Volatility:        0.15,
		CalculationTime:   time.Now(),
	}

	RespondJSON(w, http.StatusOK, result)
}

// handlePriceDerivative prices a derivative instrument
func (s *Server) handlePriceDerivative(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		Type       string  `json:"type"`
		Underlying string  `json:"underlying"`
		Strike     float64 `json:"strike"`
		Expiry     string  `json:"expiry"`
		Volatility float64 `json:"volatility"`
		RiskFree   float64 `json:"riskFree"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&requestBody); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Mock market data
	spotPrice := 100.0
	if requestBody.Underlying != "" {
		// Use the first letter of the ticker to generate a pseudo-random price
		spotPrice = 50.0 + 100.0*float64(requestBody.Underlying[0]%10)/10.0
	}

	// Parse expiry date
	expiryDate, err := time.Parse("2006-01-02", requestBody.Expiry)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid expiry date format. Use YYYY-MM-DD")
		return
	}

	// Calculate time to expiry in years
	timeToExpiry := expiryDate.Sub(time.Now()).Hours() / (24 * 365)
	if timeToExpiry <= 0 {
		RespondError(w, http.StatusBadRequest, "Expiry date must be in the future")
		return
	}

	// Mock option pricing using simple Black-Scholes approximation
	var price float64
	if strings.ToUpper(requestBody.Type) == "CALL" {
		// Simple call option pricing formula (not actual Black-Scholes)
		inTheMoney := math.Max(0, spotPrice-requestBody.Strike)
		timeValue := spotPrice * requestBody.Volatility * math.Sqrt(timeToExpiry)
		price = inTheMoney + timeValue
	} else if strings.ToUpper(requestBody.Type) == "PUT" {
		// Simple put option pricing formula (not actual Black-Scholes)
		inTheMoney := math.Max(0, requestBody.Strike-spotPrice)
		timeValue := spotPrice * requestBody.Volatility * math.Sqrt(timeToExpiry)
		price = inTheMoney + timeValue
	} else {
		RespondError(w, http.StatusBadRequest, "Invalid option type. Must be 'call' or 'put'")
		return
	}

	result := map[string]interface{}{
		"price":      price,
		"underlying": requestBody.Underlying,
		"type":       requestBody.Type,
		"strike":     requestBody.Strike,
		"expiry":     requestBody.Expiry,
		"spotPrice":  spotPrice,
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleCalculateGreeks calculates option greeks
func (s *Server) handleCalculateGreeks(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		Type       string  `json:"type"`
		Underlying string  `json:"underlying"`
		Strike     float64 `json:"strike"`
		Expiry     string  `json:"expiry"`
		Volatility float64 `json:"volatility"`
		RiskFree   float64 `json:"riskFree"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&requestBody); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Mock market data
	spotPrice := 100.0
	if requestBody.Underlying != "" {
		// Use the first letter of the ticker to generate a pseudo-random price
		spotPrice = 50.0 + 100.0*float64(requestBody.Underlying[0]%10)/10.0
	}

	// Parse expiry date
	expiryDate, err := time.Parse("2006-01-02", requestBody.Expiry)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid expiry date format. Use YYYY-MM-DD")
		return
	}

	// Calculate time to expiry in years
	timeToExpiry := expiryDate.Sub(time.Now()).Hours() / (24 * 365)
	if timeToExpiry <= 0 {
		RespondError(w, http.StatusBadRequest, "Expiry date must be in the future")
		return
	}

	// Calculate greeks (simplified calculations for mock purposes)
	isCall := strings.ToUpper(requestBody.Type) == "CALL"

	// Mock greek calculations
	var delta float64
	if isCall {
		delta = 0.5 + 0.5*math.Tanh((spotPrice-requestBody.Strike)/spotPrice/requestBody.Volatility)
	} else {
		delta = -0.5 - 0.5*math.Tanh((spotPrice-requestBody.Strike)/spotPrice/requestBody.Volatility)
	}

	gamma := math.Exp(-math.Pow(spotPrice-requestBody.Strike, 2)/(2*spotPrice*spotPrice*requestBody.Volatility*requestBody.Volatility)) /
		(spotPrice * requestBody.Volatility * math.Sqrt(2*math.Pi))

	vega := spotPrice * math.Sqrt(timeToExpiry) * gamma

	theta := -spotPrice * requestBody.Volatility / (2 * math.Sqrt(timeToExpiry)) * gamma

	rho := timeToExpiry * requestBody.Strike * 0.01
	if isCall {
		rho = math.Abs(rho)
	} else {
		rho = -math.Abs(rho)
	}

	result := map[string]interface{}{
		"delta":      delta,
		"gamma":      gamma,
		"theta":      theta,
		"vega":       vega,
		"rho":        rho,
		"underlying": requestBody.Underlying,
		"type":       requestBody.Type,
		"strike":     requestBody.Strike,
		"expiry":     requestBody.Expiry,
		"spotPrice":  spotPrice,
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleCalculateImpliedVol calculates implied volatility from option price
func (s *Server) handleCalculateImpliedVol(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		Type       string  `json:"type"`
		Underlying string  `json:"underlying"`
		Strike     float64 `json:"strike"`
		Expiry     string  `json:"expiry"`
		Price      float64 `json:"price"`
		RiskFree   float64 `json:"riskFree"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&requestBody); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// Mock market data
	spotPrice := 100.0
	if requestBody.Underlying != "" {
		// Use the first letter of the ticker to generate a pseudo-random price
		spotPrice = 50.0 + 100.0*float64(requestBody.Underlying[0]%10)/10.0
	}

	// Parse expiry date
	expiryDate, err := time.Parse("2006-01-02", requestBody.Expiry)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid expiry date format. Use YYYY-MM-DD")
		return
	}

	// Calculate time to expiry in years
	timeToExpiry := expiryDate.Sub(time.Now()).Hours() / (24 * 365)
	if timeToExpiry <= 0 {
		RespondError(w, http.StatusBadRequest, "Expiry date must be in the future")
		return
	}

	// Calculate implied volatility using a simplified formula for demo purposes
	isCall := strings.ToUpper(requestBody.Type) == "CALL"

	// Mock calculation of implied volatility
	var impliedVol float64

	moneyness := spotPrice / requestBody.Strike
	if isCall {
		// Approximate IV using a simple heuristic
		impliedVol = requestBody.Price / (spotPrice * math.Sqrt(timeToExpiry)) * 2

		// Add a moneyness adjustment
		if moneyness > 1 { // in the money
			impliedVol *= 0.9
		} else { // out of the money
			impliedVol *= 1.1
		}
	} else { // put
		impliedVol = requestBody.Price / (requestBody.Strike * math.Sqrt(timeToExpiry)) * 2

		// Add a moneyness adjustment
		if moneyness < 1 { // in the money
			impliedVol *= 0.9
		} else { // out of the money
			impliedVol *= 1.1
		}
	}

	// Cap the result to reasonable bounds
	impliedVol = math.Max(0.05, math.Min(1.0, impliedVol))

	result := map[string]interface{}{
		"impliedVolatility": impliedVol,
		"underlying":        requestBody.Underlying,
		"type":              requestBody.Type,
		"strike":            requestBody.Strike,
		"expiry":            requestBody.Expiry,
		"price":             requestBody.Price,
		"spotPrice":         spotPrice,
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleHealth returns health status of the API
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check dependencies health - simplified mock version
	dbHealthy := "healthy"
	kafkaHealthy := "healthy"

	status := http.StatusOK
	result := map[string]interface{}{
		"status":  "healthy",
		"version": "1.0.0",
		"uptime":  "12h34m56s", // Mock uptime
		"dependencies": map[string]string{
			"database": dbHealthy,
			"kafka":    kafkaHealthy,
		},
	}

	// If any dependency is not healthy, set status to service unavailable
	if dbHealthy != "healthy" || kafkaHealthy != "healthy" {
		status = http.StatusServiceUnavailable
		result["status"] = "degraded"
	}

	RespondJSON(w, status, result)
}

// handleNotFound handles 404 errors
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusNotFound, "The requested resource was not found")
}

// Helper functions

// generateID generates a unique ID for resources
func generateID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(),
		strings.ReplaceAll(strings.ToLower(strconv.FormatInt(int64(time.Now().Nanosecond()), 36)), "0", "a"))
}

// validateOrder validates an order request
func validateOrder(order *models.Order) error {
	if order.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}

	// Check order side (Buy or Sell)
	if order.Side != models.OrderSideBuy && order.Side != models.OrderSideSell {
		return fmt.Errorf("side must be Buy or Sell")
	}

	// Check order type (Market, Limit, etc.)
	if order.Type != models.OrderTypeMarket &&
		order.Type != models.OrderTypeLimit &&
		order.Type != models.OrderTypeStop &&
		order.Type != models.OrderTypeStopLimit {
		return fmt.Errorf("invalid order type")
	}

	if order.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}

	// Price is required for limit orders
	if (order.Type == models.OrderTypeLimit || order.Type == models.OrderTypeStopLimit) && order.Price <= 0 {
		return fmt.Errorf("price must be positive for limit orders")
	}

	return nil
}

// checkDatabaseConnection checks if the database is reachable
func (s *Server) checkDatabaseConnection() string {
	// Placeholder for actual database health check
	// In a real implementation, this would ping the database
	return "healthy"
}

// checkKafkaConnection checks if Kafka is reachable
func (s *Server) checkKafkaConnection() string {
	// Placeholder for actual Kafka health check
	// In a real implementation, this would check the Kafka connection
	return "healthy"
}
