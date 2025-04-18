package risk

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// ESMethod defines different methods for calculating Expected Shortfall
type ESMethod int

const (
	// HistoricalES uses historical simulation for ES
	HistoricalES ESMethod = iota
	// ParametricES uses parametric methods for ES
	ParametricES
	// MonteCarloES uses Monte Carlo simulation for ES
	MonteCarloES
)

// ExpectedShortfallCalculator calculates Expected Shortfall
type ExpectedShortfallCalculator struct {
	method           ESMethod
	confidenceLevel  float64
	historicalWindow int
	simulationRuns   int
	log              *logger.Logger
}

// NewExpectedShortfallCalculator creates a new ES calculator
func NewExpectedShortfallCalculator(method ESMethod, confidenceLevel float64, historicalWindow int) *ExpectedShortfallCalculator {
	if confidenceLevel <= 0 || confidenceLevel >= 1 {
		confidenceLevel = 0.975 // Default to 97.5% confidence level for ES
	}

	if historicalWindow <= 0 {
		historicalWindow = 252 // Default to 1 year of trading days
	}

	return &ExpectedShortfallCalculator{
		method:           method,
		confidenceLevel:  confidenceLevel,
		historicalWindow: historicalWindow,
		simulationRuns:   10000, // Default to 10,000 simulation runs
		log:              logger.GetLogger("risk.es"),
	}
}

// SetConfidenceLevel sets the confidence level for ES calculation
func (e *ExpectedShortfallCalculator) SetConfidenceLevel(confidenceLevel float64) {
	if confidenceLevel > 0 && confidenceLevel < 1 {
		e.confidenceLevel = confidenceLevel
	}
}

// SetSimulationRuns sets the number of simulation runs for Monte Carlo ES
func (e *ExpectedShortfallCalculator) SetSimulationRuns(runs int) {
	if runs > 0 {
		e.simulationRuns = runs
	}
}

// SetHistoricalWindow sets the number of days for historical ES
func (e *ExpectedShortfallCalculator) SetHistoricalWindow(days int) {
	if days > 0 {
		e.historicalWindow = days
	}
}

// Calculate computes the Expected Shortfall
func (e *ExpectedShortfallCalculator) Calculate(returns []float64, positionValue float64) float64 {
	if len(returns) == 0 || positionValue <= 0 {
		e.log.Warn("Invalid input for ES calculation: empty returns or non-positive position value")
		return 0
	}

	// Limit the number of returns used to the historical window
	if len(returns) > e.historicalWindow {
		returns = returns[len(returns)-e.historicalWindow:]
	}

	switch e.method {
	case HistoricalES:
		return e.calculateHistoricalES(returns, positionValue)
	case ParametricES:
		return e.calculateParametricES(returns, positionValue)
	case MonteCarloES:
		return e.calculateMonteCarloES(returns, positionValue)
	default:
		// Default to historical ES
		return e.calculateHistoricalES(returns, positionValue)
	}
}

// calculateHistoricalES calculates ES using historical simulation
func (e *ExpectedShortfallCalculator) calculateHistoricalES(returns []float64, positionValue float64) float64 {
	// Sort returns in ascending order
	sortedReturns := make([]float64, len(returns))
	copy(sortedReturns, returns)
	sort.Float64s(sortedReturns)

	// Calculate the index for the VaR threshold
	indexFloat := (1 - e.confidenceLevel) * float64(len(sortedReturns))
	index := int(math.Floor(indexFloat))

	// Handle edge cases
	if index <= 0 {
		return -sortedReturns[0] * positionValue
	}
	if index >= len(sortedReturns) {
		return 0
	}

	// Calculate the average of returns below the VaR threshold
	sum := 0.0
	for i := 0; i < index; i++ {
		sum += sortedReturns[i]
	}

	// ES is the average of the worst (1-confidenceLevel)*100% returns
	esReturn := sum / float64(index)

	// Convert return percentage to monetary value
	// We negate because losses are typically represented as negative returns
	return -esReturn * positionValue
}

// calculateParametricES calculates ES using parametric method (normal distribution)
func (e *ExpectedShortfallCalculator) calculateParametricES(returns []float64, positionValue float64) float64 {
	// Calculate mean and standard deviation of returns
	mean, stdDev := calculateMeanAndStdDev(returns)

	// Calculate z-score for the confidence level
	zScore := calculateNormalZScore(e.confidenceLevel)

	// For normal distribution, ES = mean - stdDev * phi(z) / (1-confidenceLevel)
	// Where phi(z) is the standard normal PDF at z
	phi := math.Exp(-0.5*zScore*zScore) / math.Sqrt(2*math.Pi)
	
	// ES is the expected value of returns in the worst (1-confidenceLevel)*100%
	esReturn := -(mean - stdDev*phi/(1-e.confidenceLevel))
	
	return esReturn * positionValue
}

// calculateMonteCarloES calculates ES using Monte Carlo simulation
func (e *ExpectedShortfallCalculator) calculateMonteCarloES(returns []float64, positionValue float64) float64 {
	// Calculate mean and standard deviation of historical returns
	mean, stdDev := calculateMeanAndStdDev(returns)
	
	// Generate random returns
	rand.Seed(time.Now().UnixNano())
	simulatedReturns := make([]float64, e.simulationRuns)
	
	for i := 0; i < e.simulationRuns; i++ {
		// Generate random return from normal distribution
		u1 := rand.Float64()
		u2 := rand.Float64()
		
		// Box-Muller transform
		z := math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)
		simulatedReturns[i] = mean + z*stdDev
	}
	
	// Calculate ES from simulated returns
	return e.calculateHistoricalES(simulatedReturns, positionValue)
}

// CalculateComponentES calculates the component ES for a given position in a portfolio
func (e *ExpectedShortfallCalculator) CalculateComponentES(
	positionReturns []float64,
	portfolioReturns []float64,
	positionValue float64,
	portfolioValue float64) float64 {
	
	if len(positionReturns) != len(portfolioReturns) || positionValue <= 0 || portfolioValue <= 0 {
		e.log.Warn("Invalid input for component ES calculation")
		return 0
	}
	
	// Sort the returns and track original indices
	type indexedReturn struct {
		originalIndex int
		value         float64
	}
	
	indexedPortfolioReturns := make([]indexedReturn, len(portfolioReturns))
	for i, r := range portfolioReturns {
		indexedPortfolioReturns[i] = indexedReturn{i, r}
	}
	
	sort.Slice(indexedPortfolioReturns, func(i, j int) bool {
		return indexedPortfolioReturns[i].value < indexedPortfolioReturns[j].value
	})
	
	// Calculate the index for the VaR threshold
	indexFloat := (1 - e.confidenceLevel) * float64(len(portfolioReturns))
	index := int(math.Floor(indexFloat))
	
	// Handle edge cases
	if index <= 0 {
		index = 1
	}
	if index >= len(portfolioReturns) {
		index = len(portfolioReturns) - 1
	}
	
	// Calculate the average position return corresponding to the worst portfolio returns
	sum := 0.0
	weight := positionValue / portfolioValue
	
	for i := 0; i < index; i++ {
		originalIndex := indexedPortfolioReturns[i].originalIndex
		sum += positionReturns[originalIndex]
	}
	
	// Component ES is the contribution of this position to the total ES
	componentES := -sum / float64(index) * positionValue
	
	return componentES
}

// CalculateIncrementalES calculates the incremental ES for a given position in a portfolio
func (e *ExpectedShortfallCalculator) CalculateIncrementalES(
	portfolioReturns []float64,
	portfolioReturnsWithoutPosition []float64,
	portfolioValue float64) float64 {
	
	if len(portfolioReturns) != len(portfolioReturnsWithoutPosition) || portfolioValue <= 0 {
		e.log.Warn("Invalid input for incremental ES calculation")
		return 0
	}
	
	// Calculate ES with and without the position
	esWithPosition := e.calculateHistoricalES(portfolioReturns, portfolioValue)
	esWithoutPosition := e.calculateHistoricalES(portfolioReturnsWithoutPosition, portfolioValue)
	
	// Incremental ES is the difference
	return esWithPosition - esWithoutPosition
}

// Helper functions

// calculateMeanAndStdDev calculates the mean and standard deviation of returns
func calculateMeanAndStdDev(returns []float64) (float64, float64) {
	if len(returns) == 0 {
		return 0, 0
	}

	// Calculate mean
	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculate standard deviation
	sumSquaredDiff := 0.0
	for _, r := range returns {
		diff := r - mean
		sumSquaredDiff += diff * diff
	}
	variance := sumSquaredDiff / float64(len(returns))
	stdDev := math.Sqrt(variance)

	return mean, stdDev
}

// calculateNormalZScore calculates the z-score for a given confidence level
func calculateNormalZScore(confidenceLevel float64) float64 {
	// Use approximation for the ICDF
	// This is the Acklam's algorithm for approximating the normal ICDF
	
	// For common confidence levels, just use pre-calculated values
	switch {
	case math.Abs(confidenceLevel-0.90) < 1e-6:
		return 1.282
	case math.Abs(confidenceLevel-0.95) < 1e-6:
		return 1.645
	case math.Abs(confidenceLevel-0.975) < 1e-6:
		return 1.96
	case math.Abs(confidenceLevel-0.99) < 1e-6:
		return 2.326
	case math.Abs(confidenceLevel-0.995) < 1e-6:
		return 2.576
	case math.Abs(confidenceLevel-0.999) < 1e-6:
		return 3.09
	}
	
	// For other confidence levels, use approximation
	if confidenceLevel <= 0 || confidenceLevel >= 1 {
		return 0
	}
	
	// Transform p to be between 0 and 1 (we need 1-p for VaR)
	p := 1 - confidenceLevel
	
	// Tail approximation
	if p < 0.02425 {
		// Rational approximation for lower region
		q := math.Sqrt(-2 * math.Log(p))
		return (((((0.00171799*q+0.0092705)*q+0.0214992)*q+0.0331809)*q+0.19587)*q + 0.6514410)*q + 2.3268354
	} else if p <= 0.97575 {
		// Rational approximation for central region
		q := p - 0.5
		r := q * q
		return (((((-3.3905e-5*r+0.00785)*r-0.0322)*r+0.08001)*r-0.13288)*r+0.197498)*r-1.0200)*q
	} else {
		// Rational approximation for upper region
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((0.00171799*q+0.0092705)*q+0.0214992)*q+0.0331809)*q+0.19587)*q + 0.6514410)*q - 2.3268354
	}
}
