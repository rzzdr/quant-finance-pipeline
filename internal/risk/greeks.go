package risk

import (
	"fmt"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// GreeksCalculator handles calculation of option Greeks
type GreeksCalculator struct {
	bsPricer *BlackScholesPricer
	log      *logger.Logger
}

// NewGreeksCalculator creates a new Greeks calculator
func NewGreeksCalculator(bsPricer *BlackScholesPricer) *GreeksCalculator {
	return &GreeksCalculator{
		bsPricer: bsPricer,
		log:      logger.GetLogger("risk.greeks"),
	}
}

// CalculateGreeks calculates option Greeks for a derivative
func (gc *GreeksCalculator) CalculateGreeks(derivative *models.MarketData, underlying *models.MarketData) *models.DerivativeGreeks {
	return gc.bsPricer.CalculateGreeks(derivative, underlying)
}

// CalculatePortfolioGreeks calculates the aggregate Greeks for an entire portfolio
func (gc *GreeksCalculator) CalculatePortfolioGreeks(portfolio *models.Portfolio, marketData map[string]*models.MarketData) *models.DerivativeGreeks {
	// Initialize portfolio Greeks
	portfolioGreeks := &models.DerivativeGreeks{
		Delta: 0,
		Gamma: 0,
		Theta: 0,
		Vega:  0,
		Rho:   0,
	}

	// Calculate Greeks for each position and aggregate
	for _, position := range portfolio.Positions {
		// Get market data for the position
		positionData, exists := marketData[position.Symbol]
		if !exists {
			gc.log.Warnf("No market data found for position %s, skipping Greeks calculation", position.Symbol)
			continue
		}

		// If this is a derivative position, calculate Greeks
		if position.IsDerivative && position.DerivativeInfo != nil {
			// Get market data for the underlying
			underlyingData, exists := marketData[position.DerivativeInfo.UnderlyingSymbol]
			if !exists {
				gc.log.Warnf("No market data found for underlying %s, skipping Greeks calculation",
					position.DerivativeInfo.UnderlyingSymbol)
				continue
			}

			// Calculate Greeks for this position
			greeks := gc.bsPricer.CalculateGreeks(positionData, underlyingData)

			// Aggregate Greeks weighted by position size
			portfolioGreeks.Delta += greeks.Delta * position.Quantity
			portfolioGreeks.Gamma += greeks.Gamma * position.Quantity
			portfolioGreeks.Theta += greeks.Theta * position.Quantity
			portfolioGreeks.Vega += greeks.Vega * position.Quantity
			portfolioGreeks.Rho += greeks.Rho * position.Quantity
		} else if !position.IsDerivative {
			// For non-derivative positions, only Delta matters (1 for long, -1 for short)
			portfolioGreeks.Delta += position.Quantity
		}
	}

	return portfolioGreeks
}

// CalculatePositionGreeks calculates Greeks for a single position
func (gc *GreeksCalculator) CalculatePositionGreeks(
	position models.Position,
	positionData *models.MarketData,
	underlyingData *models.MarketData,
) *models.DerivativeGreeks {
	if !position.IsDerivative || position.DerivativeInfo == nil || positionData == nil || underlyingData == nil {
		return nil
	}

	greeks := gc.bsPricer.CalculateGreeks(positionData, underlyingData)

	// Adjust Greeks by position size
	scaledGreeks := &models.DerivativeGreeks{
		Delta: greeks.Delta * position.Quantity,
		Gamma: greeks.Gamma * position.Quantity,
		Theta: greeks.Theta * position.Quantity,
		Vega:  greeks.Vega * position.Quantity,
		Rho:   greeks.Rho * position.Quantity,
	}

	return scaledGreeks
}

// CalculatePortfolioNetExposure calculates the net delta exposure by underlying symbol
func (gc *GreeksCalculator) CalculatePortfolioNetExposure(
	portfolio *models.Portfolio,
	marketData map[string]*models.MarketData,
) map[string]float64 {
	// Map to hold net exposure by underlying symbol
	netExposure := make(map[string]float64)

	// Calculate exposure for each position
	for _, position := range portfolio.Positions {
		// Get market data for the position
		positionData, exists := marketData[position.Symbol]
		if !exists {
			gc.log.Warnf("No market data found for position %s, skipping exposure calculation", position.Symbol)
			continue
		}

		if position.IsDerivative && position.DerivativeInfo != nil {
			// Get underlying symbol
			underlyingSymbol := position.DerivativeInfo.UnderlyingSymbol

			// Get market data for the underlying
			underlyingData, exists := marketData[underlyingSymbol]
			if !exists {
				gc.log.Warnf("No market data found for underlying %s, skipping exposure calculation",
					underlyingSymbol)
				continue
			}

			// Calculate Greeks for this position
			greeks := gc.bsPricer.CalculateGreeks(positionData, underlyingData)

			// Add delta exposure to the underlying
			netExposure[underlyingSymbol] += greeks.Delta * position.Quantity
		} else {
			// For non-derivative positions, the exposure is the position itself
			netExposure[position.Symbol] += position.Quantity
		}
	}

	return netExposure
}

// ProjectGreeksChange projects how Greeks will change over time or with market movements
func (gc *GreeksCalculator) ProjectGreeksChange(
	derivative *models.MarketData,
	underlying *models.MarketData,
	daysForward int,
	priceMovePercent float64,
) *models.DerivativeGreeks {
	// Calculate current Greeks
	currentGreeks := gc.bsPricer.CalculateGreeks(derivative, underlying)

	// Create copies of the market data for projection
	projectedDerivative := deriveProjectedMarketData(derivative, daysForward, priceMovePercent)
	projectedUnderlying := deriveProjectedMarketData(underlying, daysForward, priceMovePercent)

	// Calculate projected Greeks
	projectedGreeks := gc.bsPricer.CalculateGreeks(projectedDerivative, projectedUnderlying)

	// Calculate change in Greeks
	greeksChange := &models.DerivativeGreeks{
		Delta: projectedGreeks.Delta - currentGreeks.Delta,
		Gamma: projectedGreeks.Gamma - currentGreeks.Gamma,
		Theta: projectedGreeks.Theta - currentGreeks.Theta,
		Vega:  projectedGreeks.Vega - currentGreeks.Vega,
		Rho:   projectedGreeks.Rho - currentGreeks.Rho,
	}

	return greeksChange
}

// CreateGreeksSurface generates a surface of Greek values across different strikes and expirations
func (gc *GreeksCalculator) CreateGreeksSurface(
	underlying *models.MarketData,
	strikes []float64,
	expiryDays []int,
	volatility float64,
	optionType models.OptionType,
) map[string]map[string]map[string]*models.DerivativeGreeks {
	// Initialize the surface map
	// Format: surface[expiry][strike][greek_type] = value
	surface := make(map[string]map[string]map[string]*models.DerivativeGreeks)

	// Base derivative for cloning
	baseDerivative := &models.MarketData{
		Symbol:    "TEMP_OPTION",
		Price:     0, // Will be calculated
		Timestamp: time.Now(),
		DerivativeInfo: &models.DerivativeInfo{
			Type:              models.DerivativeTypeOption,
			UnderlyingSymbol:  underlying.Symbol,
			ImpliedVolatility: volatility,
			OptionType:        optionType,
		},
	}

	// Generate surface points
	for _, days := range expiryDays {
		// Create expiry entry if it doesn't exist
		expiryKey := fmt.Sprintf("%d", days)
		if _, exists := surface[expiryKey]; !exists {
			surface[expiryKey] = make(map[string]map[string]*models.DerivativeGreeks)
		}

		// Set expiry date
		expiry := time.Now().AddDate(0, 0, days)

		for _, strike := range strikes {
			// Create strike entry if it doesn't exist
			strikeKey := fmt.Sprintf("%.2f", strike)
			if _, exists := surface[expiryKey][strikeKey]; !exists {
				surface[expiryKey][strikeKey] = make(map[string]*models.DerivativeGreeks)
			}

			// Create a derivative with this strike and expiry
			derivative := *baseDerivative
			derivative.DerivativeInfo = &models.DerivativeInfo{
				Type:              models.DerivativeTypeOption,
				UnderlyingSymbol:  underlying.Symbol,
				StrikePrice:       strike,
				ExpiryDate:        expiry,
				ImpliedVolatility: volatility,
				OptionType:        optionType,
			}

			// Calculate Greeks
			greeks := gc.bsPricer.CalculateGreeks(&derivative, underlying)

			// Store in surface
			surface[expiryKey][strikeKey]["greeks"] = greeks
		}
	}

	return surface
}

// deriveProjectedMarketData creates a copy of market data with projected changes
func deriveProjectedMarketData(md *models.MarketData, daysForward int, priceMovePercent float64) *models.MarketData {
	// Create a copy
	projected := md.DeepCopy()

	// Adjust price based on percent move
	priceChange := md.Price * priceMovePercent / 100.0
	projected.Price += priceChange

	// Adjust timestamp
	projected.Timestamp = md.Timestamp.AddDate(0, 0, daysForward)

	// For derivatives, adjust expiry date if present
	if projected.DerivativeInfo != nil && !projected.DerivativeInfo.ExpiryDate.IsZero() {
		// Time to expiry is reduced by daysForward
		// But we don't change the actual expiry date
	}

	return projected
}
