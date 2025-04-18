package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// HedgingStrategyType represents the type of hedging strategy
type HedgingStrategyType int

const (
	// HedgingStrategyTypeDelta represents delta hedging
	HedgingStrategyTypeDelta HedgingStrategyType = iota

	// HedgingStrategyTypeDeltaGamma represents delta-gamma hedging
	HedgingStrategyTypeDeltaGamma

	// HedgingStrategyTypeMinimumVariance represents minimum variance hedging
	HedgingStrategyTypeMinimumVariance
)

// HedgingCalculator handles calculation of hedging strategies
type HedgingCalculator struct {
	greeksCalculator   *GreeksCalculator
	bsPricer           *BlackScholesPricer
	marketDataProvider MarketDataProvider
	log                *logger.Logger
}

// MarketDataProvider defines an interface for retrieving market data
type MarketDataProvider interface {
	GetMarketData(symbol string) (*models.MarketData, error)
	GetMarketDataBatch(symbols []string) (map[string]*models.MarketData, error)
}

// NewHedgingCalculator creates a new hedging calculator
func NewHedgingCalculator(
	greeksCalculator *GreeksCalculator,
	bsPricer *BlackScholesPricer,
	marketDataProvider MarketDataProvider,
) *HedgingCalculator {
	return &HedgingCalculator{
		greeksCalculator:   greeksCalculator,
		bsPricer:           bsPricer,
		marketDataProvider: marketDataProvider,
		log:                logger.GetLogger("risk.hedging"),
	}
}

// GenerateHedgingStrategy creates a hedging strategy for a portfolio
func (hc *HedgingCalculator) GenerateHedgingStrategy(
	ctx context.Context,
	portfolio *models.Portfolio,
	strategyType HedgingStrategyType,
) (*models.HedgingStrategy, error) {
	// Get all symbols in the portfolio
	symbols := make([]string, 0, len(portfolio.Positions))
	for _, pos := range portfolio.Positions {
		symbols = append(symbols, pos.Symbol)
		if pos.IsDerivative && pos.DerivativeInfo != nil {
			symbols = append(symbols, pos.DerivativeInfo.UnderlyingSymbol)
		}
	}

	// Add common hedging instruments
	hedgingInstruments := []string{"SPY", "QQQ", "IWM", "TLT", "GLD", "VXX"}
	symbols = append(symbols, hedgingInstruments...)

	// Get market data for all symbols
	marketData, err := hc.marketDataProvider.GetMarketDataBatch(symbols)
	if err != nil {
		return nil, fmt.Errorf("failed to get market data: %w", err)
	}

	// Calculate portfolio Greeks
	portfolioGreeks := hc.greeksCalculator.CalculatePortfolioGreeks(portfolio, marketData)

	// Calculate net exposure by underlying
	netExposure := hc.greeksCalculator.CalculatePortfolioNetExposure(portfolio, marketData)

	// Generate hedging strategy based on type
	var actions []models.HedgingAction
	var estimatedCost float64
	var riskReduction float64

	switch strategyType {
	case HedgingStrategyTypeDelta:
		actions, estimatedCost, riskReduction = hc.generateDeltaHedge(portfolio, marketData, portfolioGreeks, netExposure)
	case HedgingStrategyTypeDeltaGamma:
		actions, estimatedCost, riskReduction = hc.generateDeltaGammaHedge(portfolio, marketData, portfolioGreeks, netExposure)
	case HedgingStrategyTypeMinimumVariance:
		actions, estimatedCost, riskReduction = hc.generateMinimumVarianceHedge(portfolio, marketData, portfolioGreeks, netExposure)
	default:
		return nil, fmt.Errorf("unsupported hedging strategy type: %v", strategyType)
	}

	// Create recommendations
	recommendations := hc.generateHedgingRecommendations(portfolio, marketData, portfolioGreeks)

	// Create hedging strategy
	strategy := &models.HedgingStrategy{
		PortfolioID:     portfolio.ID,
		Timestamp:       time.Now(),
		Actions:         actions,
		EstimatedCost:   estimatedCost,
		RiskReduction:   riskReduction,
		CurrentRisk:     math.Abs(portfolioGreeks.Delta),
		Recommendations: recommendations,
	}

	return strategy, nil
}

// generateDeltaHedge generates a delta hedging strategy
func (hc *HedgingCalculator) generateDeltaHedge(
	portfolio *models.Portfolio,
	marketData map[string]*models.MarketData,
	portfolioGreeks *models.DerivativeGreeks,
	netExposure map[string]float64,
) ([]models.HedgingAction, float64, float64) {
	var actions []models.HedgingAction
	var totalCost float64

	// Net delta to hedge
	netDelta := portfolioGreeks.Delta
	absNetDelta := math.Abs(netDelta)

	// If delta is close to zero, no hedging needed
	if absNetDelta < 0.01 {
		return actions, 0, 0
	}

	// Find the most liquid instrument to hedge with
	// Default to SPY (S&P 500 ETF)
	hedgeSymbol := "SPY"
	hedgePrice := 0.0

	if md, ok := marketData[hedgeSymbol]; ok {
		hedgePrice = md.Price
	} else {
		hc.log.Warnf("Hedge instrument %s not found in market data", hedgeSymbol)
		return actions, 0, 0
	}

	// Calculate hedge quantity (opposite sign of net delta)
	hedgeQty := -netDelta

	// Round to nearest lot size
	hedgeQty = math.Round(hedgeQty/100) * 100

	// Create hedging action
	if hedgeQty != 0 {
		action := models.HedgingAction{
			Instrument:     hedgeSymbol,
			Quantity:       hedgeQty,
			EstimatedPrice: hedgePrice,
			Reason:         fmt.Sprintf("Delta hedge of %.2f", netDelta),
			RiskOffset:     netDelta,
		}

		actions = append(actions, action)
		totalCost = math.Abs(hedgeQty * hedgePrice)
	}

	// Risk reduction is close to 100% for delta hedging
	riskReduction := 0.99

	return actions, totalCost, riskReduction
}

// generateDeltaGammaHedge generates a delta-gamma hedging strategy
func (hc *HedgingCalculator) generateDeltaGammaHedge(
	portfolio *models.Portfolio,
	marketData map[string]*models.MarketData,
	portfolioGreeks *models.DerivativeGreeks,
	netExposure map[string]float64,
) ([]models.HedgingAction, float64, float64) {
	var actions []models.HedgingAction
	var totalCost float64

	// Net delta and gamma to hedge
	netDelta := portfolioGreeks.Delta
	netGamma := portfolioGreeks.Gamma

	// If both delta and gamma are close to zero, no hedging needed
	if math.Abs(netDelta) < 0.01 && math.Abs(netGamma) < 0.01 {
		return actions, 0, 0
	}

	// Find the main underlying with the most exposure
	var mainUnderlying string
	var maxExposure float64

	for symbol, exposure := range netExposure {
		if math.Abs(exposure) > maxExposure {
			maxExposure = math.Abs(exposure)
			mainUnderlying = symbol
		}
	}

	// Default to SPY if no underlying found
	if mainUnderlying == "" {
		mainUnderlying = "SPY"
	}

	// Get market data for the main underlying
	underlyingData, ok := marketData[mainUnderlying]
	if !ok {
		hc.log.Warnf("Main underlying %s not found in market data", mainUnderlying)
		return actions, 0, 0
	}

	// Find options for the underlying with different strikes
	// Here we would need a function to find suitable options to hedge with
	// For now, we'll simulate finding an ATM call and put

	// Simulate ATM call and put
	atmStrike := math.Round(underlyingData.Price/5) * 5 // Round to nearest $5

	// Simulate finding call and put options
	callOption := &models.MarketData{
		Symbol:    fmt.Sprintf("%s_C_%.0f", mainUnderlying, atmStrike),
		Price:     5.0, // Simplified
		Timestamp: time.Now(),
		DerivativeInfo: &models.DerivativeInfo{
			Type:              models.DerivativeTypeOption,
			UnderlyingSymbol:  mainUnderlying,
			StrikePrice:       atmStrike,
			ExpiryDate:        time.Now().AddDate(0, 1, 0), // 1 month expiry
			OptionType:        models.OptionTypeCall,
			ImpliedVolatility: 0.2, // Simplified
		},
	}

	putOption := &models.MarketData{
		Symbol:    fmt.Sprintf("%s_P_%.0f", mainUnderlying, atmStrike),
		Price:     5.0, // Simplified
		Timestamp: time.Now(),
		DerivativeInfo: &models.DerivativeInfo{
			Type:              models.DerivativeTypeOption,
			UnderlyingSymbol:  mainUnderlying,
			StrikePrice:       atmStrike,
			ExpiryDate:        time.Now().AddDate(0, 1, 0), // 1 month expiry
			OptionType:        models.OptionTypePut,
			ImpliedVolatility: 0.2, // Simplified
		},
	}

	// Calculate Greeks for these options
	callGreeks := hc.bsPricer.CalculateGreeks(callOption, underlyingData)
	putGreeks := hc.bsPricer.CalculateGreeks(putOption, underlyingData)

	// Solve the system of equations to find quantities
	// For gamma hedging, we need to solve:
	// q1 * callDelta + q2 * putDelta = -netDelta
	// q1 * callGamma + q2 * putGamma = -netGamma

	// Calculate determinant
	det := callGreeks.Delta*putGreeks.Gamma - callGreeks.Gamma*putGreeks.Delta

	if math.Abs(det) < 0.0001 {
		// Determinant too close to zero, fallback to delta hedge
		return hc.generateDeltaHedge(portfolio, marketData, portfolioGreeks, netExposure)
	}

	// Solve for quantities
	q1 := (-putGreeks.Delta*netGamma + putGreeks.Gamma*netDelta) / det
	q2 := (callGreeks.Delta*netGamma - callGreeks.Gamma*netDelta) / det

	// Round quantities to standard option lot size (typically 100 shares per contract)
	q1 = math.Round(q1/100) * 100
	q2 = math.Round(q2/100) * 100

	// Create hedging actions
	if q1 != 0 {
		action := models.HedgingAction{
			Instrument:     callOption.Symbol,
			Quantity:       q1,
			EstimatedPrice: callOption.Price,
			Reason:         "Delta-Gamma hedge (call option)",
			RiskOffset:     q1 * callGreeks.Delta,
		}
		actions = append(actions, action)
		totalCost += math.Abs(q1 * callOption.Price)
	}

	if q2 != 0 {
		action := models.HedgingAction{
			Instrument:     putOption.Symbol,
			Quantity:       q2,
			EstimatedPrice: putOption.Price,
			Reason:         "Delta-Gamma hedge (put option)",
			RiskOffset:     q2 * putGreeks.Delta,
		}
		actions = append(actions, action)
		totalCost += math.Abs(q2 * putOption.Price)
	}

	// Theoretical risk reduction (both delta and gamma are hedged)
	riskReduction := 0.95

	return actions, totalCost, riskReduction
}

// generateMinimumVarianceHedge generates a minimum variance hedging strategy
func (hc *HedgingCalculator) generateMinimumVarianceHedge(
	portfolio *models.Portfolio,
	marketData map[string]*models.MarketData,
	portfolioGreeks *models.DerivativeGreeks,
	netExposure map[string]float64,
) ([]models.HedgingAction, float64, float64) {
	var actions []models.HedgingAction
	var totalCost float64

	// For minimum variance hedging, we need historical returns and covariance matrix
	// This is a simplified implementation without historical data

	// Identify top 3 assets for hedging
	hedgeAssets := []string{"SPY", "QQQ", "TLT"}

	// Simple hedging weights (in reality would use optimization)
	weights := map[string]float64{
		"SPY": -0.6, // 60% of hedge with SPY
		"QQQ": -0.3, // 30% of hedge with QQQ
		"TLT": -0.1, // 10% of hedge with TLT
	}

	// Total portfolio value (simplified)
	portfolioValue := 1000000.0 // $1M portfolio

	// Total hedge amount based on delta
	hedgeAmount := -portfolioGreeks.Delta * 100 // Scale factor for demonstration

	// Create hedging actions
	for _, symbol := range hedgeAssets {
		md, ok := marketData[symbol]
		if !ok {
			hc.log.Warnf("Hedge asset %s not found in market data", symbol)
			continue
		}

		// Calculate quantity based on weight and current price
		weight := weights[symbol]
		quantity := (hedgeAmount * weight) / md.Price

		// Round to standard lot size (100 shares)
		quantity = math.Round(quantity/100) * 100

		if quantity != 0 {
			action := models.HedgingAction{
				Instrument:     symbol,
				Quantity:       quantity,
				EstimatedPrice: md.Price,
				Reason:         fmt.Sprintf("Minimum variance hedge (%.1f%%)", weight*100),
				RiskOffset:     quantity, // Simplified, in reality would be more complex
			}

			actions = append(actions, action)
			totalCost += math.Abs(quantity * md.Price)
		}
	}

	// Simplified risk reduction
	riskReduction := 0.85 // 85% risk reduction

	return actions, totalCost, riskReduction
}

// generateHedgingRecommendations generates additional hedging recommendations
func (hc *HedgingCalculator) generateHedgingRecommendations(
	portfolio *models.Portfolio,
	marketData map[string]*models.MarketData,
	portfolioGreeks *models.DerivativeGreeks,
) []models.HedgingRecommendation {
	var recommendations []models.HedgingRecommendation

	// Check if delta hedging is needed
	if math.Abs(portfolioGreeks.Delta) > 0.1 {
		recommendations = append(recommendations, models.HedgingRecommendation{
			Symbol:        "SPY",
			Quantity:      -math.Round(portfolioGreeks.Delta/100) * 100,
			EstimatedCost: math.Abs(portfolioGreeks.Delta) * 400, // Rough estimate
			Description:   fmt.Sprintf("Consider delta hedging with SPY (net delta: %.2f)", portfolioGreeks.Delta),
		})
	}

	// Check if vega hedging is needed
	if math.Abs(portfolioGreeks.Vega) > 0.1 {
		recommendations = append(recommendations, models.HedgingRecommendation{
			Symbol:        "VXX",
			Quantity:      -math.Round(portfolioGreeks.Vega*10/100) * 100,
			EstimatedCost: math.Abs(portfolioGreeks.Vega) * 10 * 25, // Rough estimate
			Description:   fmt.Sprintf("Consider vega hedging with VXX (net vega: %.2f)", portfolioGreeks.Vega),
		})
	}

	// Check if gamma hedging is needed
	if math.Abs(portfolioGreeks.Gamma) > 0.1 {
		recommendations = append(recommendations, models.HedgingRecommendation{
			Symbol:        "SPY_STRADDLE",
			Quantity:      math.Round(portfolioGreeks.Gamma*100/100) * 100,
			EstimatedCost: math.Abs(portfolioGreeks.Gamma) * 100 * 10, // Rough estimate
			Description:   fmt.Sprintf("Consider gamma hedging with SPY straddles (net gamma: %.2f)", portfolioGreeks.Gamma),
		})
	}

	return recommendations
}

// OptimizeHedgingStrategy optimizes a hedging strategy based on cost constraints
func (hc *HedgingCalculator) OptimizeHedgingStrategy(
	strategy *models.HedgingStrategy,
	maxCost float64,
) *models.HedgingStrategy {
	// If strategy is already under budget, return as is
	if strategy.EstimatedCost <= maxCost {
		return strategy
	}

	// Create a copy of the strategy
	optimized := &models.HedgingStrategy{
		PortfolioID:     strategy.PortfolioID,
		Timestamp:       strategy.Timestamp,
		Actions:         make([]models.HedgingAction, 0, len(strategy.Actions)),
		Recommendations: strategy.Recommendations,
		CurrentRisk:     strategy.CurrentRisk,
	}

	// Sort actions by effectiveness (risk offset per dollar)
	type actionWithEffectiveness struct {
		action        models.HedgingAction
		effectiveness float64
	}

	actionEffectiveness := make([]actionWithEffectiveness, 0, len(strategy.Actions))
	for _, action := range strategy.Actions {
		cost := math.Abs(action.Quantity * action.EstimatedPrice)
		if cost > 0 {
			effectiveness := math.Abs(action.RiskOffset) / cost
			actionEffectiveness = append(actionEffectiveness, actionWithEffectiveness{
				action:        action,
				effectiveness: effectiveness,
			})
		}
	}

	// Sort by effectiveness (highest first)
	// This would typically be implemented with a sorting function
	// For simplicity, we'll use a simple bubble sort
	for i := 0; i < len(actionEffectiveness); i++ {
		for j := i + 1; j < len(actionEffectiveness); j++ {
			if actionEffectiveness[i].effectiveness < actionEffectiveness[j].effectiveness {
				actionEffectiveness[i], actionEffectiveness[j] = actionEffectiveness[j], actionEffectiveness[i]
			}
		}
	}

	// Add actions until we hit the budget
	totalCost := 0.0
	totalRiskOffset := 0.0

	for _, ae := range actionEffectiveness {
		action := ae.action
		actionCost := math.Abs(action.Quantity * action.EstimatedPrice)

		// If adding this action exceeds budget, try to scale it down
		if totalCost+actionCost > maxCost {
			// Scale factor to fit remaining budget
			scaleFactor := (maxCost - totalCost) / actionCost
			if scaleFactor > 0 {
				// Scale down the quantity
				scaledQuantity := action.Quantity * scaleFactor
				// Round to nearest standard lot
				scaledQuantity = math.Round(scaledQuantity/100) * 100

				if scaledQuantity != 0 {
					scaledAction := action
					scaledAction.Quantity = scaledQuantity
					scaledAction.RiskOffset = action.RiskOffset * scaleFactor
					scaledAction.Reason += fmt.Sprintf(" (scaled to %.0f%% due to budget constraint)", scaleFactor*100)

					optimized.Actions = append(optimized.Actions, scaledAction)
					totalCost += math.Abs(scaledQuantity * action.EstimatedPrice)
					totalRiskOffset += math.Abs(scaledAction.RiskOffset)
				}
			}
			break
		}

		// Add action as is
		optimized.Actions = append(optimized.Actions, action)
		totalCost += actionCost
		totalRiskOffset += math.Abs(action.RiskOffset)
	}

	// Update strategy metrics
	optimized.EstimatedCost = totalCost

	// Calculate risk reduction as a percentage of original risk offset
	originalRiskOffset := 0.0
	for _, action := range strategy.Actions {
		originalRiskOffset += math.Abs(action.RiskOffset)
	}

	if originalRiskOffset > 0 {
		optimized.RiskReduction = totalRiskOffset / originalRiskOffset * strategy.RiskReduction
	} else {
		optimized.RiskReduction = 0
	}

	return optimized
}

// ImplementHedgingStrategy converts a hedging strategy into executable orders
func (hc *HedgingCalculator) ImplementHedgingStrategy(
	ctx context.Context,
	strategy *models.HedgingStrategy,
) []*models.Order {
	orders := make([]*models.Order, 0, len(strategy.Actions))

	// Convert each hedging action to an order
	for _, action := range strategy.Actions {
		// Determine order side
		var side models.OrderSide
		if action.Quantity > 0 {
			side = models.OrderSideBuy
		} else {
			side = models.OrderSideSell
		}

		// Create order
		order := &models.Order{
			OrderID:      uuid.New().String(),
			Symbol:       action.Instrument,
			Side:         side,
			Type:         models.OrderTypeMarket,
			Quantity:     math.Abs(action.Quantity),
			CreationTime: time.Now(),
			UpdateTime:   time.Now(),
			ClientID:     "HEDGE_" + strategy.PortfolioID,
		}

		orders = append(orders, order)
	}

	return orders
}
