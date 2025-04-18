package risk

import (
	"math"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// DerivativePricer interface defines methods for pricing derivatives
type DerivativePricer interface {
	Price(derivative *models.MarketData, underlying *models.MarketData) float64
	CalculateGreeks(derivative *models.MarketData, underlying *models.MarketData) *models.DerivativeGreeks
}

// BlackScholesPricer implements the Black-Scholes option pricing model
type BlackScholesPricer struct {
	riskFreeRate  float64
	dividendYield float64
	log           *logger.Logger
}

// NewBlackScholesPricer creates a new Black-Scholes pricer
func NewBlackScholesPricer() *BlackScholesPricer {
	return &BlackScholesPricer{
		riskFreeRate:  0.02, // Default 2%
		dividendYield: 0.01, // Default 1%
		log:           logger.GetLogger("risk.blackscholes"),
	}
}

// SetRiskFreeRate sets the risk-free rate
func (bs *BlackScholesPricer) SetRiskFreeRate(rate float64) {
	bs.riskFreeRate = rate
}

// SetDividendYield sets the dividend yield
func (bs *BlackScholesPricer) SetDividendYield(yield float64) {
	bs.dividendYield = yield
}

// Price calculates the price of an option
func (bs *BlackScholesPricer) Price(derivative *models.MarketData, underlying *models.MarketData) float64 {
	if derivative == nil || underlying == nil {
		bs.log.Error("Cannot price option: derivative or underlying is nil")
		return 0
	}

	if derivative.DerivativeInfo == nil {
		bs.log.Error("Cannot price option: derivative info is nil")
		return 0
	}

	// Extract option parameters
	S := underlying.Price                                // Current underlying price
	K := derivative.DerivativeInfo.StrikePrice           // Strike price
	r := bs.riskFreeRate                                 // Risk-free rate
	q := bs.dividendYield                                // Dividend yield
	sigma := derivative.DerivativeInfo.ImpliedVolatility // Volatility

	// Calculate time to expiry in years
	T := bs.calculateTimeToExpiry(derivative.DerivativeInfo.ExpiryDate)

	// Check for invalid inputs
	if S <= 0 || K <= 0 || T <= 0 || sigma <= 0 {
		bs.log.Errorf("Invalid inputs for option pricing: S=%f, K=%f, T=%f, sigma=%f", S, K, T, sigma)
		return 0
	}

	// Determine option type and calculate price
	if derivative.DerivativeInfo.Type != models.DerivativeTypeOption {
		bs.log.Error("Cannot price non-option derivative with Black-Scholes")
		return 0
	}

	if derivative.DerivativeInfo.OptionType == models.OptionTypeCall {
		return bs.calculateCallPrice(S, K, r, q, T, sigma)
	} else {
		return bs.calculatePutPrice(S, K, r, q, T, sigma)
	}
}

// calculateCallPrice calculates the price of a call option
func (bs *BlackScholesPricer) calculateCallPrice(S, K, r, q, T, sigma float64) float64 {
	d1 := (math.Log(S/K) + (r-q+0.5*sigma*sigma)*T) / (sigma * math.Sqrt(T))
	d2 := d1 - sigma*math.Sqrt(T)

	return S*math.Exp(-q*T)*normalCDF(d1) - K*math.Exp(-r*T)*normalCDF(d2)
}

// calculatePutPrice calculates the price of a put option
func (bs *BlackScholesPricer) calculatePutPrice(S, K, r, q, T, sigma float64) float64 {
	d1 := (math.Log(S/K) + (r-q+0.5*sigma*sigma)*T) / (sigma * math.Sqrt(T))
	d2 := d1 - sigma*math.Sqrt(T)

	return K*math.Exp(-r*T)*normalCDF(-d2) - S*math.Exp(-q*T)*normalCDF(-d1)
}

// CalculateGreeks calculates the Greeks for an option
func (bs *BlackScholesPricer) CalculateGreeks(derivative *models.MarketData, underlying *models.MarketData) *models.DerivativeGreeks {
	if derivative == nil || underlying == nil || derivative.DerivativeInfo == nil {
		bs.log.Error("Cannot calculate Greeks: invalid inputs")
		return &models.DerivativeGreeks{}
	}

	// Extract option parameters
	S := underlying.Price
	K := derivative.DerivativeInfo.StrikePrice
	r := bs.riskFreeRate
	q := bs.dividendYield
	sigma := derivative.DerivativeInfo.ImpliedVolatility
	T := bs.calculateTimeToExpiry(derivative.DerivativeInfo.ExpiryDate)

	// Check for invalid inputs
	if S <= 0 || K <= 0 || T <= 0 || sigma <= 0 {
		bs.log.Errorf("Invalid inputs for Greeks calculation: S=%f, K=%f, T=%f, sigma=%f", S, K, T, sigma)
		return &models.DerivativeGreeks{}
	}

	// Calculate d1 and d2
	d1 := (math.Log(S/K) + (r-q+0.5*sigma*sigma)*T) / (sigma * math.Sqrt(T))
	d2 := d1 - sigma*math.Sqrt(T)

	// Initialize Greeks
	greeks := &models.DerivativeGreeks{}

	// Calculate Delta
	if derivative.DerivativeInfo.OptionType == models.OptionTypeCall {
		greeks.Delta = math.Exp(-q*T) * normalCDF(d1)
	} else {
		greeks.Delta = math.Exp(-q*T) * (normalCDF(d1) - 1)
	}

	// Calculate Gamma (same for call and put)
	greeks.Gamma = math.Exp(-q*T) * normalPDF(d1) / (S * sigma * math.Sqrt(T))

	// Calculate Theta
	term1 := -S * sigma * math.Exp(-q*T) * normalPDF(d1) / (2 * math.Sqrt(T))
	if derivative.DerivativeInfo.OptionType == models.OptionTypeCall {
		term2 := -r * K * math.Exp(-r*T) * normalCDF(d2)
		term3 := q * S * math.Exp(-q*T) * normalCDF(d1)
		greeks.Theta = (term1 + term2 + term3) / 365 // Convert to daily theta
	} else {
		term2 := r * K * math.Exp(-r*T) * normalCDF(-d2)
		term3 := -q * S * math.Exp(-q*T) * normalCDF(-d1)
		greeks.Theta = (term1 + term2 + term3) / 365 // Convert to daily theta
	}

	// Calculate Vega (same for call and put)
	greeks.Vega = S * math.Exp(-q*T) * normalPDF(d1) * math.Sqrt(T) / 100 // Vega per 1% change in vol

	// Calculate Rho
	if derivative.DerivativeInfo.OptionType == models.OptionTypeCall {
		greeks.Rho = K * T * math.Exp(-r*T) * normalCDF(d2) / 100 // Rho per 1% change in rate
	} else {
		greeks.Rho = -K * T * math.Exp(-r*T) * normalCDF(-d2) / 100 // Rho per 1% change in rate
	}

	return greeks
}

// ImpliedVolatility calculates the implied volatility for an option given its market price
func (bs *BlackScholesPricer) ImpliedVolatility(derivative *models.MarketData, underlying *models.MarketData, marketPrice float64) float64 {
	if derivative == nil || underlying == nil || derivative.DerivativeInfo == nil {
		bs.log.Error("Cannot calculate implied volatility: invalid inputs")
		return 0
	}

	// Check if market price exists
	if marketPrice <= 0 {
		bs.log.Error("Cannot calculate implied volatility: invalid market price")
		return 0
	}

	// Extract option parameters
	S := underlying.Price
	K := derivative.DerivativeInfo.StrikePrice
	r := bs.riskFreeRate
	q := bs.dividendYield
	T := bs.calculateTimeToExpiry(derivative.DerivativeInfo.ExpiryDate)
	isCall := derivative.DerivativeInfo.OptionType == models.OptionTypeCall

	// Newton-Raphson method to find implied volatility
	sigma := 0.2 // Initial guess
	precision := 0.0001
	maxIterations := 100

	for i := 0; i < maxIterations; i++ {
		var price float64
		if isCall {
			price = bs.calculateCallPrice(S, K, r, q, T, sigma)
		} else {
			price = bs.calculatePutPrice(S, K, r, q, T, sigma)
		}

		// Calculate difference
		diff := price - marketPrice
		if math.Abs(diff) < precision {
			return sigma
		}

		// Calculate vega
		d1 := (math.Log(S/K) + (r-q+0.5*sigma*sigma)*T) / (sigma * math.Sqrt(T))
		vega := S * math.Exp(-q*T) * normalPDF(d1) * math.Sqrt(T)

		// Update sigma using Newton-Raphson method
		sigma = sigma - diff/vega

		// Check bounds
		if sigma <= 0.001 {
			sigma = 0.001
		} else if sigma > 5 {
			return 0 // Unrealistic implied volatility
		}
	}

	bs.log.Warn("Implied volatility calculation did not converge")
	return sigma
}

// calculateTimeToExpiry calculates time to expiry in years
func (bs *BlackScholesPricer) calculateTimeToExpiry(expiryDate time.Time) float64 {
	timeToExpiry := expiryDate.Sub(time.Now())

	// Convert to years (assuming 365 days in a year)
	T := timeToExpiry.Hours() / (24 * 365)

	// Ensure T is positive
	if T <= 0 {
		bs.log.Warn("Option has expired or expires today, using small positive value for T")
		return 0.001 // Small positive value
	}

	return T
}

// normalCDF returns the cumulative distribution function of the standard normal distribution
func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt(2)))
}

// normalPDF returns the probability density function of the standard normal distribution
func normalPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}
