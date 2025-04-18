package risk

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// VaRMethod defines the method used for VaR calculation
type VaRMethod int

const (
	// HistoricalVaR uses historical returns
	HistoricalVaR VaRMethod = iota
	// ParametricVaR uses normal distribution assumption
	ParametricVaR
	// MonteCarloVaR uses simulation
	MonteCarloVaR
)

// VaRCalculator calculates Value at Risk
type VaRCalculator struct {
	method           VaRMethod
	confidenceLevel  float64
	historicalWindow int
	log              *logger.Logger
	simulationRuns   int
	rng              *rand.Rand
}

// NewVaRCalculator creates a new Value at Risk calculator
func NewVaRCalculator(method VaRMethod, confidenceLevel float64, historicalWindow int) *VaRCalculator {
	if confidenceLevel <= 0 || confidenceLevel >= 1 {
		confidenceLevel = 0.99 // Default to 99% confidence level
	}

	if historicalWindow <= 0 {
		historicalWindow = 252 // Default to 1 year of trading days
	}

	return &VaRCalculator{
		method:           method,
		confidenceLevel:  confidenceLevel,
		historicalWindow: historicalWindow,
		log:              logger.GetLogger("risk.var"),
		simulationRuns:   10000, // Default simulation runs
		rng:              rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SetSimulationRuns sets the number of simulation runs for Monte Carlo VaR
func (v *VaRCalculator) SetSimulationRuns(runs int) {
	if runs > 0 {
		v.simulationRuns = runs
	}
}

// SetConfidenceLevel sets the confidence level for VaR calculation
func (v *VaRCalculator) SetConfidenceLevel(confidenceLevel float64) {
	if confidenceLevel > 0 && confidenceLevel < 1 {
		v.confidenceLevel = confidenceLevel
	}
}

// SetHistoricalWindow sets the number of days for historical VaR
func (v *VaRCalculator) SetHistoricalWindow(days int) {
	if days > 0 {
		v.historicalWindow = days
	}
}

// CalculateVaR calculates VaR for a position
func (v *VaRCalculator) CalculateVaR(returns []float64, positionValue float64) float64 {
	if len(returns) == 0 || positionValue <= 0 {
		v.log.Warn("Invalid input for VaR calculation: empty returns or non-positive position value")
		return 0
	}

	// Truncate returns if we have more than we need
	if len(returns) > v.historicalWindow && v.historicalWindow > 0 {
		returns = returns[len(returns)-v.historicalWindow:]
	}

	switch v.method {
	case HistoricalVaR:
		return v.calculateHistoricalVaR(returns, positionValue)
	case ParametricVaR:
		return v.calculateParametricVaR(returns, positionValue)
	case MonteCarloVaR:
		return v.calculateMonteCarloVaR(returns, positionValue)
	default:
		return v.calculateHistoricalVaR(returns, positionValue)
	}
}

// calculateHistoricalVaR calculates VaR using historical simulation
func (v *VaRCalculator) calculateHistoricalVaR(returns []float64, positionValue float64) float64 {
	// Sort returns in ascending order
	sortedReturns := make([]float64, len(returns))
	copy(sortedReturns, returns)
	sort.Float64s(sortedReturns)

	// Calculate the index for the quantile
	index := int(math.Floor((1.0 - v.confidenceLevel) * float64(len(sortedReturns))))
	if index < 0 {
		index = 0
	}

	// Get the return at the quantile
	quantileReturn := sortedReturns[index]

	// VaR is the absolute value of the loss (negative return) at the confidence level
	// multiplied by the position value
	return math.Abs(math.Min(quantileReturn, 0)) * positionValue
}

// calculateParametricVaR calculates VaR using parametric method (normal distribution)
func (v *VaRCalculator) calculateParametricVaR(returns []float64, positionValue float64) float64 {
	// Calculate mean and standard deviation of returns
	mean, stdDev := calculateMeanAndStdDev(returns)

	// Calculate z-score for the confidence level
	zScore := calculateZScore(v.confidenceLevel)

	// Calculate VaR
	// For normal distribution, VaR = -mean + z*stdDev (for losses)
	varReturn := -mean + zScore*stdDev

	// VaR in currency units
	return math.Max(0, varReturn) * positionValue
}

// calculateMonteCarloVaR calculates VaR using Monte Carlo simulation
func (v *VaRCalculator) calculateMonteCarloVaR(returns []float64, positionValue float64) float64 {
	// Calculate mean and standard deviation of returns
	mean, stdDev := calculateMeanAndStdDev(returns)

	// Simulate returns
	simulatedReturns := make([]float64, v.simulationRuns)
	for i := 0; i < v.simulationRuns; i++ {
		// Generate random return from normal distribution with given mean and stdDev
		randomReturn := v.generateNormalReturn(mean, stdDev)
		simulatedReturns[i] = randomReturn
	}

	// Use historical VaR method on the simulated returns
	return v.calculateHistoricalVaR(simulatedReturns, positionValue)
}

// calculateMeanAndStdDev calculates the mean and standard deviation of returns
func calculateMeanAndStdDev(returns []float64) (float64, float64) {
	if len(returns) == 0 {
		return 0, 0
	}

	// Calculate mean
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculate variance
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	variance /= float64(len(returns))

	// Return mean and standard deviation
	return mean, math.Sqrt(variance)
}

// calculateZScore calculates the z-score for a given confidence level
func calculateZScore(confidenceLevel float64) float64 {
	// Approximate the inverse normal CDF (probit function)
	// This is a reasonable approximation for common confidence levels

	// For 99% confidence (0.99), z ≈ 2.33
	// For 95% confidence (0.95), z ≈ 1.65
	// For 90% confidence (0.90), z ≈ 1.28

	switch {
	case confidenceLevel >= 0.99:
		return 2.326 // 99%
	case confidenceLevel >= 0.975:
		return 1.96 // 97.5%
	case confidenceLevel >= 0.95:
		return 1.645 // 95%
	case confidenceLevel >= 0.90:
		return 1.282 // 90%
	default:
		// Linear approximation for other values
		return math.Sqrt(2) * math.Sqrt(-math.Log(2*(1-confidenceLevel)))
	}
}

// generateNormalReturn generates a random return from a normal distribution
func (v *VaRCalculator) generateNormalReturn(mean float64, stdDev float64) float64 {
	// Box-Muller transform to generate a standard normal random variable
	u1 := v.rng.Float64()
	u2 := v.rng.Float64()

	// Convert uniform random variables to normal random variable
	z := math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)

	// Scale and shift to get desired mean and standard deviation
	return mean + stdDev*z
}

// BootstrapVaR calculates VaR using bootstrap resampling
func (v *VaRCalculator) BootstrapVaR(returns []float64, positionValue float64, bootstrapSamples int) float64 {
	if len(returns) == 0 || bootstrapSamples <= 0 {
		return 0
	}

	bootstrapReturns := make([]float64, bootstrapSamples)
	n := len(returns)

	// Generate bootstrap samples
	for i := 0; i < bootstrapSamples; i++ {
		// Sample with replacement
		sampleSum := 0.0
		for j := 0; j < n; j++ {
			randIndex := int(rand.Float64() * float64(n))
			if randIndex >= n {
				randIndex = n - 1
			}
			sampleSum += returns[randIndex]
		}
		bootstrapReturns[i] = sampleSum / float64(n)
	}

	// Calculate VaR using the bootstrap sample
	return v.calculateHistoricalVaR(bootstrapReturns, positionValue)
}

// CalculateConditionalVaR calculates Conditional VaR (CVaR), also known as Expected Shortfall (ES),
// which is the expected loss given that VaR is exceeded
func (v *VaRCalculator) CalculateConditionalVaR(returns []float64, positionValue float64) float64 {
	// Sort returns in ascending order
	sortedReturns := make([]float64, len(returns))
	copy(sortedReturns, returns)
	sort.Float64s(sortedReturns)

	// Calculate the index for the VaR quantile
	indexFloat := (1 - v.confidenceLevel) * float64(len(sortedReturns))
	index := int(math.Floor(indexFloat))

	// Handle edge cases
	if index <= 0 {
		return -sortedReturns[0] * positionValue
	} else if index >= len(sortedReturns) {
		return 0
	}

	// Calculate the average of returns below the VaR threshold
	sum := 0.0
	for i := 0; i < index; i++ {
		sum += sortedReturns[i]
	}

	// CVaR is the average of the returns in the tail
	cvarReturn := sum / float64(index)

	// Negate for the same reason as VaR
	return -cvarReturn * positionValue
}
