package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// CalculatorConfig contains configuration for the risk calculator
type CalculatorConfig struct {
	VaRConfidenceLevel float64
	ESConfidenceLevel  float64
	SimulationRuns     int
	HistoricalDays     int
	WorkerCount        int
}

// Calculator performs risk calculations for portfolios
type Calculator struct {
	config              CalculatorConfig
	varCalculator       *VaRCalculator
	esCalculator        *ExpectedShortfallCalculator
	bsPricer            *BlackScholesPricer
	portfolioStore      PortfolioStore
	historicalDataStore HistoricalDataStore
	log                 *logger.Logger
}

// PortfolioStore defines an interface for storing and retrieving portfolios
type PortfolioStore interface {
	GetPortfolio(id string) (*models.Portfolio, error)
	GetAllPortfolios() ([]*models.Portfolio, error)
	SavePortfolio(portfolio *models.Portfolio) error
	DeletePortfolio(id string) error
}

// HistoricalDataStore defines an interface for retrieving historical market data
type HistoricalDataStore interface {
	GetHistoricalReturns(symbol string, days int) ([]float64, error)
	GetHistoricalPrices(symbol string, days int) ([]float64, error)
}

// NewCalculator creates a new risk calculator
func NewCalculator(config CalculatorConfig, portfolioStore PortfolioStore, historicalDataStore HistoricalDataStore) *Calculator {
	// Initialize with default values if not provided
	if config.VaRConfidenceLevel <= 0 {
		config.VaRConfidenceLevel = 0.99 // 99% confidence level
	}

	if config.ESConfidenceLevel <= 0 {
		config.ESConfidenceLevel = 0.975 // 97.5% confidence level
	}

	if config.SimulationRuns <= 0 {
		config.SimulationRuns = 10000 // 10,000 simulation runs
	}

	if config.HistoricalDays <= 0 {
		config.HistoricalDays = 252 // One trading year
	}

	if config.WorkerCount <= 0 {
		config.WorkerCount = 4 // Default to 4 workers
	}

	// Create VaR calculator
	varCalculator := NewVaRCalculator(HistoricalVaR, config.VaRConfidenceLevel, config.HistoricalDays)
	varCalculator.SetSimulationRuns(config.SimulationRuns)

	// Create ES calculator
	esCalculator := NewExpectedShortfallCalculator(HistoricalES, config.ESConfidenceLevel, config.HistoricalDays)
	esCalculator.SetSimulationRuns(config.SimulationRuns)

	// Create Black-Scholes pricer
	bsPricer := NewBlackScholesPricer()

	return &Calculator{
		config:              config,
		varCalculator:       varCalculator,
		esCalculator:        esCalculator,
		bsPricer:            bsPricer,
		portfolioStore:      portfolioStore,
		historicalDataStore: historicalDataStore,
		log:                 logger.GetLogger("risk.calculator"),
	}
}

// CalculateRiskMetrics calculates risk metrics for a portfolio
func (c *Calculator) CalculateRiskMetrics(ctx context.Context, portfolioID string) (*models.RiskMetrics, error) {
	startTime := time.Now()
	c.log.Infof("Starting risk calculation for portfolio %s", portfolioID)

	// Get portfolio
	portfolio, err := c.portfolioStore.GetPortfolio(portfolioID)
	if err != nil {
		c.log.Errorf("Failed to get portfolio %s: %v", portfolioID, err)
		return nil, err
	}

	// Create risk metrics object
	riskMetrics := models.NewRiskMetrics(portfolioID, 0, 0, 0)
	riskMetrics.Timestamp = time.Now()

	// Calculate portfolio value and collect position data for VaR calculation
	var totalValue float64
	positionReturns := make(map[string][]float64)
	positionValues := make(map[string]float64)

	// Process each position in the portfolio
	for _, position := range portfolio.Positions {
		// Get historical returns for position
		returns, err := c.historicalDataStore.GetHistoricalReturns(position.Symbol, c.config.HistoricalDays)
		if err != nil {
			c.log.Warnf("Failed to get historical returns for %s: %v, skipping position", position.Symbol, err)
			continue
		}

		// Get historical prices for position
		prices, err := c.historicalDataStore.GetHistoricalPrices(position.Symbol, 1)
		if err != nil {
			c.log.Warnf("Failed to get current price for %s: %v, skipping position", position.Symbol, err)
			continue
		}

		// Calculate position value
		currentPrice := prices[len(prices)-1]
		positionValue := currentPrice * position.Quantity
		totalValue += positionValue

		// Store returns and values for later calculations
		positionReturns[position.Symbol] = returns
		positionValues[position.Symbol] = positionValue
	}

	// Store portfolio value
	riskMetrics.PortfolioValue = totalValue

	// Calculate portfolio returns by weighting position returns
	portfolioReturns := calculatePortfolioReturns(positionReturns, positionValues, totalValue)

	// Calculate Value at Risk
	portfolioVaR := c.varCalculator.CalculateVaR(portfolioReturns, totalValue)
	riskMetrics.ValueAtRisk = portfolioVaR

	// Calculate Expected Shortfall
	portfolioES := c.esCalculator.Calculate(portfolioReturns, totalValue)
	riskMetrics.ExpectedShortfall = portfolioES

	// Calculate individual position risks
	c.calculatePositionRisks(portfolio, positionReturns, positionValues, portfolioES, riskMetrics)

	c.log.Infof("Completed risk calculation for portfolio %s in %v", portfolioID, time.Since(startTime))
	return riskMetrics, nil
}

// calculatePortfolioReturns calculates weighted portfolio returns from position returns
func calculatePortfolioReturns(positionReturns map[string][]float64, positionValues map[string]float64, totalValue float64) []float64 {
	if totalValue <= 0 {
		return []float64{}
	}

	// Find the length of return series (assume all are the same length)
	var returnLength int
	for _, returns := range positionReturns {
		if len(returns) > 0 {
			returnLength = len(returns)
			break
		}
	}

	if returnLength == 0 {
		return []float64{}
	}

	// Calculate weighted portfolio returns
	portfolioReturns := make([]float64, returnLength)
	for symbol, returns := range positionReturns {
		weight := positionValues[symbol] / totalValue
		for i, ret := range returns {
			if i < returnLength {
				portfolioReturns[i] += ret * weight
			}
		}
	}

	return portfolioReturns
}

// calculatePositionRisks calculates risk metrics for individual positions
func (c *Calculator) calculatePositionRisks(portfolio *models.Portfolio, positionReturns map[string][]float64,
	positionValues map[string]float64, portfolioES float64, riskMetrics *models.RiskMetrics) {

	type positionRiskJob struct {
		symbol       string
		returns      []float64
		positionSize float64
		value        float64
		info         *models.RiskDerivativeInfo
	}

	type positionRiskResult struct {
		symbol string
		risk   models.PositionRisk
		err    error
	}

	// Create job and result channels
	jobs := make(chan positionRiskJob, len(portfolio.Positions))
	results := make(chan positionRiskResult, len(portfolio.Positions))

	// Start workers
	var wg sync.WaitGroup
	workerCount := min(c.config.WorkerCount, len(portfolio.Positions))

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				// Calculate position VaR
				posVaR := c.varCalculator.CalculateVaR(job.returns, job.value)

				// Calculate position ES
				posES := c.esCalculator.Calculate(job.returns, job.value)

				// Calculate contribution to portfolio risk
				contribution := posES / portfolioES * job.value / riskMetrics.PortfolioValue

				// Create position risk object
				var greeks *models.DerivativeGreeks
				if job.info != nil && job.info.Greeks != nil {
					greeks = &models.DerivativeGreeks{
						Delta: job.info.Greeks.Delta,
						Gamma: job.info.Greeks.Gamma,
						Theta: job.info.Greeks.Theta,
						Vega:  job.info.Greeks.Vega,
						Rho:   job.info.Greeks.Rho,
					}
				}

				risk := models.NewPositionRisk(job.symbol, job.positionSize, job.value, posVaR, posES, contribution, greeks)
				results <- positionRiskResult{symbol: job.symbol, risk: risk}
			}
		}()
	}

	// Submit jobs
	for _, position := range portfolio.Positions {
		if returns, ok := positionReturns[position.Symbol]; ok {
			value := positionValues[position.Symbol]
			jobs <- positionRiskJob{
				symbol:       position.Symbol,
				returns:      returns,
				positionSize: position.Quantity,
				value:        value,
				info:         position.DerivativeInfo,
			}
		}
	}
	close(jobs)

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	riskMetrics.PositionRisks = make(map[string]models.PositionRisk)
	for result := range results {
		if result.err != nil {
			c.log.Warnf("Error calculating risk for position %s: %v", result.symbol, result.err)
			continue
		}
		riskMetrics.PositionRisks[result.symbol] = result.risk
	}
}

// RunStressTest runs a stress test on a portfolio
func (c *Calculator) RunStressTest(ctx context.Context, portfolioID string, scenarios []models.StressScenario) (*models.StressTestResults, error) {
	c.log.Infof("Starting stress test for portfolio %s", portfolioID)

	// Get portfolio
	portfolio, err := c.portfolioStore.GetPortfolio(portfolioID)
	if err != nil {
		c.log.Errorf("Failed to get portfolio %s: %v", portfolioID, err)
		return nil, err
	}

	// Create stress test results
	results := &models.StressTestResults{
		Scenarios: make([]models.StressScenario, 0, len(scenarios)),
	}

	// Run each scenario
	for _, scenario := range scenarios {
		// Simulate scenario impact
		c.applyScenario(portfolio, &scenario)
		results.Scenarios = append(results.Scenarios, scenario)
	}

	return results, nil
}

// applyScenario applies a stress scenario to a portfolio
func (c *Calculator) applyScenario(portfolio *models.Portfolio, scenario *models.StressScenario) {
	// Initialize value change maps if not present
	if scenario.PositionValueChanges == nil {
		scenario.PositionValueChanges = make(map[string]float64)
	}

	var portfolioValue, portfolioValueAfterShock float64

	// Calculate initial portfolio value and impact of scenario
	for _, position := range portfolio.Positions {
		// Get position value (simplified here)
		positionValue := position.Quantity * 100 // Placeholder, would use actual price

		portfolioValue += positionValue

		// Apply shock based on asset type/characteristics
		var valueChangePercent float64

		// Try to get specific shock for this position
		if specificChange, ok := scenario.PositionValueChanges[position.Symbol]; ok {
			valueChangePercent = specificChange
		} else {
			// Apply default shock based on scenario
			if position.IsDerivative && position.DerivativeInfo != nil {
				// For derivatives, apply more complex shock based on Greeks
				valueChangePercent = calculateDerivativeScenarioImpact(position)
			} else {
				// For other assets, use a simple percentage shock
				valueChangePercent = -0.10 // Default 10% drop
			}
		}

		// Calculate value after shock
		valueAfterShock := positionValue * (1 + valueChangePercent)
		portfolioValueAfterShock += valueAfterShock

		// Store individual position change
		scenario.PositionValueChanges[position.Symbol] = valueChangePercent
	}

	// Calculate portfolio level changes
	scenario.PortfolioValueChange = portfolioValueAfterShock - portfolioValue
	if portfolioValue > 0 {
		scenario.PercentageChange = scenario.PortfolioValueChange / portfolioValue
	}
}

// calculateDerivativeScenarioImpact calculates the impact of a scenario on a derivative position
func calculateDerivativeScenarioImpact(position models.Position) float64 {
	// This is a simplified model for scenario impact
	// In a real system, would use the position's Greeks and scenario parameters

	if position.DerivativeInfo == nil || position.DerivativeInfo.Greeks == nil {
		return -0.15 // Default 15% drop for derivatives
	}

	// Simulate a 10% drop in the underlying
	underlyingShock := -0.10

	// Calculate impact using Delta and Gamma
	delta := position.DerivativeInfo.Greeks.Delta
	gamma := position.DerivativeInfo.Greeks.Gamma

	// Delta effect + Gamma effect (second-order)
	return delta*underlyingShock + 0.5*gamma*underlyingShock*underlyingShock
}

// GenerateHedgingStrategy generates a hedging strategy for a portfolio
// This method is maintained for backwards compatibility and now supports the
// strategyType parameter to match the HedgingCalculator interface
func (c *Calculator) GenerateHedgingStrategy(ctx context.Context, portfolioOrID interface{}, strategyType ...HedgingStrategyType) (*models.HedgingStrategy, error) {
	var portfolio *models.Portfolio
	var portfolioID string
	var err error

	// Handle different types of first argument (for backwards compatibility)
	switch v := portfolioOrID.(type) {
	case string:
		portfolioID = v
		c.log.Infof("Generating hedging strategy for portfolio %s", portfolioID)
		portfolio, err = c.portfolioStore.GetPortfolio(portfolioID)
		if err != nil {
			c.log.Errorf("Failed to get portfolio %s: %v", portfolioID, err)
			return nil, err
		}
	case *models.Portfolio:
		portfolio = v
		portfolioID = portfolio.ID
		c.log.Infof("Generating hedging strategy for portfolio %s", portfolioID)
	default:
		return nil, fmt.Errorf("invalid portfolio argument type: %T", portfolioOrID)
	}

	// Use default strategy type if not provided
	var selectedStrategyType HedgingStrategyType = HedgingStrategyTypeDelta
	if len(strategyType) > 0 {
		selectedStrategyType = strategyType[0]
	}

	// Calculate portfolio risk before hedging
	riskMetrics, err := c.CalculateRiskMetrics(ctx, portfolioID)
	if err != nil {
		c.log.Errorf("Failed to calculate risk metrics for portfolio %s: %v", portfolioID, err)
		return nil, err
	}

	// Create hedging strategy
	strategy := &models.HedgingStrategy{
		PortfolioID:   portfolioID,
		Timestamp:     time.Now(),
		Actions:       make([]models.HedgingAction, 0),
		EstimatedCost: 0,
		RiskReduction: 0,
		CurrentRisk:   riskMetrics.ValueAtRisk,
	}

	// Generate hedging actions based on strategy type
	var actions []models.HedgingAction
	var cost, riskReduction float64

	switch selectedStrategyType {
	case HedgingStrategyTypeDeltaGamma, HedgingStrategyTypeMinimumVariance:
		// For advanced strategies, just log a warning and fall back to delta hedging
		c.log.Warnf("Strategy type %v not fully implemented in Calculator, falling back to delta hedging", selectedStrategyType)
		fallthrough
	default: // HedgingStrategyTypeDelta
		actions, cost, riskReduction = c.generateHedgingActions(portfolio, riskMetrics)
	}

	strategy.Actions = actions
	strategy.EstimatedCost = cost
	strategy.RiskReduction = riskReduction

	// Generate recommendations
	strategy.Recommendations = c.generateHedgingRecommendations(riskMetrics)

	return strategy, nil
}

// generateHedgingActions generates hedging actions for a portfolio
func (c *Calculator) generateHedgingActions(portfolio *models.Portfolio, riskMetrics *models.RiskMetrics) ([]models.HedgingAction, float64, float64) {
	actions := make([]models.HedgingAction, 0)
	var totalCost, totalRiskReduction float64

	// Analyze portfolio and create hedging actions
	// This is a simplified implementation

	netDelta := 0.0

	// Compute net delta exposure
	for _, position := range portfolio.Positions {
		if position.IsDerivative && position.DerivativeInfo != nil && position.DerivativeInfo.Greeks != nil {
			delta := position.DerivativeInfo.Greeks.Delta * position.Quantity
			netDelta += delta
		}
	}

	// If we have significant delta exposure, create a hedging action
	if abs(netDelta) > 0.1 {
		// Create a hedging action to offset delta
		estimatedPrice := 100.0     // Placeholder price
		quantity := -netDelta / 1.0 // Simple 1:1 hedge

		action := models.HedgingAction{
			Instrument:     "ES_FUTURE", // Example instrument
			Quantity:       quantity,
			EstimatedPrice: estimatedPrice,
			Reason:         "Delta hedge to offset net exposure",
			RiskOffset:     abs(netDelta),
		}

		actions = append(actions, action)
		totalCost += abs(quantity*estimatedPrice) * 0.0001                  // Assume 1bp transaction cost
		totalRiskReduction += abs(netDelta) / riskMetrics.ValueAtRisk * 100 // As percentage of VAR
	}

	return actions, totalCost, totalRiskReduction
}

// generateHedgingRecommendations generates additional hedging recommendations based on risk metrics
func (c *Calculator) generateHedgingRecommendations(riskMetrics *models.RiskMetrics) []models.HedgingRecommendation {
	recommendations := make([]models.HedgingRecommendation, 0)

	// Look for the positions that contribute most to overall risk
	for symbol, posRisk := range riskMetrics.PositionRisks {
		if posRisk.ContributionToRisk > 0.2 { // Position contributes >20% to total risk
			// Recommend hedging
			hedgeInstrument := "PUT_OPTION_" + symbol // Example hedge instrument

			recommendation := models.HedgingRecommendation{
				Symbol:        hedgeInstrument,
				Quantity:      posRisk.PositionSize * 0.5,   // Hedge 50% of position
				EstimatedCost: posRisk.PositionValue * 0.02, // Assume 2% of position value as premium
				Description:   "Consider buying put options to hedge high-risk position " + symbol,
			}

			recommendations = append(recommendations, recommendation)
		}
	}

	return recommendations
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetPortfolio retrieves a portfolio by ID
func (c *Calculator) GetPortfolio(id string) (*models.Portfolio, error) {
	portfolio, err := c.portfolioStore.GetPortfolio(id)
	if err != nil {
		c.log.Errorf("Failed to get portfolio %s: %v", id, err)
		return nil, err
	}
	return portfolio, nil
}
