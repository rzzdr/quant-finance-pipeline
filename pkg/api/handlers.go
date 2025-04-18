package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/internal/risk"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Handlers contains all HTTP handlers for the API
type Handlers struct {
	marketProcessor market.Processor
	riskCalculator  *risk.Calculator
	hedgingCalc     *risk.HedgingCalculator
	bsPricer        *risk.BlackScholesPricer
	log             *logger.Logger
}

// CreateHandlers creates new API handlers
func CreateHandlers(
	marketProcessor market.Processor,
	riskCalculator *risk.Calculator,
	hedgingCalc *risk.HedgingCalculator,
	bsPricer *risk.BlackScholesPricer,
) *Handlers {
	return &Handlers{
		marketProcessor: marketProcessor,
		riskCalculator:  riskCalculator,
		hedgingCalc:     hedgingCalc,
		bsPricer:        bsPricer,
		log:             logger.GetLogger("api.handlers"),
	}
}

// HealthCheckHandler handles health check requests
func (h *Handlers) HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
	})
}

// GetMarketDataHandler returns market data for a symbol
func (h *Handlers) GetMarketDataHandler(c *gin.Context) {
	symbol := c.Param("symbol")

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "symbol is required",
		})
		return
	}

	// Get market data from the processor
	data, err := h.marketProcessor.GetMarketData(symbol)
	if err != nil {
		h.log.Errorf("Failed to get market data for %s: %v", symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get market data: %v", err),
		})
		return
	}

	// Check if data is nil
	if data == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("No market data found for symbol %s", symbol),
		})
		return
	}

	c.JSON(http.StatusOK, data)
}

// GetOrderBookHandler returns order book data for a symbol
func (h *Handlers) GetOrderBookHandler(c *gin.Context) {
	symbol := c.Param("symbol")

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "symbol is required",
		})
		return
	}

	// Get order book from the processor
	orderBook, err := h.marketProcessor.GetOrderBook(symbol)
	if err != nil {
		h.log.Errorf("Failed to get order book for %s: %v", symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get order book: %v", err),
		})
		return
	}

	// Check if order book is nil
	if orderBook == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("No order book found for symbol %s", symbol),
		})
		return
	}

	c.JSON(http.StatusOK, orderBook)
}

// PlaceOrderHandler handles order placement
func (h *Handlers) PlaceOrderHandler(c *gin.Context) {
	var order models.Order
	if err := c.ShouldBindJSON(&order); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid order data: %v", err),
		})
		return
	}

	// Validate order
	if order.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order symbol is required",
		})
		return
	}

	if order.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order quantity must be positive",
		})
		return
	}

	if order.Type == models.OrderTypeLimit && order.Price <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Limit price must be positive",
		})
		return
	}

	// Set creation time if not provided
	if order.CreationTime.IsZero() {
		order.CreationTime = time.Now()
	}
	order.UpdateTime = time.Now()

	// Process the order
	result, err := h.marketProcessor.PlaceOrder(&order)
	if err != nil {
		h.log.Errorf("Failed to place order: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to place order: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetOrderHandler returns order details
func (h *Handlers) GetOrderHandler(c *gin.Context) {
	orderID := c.Param("id")

	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order ID is required",
		})
		return
	}

	// Get order from the processor
	order, err := h.marketProcessor.GetOrder(orderID)
	if err != nil {
		h.log.Errorf("Failed to get order %s: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get order: %v", err),
		})
		return
	}

	// Check if order is nil
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("Order %s not found", orderID),
		})
		return
	}

	c.JSON(http.StatusOK, order)
}

// CancelOrderHandler handles order cancellation
func (h *Handlers) CancelOrderHandler(c *gin.Context) {
	orderID := c.Param("id")

	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order ID is required",
		})
		return
	}

	// Cancel the order
	err := h.marketProcessor.CancelOrder(orderID)
	if err != nil {
		h.log.Errorf("Failed to cancel order %s: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to cancel order: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": fmt.Sprintf("Order %s cancelled successfully", orderID),
	})
}

// CalculateRiskMetricsHandler calculates risk metrics for a portfolio
func (h *Handlers) CalculateRiskMetricsHandler(c *gin.Context) {
	var request struct {
		PortfolioID string `json:"portfolioId"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if request.PortfolioID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Portfolio ID is required",
		})
		return
	}

	// Calculate risk metrics
	metrics, err := h.riskCalculator.CalculateRiskMetrics(c.Request.Context(), request.PortfolioID)
	if err != nil {
		h.log.Errorf("Failed to calculate risk metrics for portfolio %s: %v", request.PortfolioID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to calculate risk metrics: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// RunStressTestHandler runs stress tests on a portfolio
func (h *Handlers) RunStressTestHandler(c *gin.Context) {
	var request struct {
		PortfolioID string                  `json:"portfolioId"`
		Scenarios   []models.StressScenario `json:"scenarios"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if request.PortfolioID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Portfolio ID is required",
		})
		return
	}

	if len(request.Scenarios) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "At least one scenario is required",
		})
		return
	}

	// Run stress test
	results, err := h.riskCalculator.RunStressTest(c.Request.Context(), request.PortfolioID, request.Scenarios)
	if err != nil {
		h.log.Errorf("Failed to run stress test for portfolio %s: %v", request.PortfolioID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to run stress test: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, results)
}

// GenerateHedgingStrategyHandler generates a hedging strategy for a portfolio
func (h *Handlers) GenerateHedgingStrategyHandler(c *gin.Context) {
	var request struct {
		PortfolioID  string  `json:"portfolioId"`
		StrategyType string  `json:"strategyType"`
		MaxBudget    float64 `json:"maxBudget,omitempty"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if request.PortfolioID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Portfolio ID is required",
		})
		return
	}

	// Get portfolio from the risk calculator
	portfolio, err := h.riskCalculator.GetPortfolio(request.PortfolioID)
	if err != nil {
		h.log.Errorf("Failed to get portfolio %s: %v", request.PortfolioID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get portfolio: %v", err),
		})
		return
	}

	// Determine strategy type
	var strategyType risk.HedgingStrategyType
	switch request.StrategyType {
	case "delta":
		strategyType = risk.HedgingStrategyTypeDelta
	case "delta-gamma":
		strategyType = risk.HedgingStrategyTypeDeltaGamma
	case "minimum-variance":
		strategyType = risk.HedgingStrategyTypeMinimumVariance
	default:
		strategyType = risk.HedgingStrategyTypeDelta // Default to delta hedging
	}

	// Generate hedging strategy
	strategy, err := h.hedgingCalc.GenerateHedgingStrategy(c.Request.Context(), portfolio, strategyType)
	if err != nil {
		h.log.Errorf("Failed to generate hedging strategy for portfolio %s: %v", request.PortfolioID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to generate hedging strategy: %v", err),
		})
		return
	}

	// Optimize if max budget is provided
	if request.MaxBudget > 0 && strategy.EstimatedCost > request.MaxBudget {
		strategy = h.hedgingCalc.OptimizeHedgingStrategy(strategy, request.MaxBudget)
	}

	c.JSON(http.StatusOK, strategy)
}

// CreateHedgingStrategyHandler creates orders for a hedging strategy
func (h *Handlers) CreateHedgingStrategyHandler(c *gin.Context) {
	var strategy models.HedgingStrategy

	if err := c.ShouldBindJSON(&strategy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid hedging strategy: %v", err),
		})
		return
	}

	if strategy.PortfolioID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Portfolio ID is required",
		})
		return
	}

	if len(strategy.Actions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "At least one hedging action is required",
		})
		return
	}

	// Convert hedging strategy to orders
	orders := h.hedgingCalc.ImplementHedgingStrategy(c.Request.Context(), &strategy)

	// Place the orders
	results := make([]interface{}, 0, len(orders))
	for _, order := range orders {
		result, err := h.marketProcessor.PlaceOrder(order)
		if err != nil {
			h.log.Warnf("Failed to place hedging order %s: %v", order.OrderID, err)
			results = append(results, gin.H{
				"orderId": order.OrderID,
				"status":  "failed",
				"error":   err.Error(),
			})
		} else {
			results = append(results, result)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"orders": results,
	})
}

// PriceDerivativeHandler calculates price of a derivative
func (h *Handlers) PriceDerivativeHandler(c *gin.Context) {
	var request struct {
		DerivativeSymbol string  `json:"derivativeSymbol"`
		UnderlyingSymbol string  `json:"underlyingSymbol"`
		StrikePrice      float64 `json:"strikePrice"`
		ExpiryDate       string  `json:"expiryDate"`
		OptionType       string  `json:"optionType"`
		Volatility       float64 `json:"volatility"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Validate required fields
	if request.UnderlyingSymbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Underlying symbol is required",
		})
		return
	}

	// Get market data for underlying
	underlyingData, err := h.marketProcessor.GetMarketData(request.UnderlyingSymbol)
	if err != nil {
		h.log.Errorf("Failed to get market data for %s: %v", request.UnderlyingSymbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get underlying market data: %v", err),
		})
		return
	}

	// Parse expiry date
	expiryDate, err := time.Parse("2006-01-02", request.ExpiryDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid expiry date format, use YYYY-MM-DD: %v", err),
		})
		return
	}

	// Determine option type
	var optionType models.OptionType
	if request.OptionType == "call" {
		optionType = models.OptionTypeCall
	} else if request.OptionType == "put" {
		optionType = models.OptionTypePut
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Option type must be 'call' or 'put'",
		})
		return
	}

	// Create derivative market data
	derivative := &models.MarketData{
		Symbol:    request.DerivativeSymbol,
		Timestamp: time.Now(),
		DerivativeInfo: &models.DerivativeInfo{
			Type:              models.DerivativeTypeOption,
			UnderlyingSymbol:  request.UnderlyingSymbol,
			StrikePrice:       request.StrikePrice,
			ExpiryDate:        expiryDate,
			OptionType:        optionType,
			ImpliedVolatility: request.Volatility,
		},
	}

	// Price the derivative
	price, err := h.bsPricer.PriceOption(derivative, underlyingData)
	if err != nil {
		h.log.Errorf("Failed to price derivative: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to price derivative: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":           request.DerivativeSymbol,
		"underlyingSymbol": request.UnderlyingSymbol,
		"underlyingPrice":  underlyingData.Price,
		"strikePrice":      request.StrikePrice,
		"expiryDate":       request.ExpiryDate,
		"optionType":       request.OptionType,
		"volatility":       request.Volatility,
		"price":            price,
	})
}

// CalculateGreeksHandler calculates Greeks for a derivative
func (h *Handlers) CalculateGreeksHandler(c *gin.Context) {
	var request struct {
		DerivativeSymbol string  `json:"derivativeSymbol"`
		UnderlyingSymbol string  `json:"underlyingSymbol"`
		StrikePrice      float64 `json:"strikePrice"`
		ExpiryDate       string  `json:"expiryDate"`
		OptionType       string  `json:"optionType"`
		Volatility       float64 `json:"volatility"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Validate required fields
	if request.UnderlyingSymbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Underlying symbol is required",
		})
		return
	}

	// Get market data for underlying
	underlyingData, err := h.marketProcessor.GetMarketData(request.UnderlyingSymbol)
	if err != nil {
		h.log.Errorf("Failed to get market data for %s: %v", request.UnderlyingSymbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get underlying market data: %v", err),
		})
		return
	}

	// Parse expiry date
	expiryDate, err := time.Parse("2006-01-02", request.ExpiryDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid expiry date format, use YYYY-MM-DD: %v", err),
		})
		return
	}

	// Determine option type
	var optionType models.OptionType
	if request.OptionType == "call" {
		optionType = models.OptionTypeCall
	} else if request.OptionType == "put" {
		optionType = models.OptionTypePut
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Option type must be 'call' or 'put'",
		})
		return
	}

	// Create derivative market data
	derivative := &models.MarketData{
		Symbol:    request.DerivativeSymbol,
		Timestamp: time.Now(),
		DerivativeInfo: &models.DerivativeInfo{
			Type:              models.DerivativeTypeOption,
			UnderlyingSymbol:  request.UnderlyingSymbol,
			StrikePrice:       request.StrikePrice,
			ExpiryDate:        expiryDate,
			OptionType:        optionType,
			ImpliedVolatility: request.Volatility,
		},
	}

	// Calculate Greeks
	greeks := h.bsPricer.CalculateGreeks(derivative, underlyingData)

	c.JSON(http.StatusOK, gin.H{
		"symbol":           request.DerivativeSymbol,
		"underlyingSymbol": request.UnderlyingSymbol,
		"underlyingPrice":  underlyingData.Price,
		"strikePrice":      request.StrikePrice,
		"expiryDate":       request.ExpiryDate,
		"optionType":       request.OptionType,
		"volatility":       request.Volatility,
		"greeks":           greeks,
	})
}

// CalculateImpliedVolHandler calculates implied volatility for an option
func (h *Handlers) CalculateImpliedVolHandler(c *gin.Context) {
	var request struct {
		DerivativeSymbol string  `json:"derivativeSymbol"`
		UnderlyingSymbol string  `json:"underlyingSymbol"`
		StrikePrice      float64 `json:"strikePrice"`
		ExpiryDate       string  `json:"expiryDate"`
		OptionType       string  `json:"optionType"`
		MarketPrice      float64 `json:"marketPrice"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Validate required fields
	if request.UnderlyingSymbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Underlying symbol is required",
		})
		return
	}

	if request.MarketPrice <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Market price must be positive",
		})
		return
	}

	// Get market data for underlying
	underlyingData, err := h.marketProcessor.GetMarketData(request.UnderlyingSymbol)
	if err != nil {
		h.log.Errorf("Failed to get market data for %s: %v", request.UnderlyingSymbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get underlying market data: %v", err),
		})
		return
	}

	// Parse expiry date
	expiryDate, err := time.Parse("2006-01-02", request.ExpiryDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid expiry date format, use YYYY-MM-DD: %v", err),
		})
		return
	}

	// Determine option type
	var optionType models.OptionType
	if request.OptionType == "call" {
		optionType = models.OptionTypeCall
	} else if request.OptionType == "put" {
		optionType = models.OptionTypePut
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Option type must be 'call' or 'put'",
		})
		return
	}

	// Create derivative market data
	derivative := &models.MarketData{
		Symbol:    request.DerivativeSymbol,
		Timestamp: time.Now(),
		DerivativeInfo: &models.DerivativeInfo{
			Type:              models.DerivativeTypeOption,
			UnderlyingSymbol:  request.UnderlyingSymbol,
			StrikePrice:       request.StrikePrice,
			ExpiryDate:        expiryDate,
			OptionType:        optionType,
			ImpliedVolatility: 0.2, // Initial guess
		},
	}

	// Calculate implied volatility
	impliedVol := h.bsPricer.ImpliedVolatility(derivative, underlyingData, request.MarketPrice)

	// Update derivative with calculated implied volatility
	derivative.DerivativeInfo.ImpliedVolatility = impliedVol

	// Calculate Greeks with implied volatility
	greeks := h.bsPricer.CalculateGreeks(derivative, underlyingData)

	c.JSON(http.StatusOK, gin.H{
		"symbol":            request.DerivativeSymbol,
		"underlyingSymbol":  request.UnderlyingSymbol,
		"underlyingPrice":   underlyingData.Price,
		"strikePrice":       request.StrikePrice,
		"expiryDate":        request.ExpiryDate,
		"optionType":        request.OptionType,
		"marketPrice":       request.MarketPrice,
		"impliedVolatility": impliedVol,
		"greeks":            greeks,
	})
}

// GetPortfolio is a helper method to get a portfolio by ID
func (h *Handlers) GetRiskCalculator() *risk.Calculator {
	return h.riskCalculator
}
