package models

import (
	"time"
)

// The severity of a risk alert
type AlertSeverity int

const (
	AlertSeverityInfo AlertSeverity = iota
	AlertSeverityWarning
	AlertSeverityCritical
)

// Position represents a single position within a portfolio
type Position struct {
	Symbol         string
	Quantity       float64
	IsDerivative   bool
	DerivativeInfo *RiskDerivativeInfo
}

// Portfolio represents a collection of positions
type Portfolio struct {
	ID        string
	Name      string
	Positions []Position
	Owner     string
	Created   time.Time
	Updated   time.Time
}

// RiskDerivativeInfo contains information specific to derivative instruments
type RiskDerivativeInfo struct {
	UnderlyingSymbol  string
	ExpiryDate        time.Time
	StrikePrice       float64
	OptionType        string // "call" or "put"
	ImpliedVolatility float64
	Delta             float64
	Gamma             float64
	Theta             float64
	Vega              float64
	Rho               float64
	Greeks            *DerivativeGreeks
}

// RiskMarketData contains price and related information for a financial instrument used in risk calculations
type RiskMarketData struct {
	Symbol           string
	Price            float64
	BidPrice         float64
	AskPrice         float64
	Volume           int64
	Timestamp        time.Time
	UnderlyingSymbol string
	DerivativeInfo   *RiskDerivativeInfo
}

// The Greeks for a derivative position
type DerivativeGreeks struct {
	Delta float64
	Gamma float64
	Theta float64
	Vega  float64
	Rho   float64
}

// Risk metrics for a single position
type PositionRisk struct {
	Symbol             string
	PositionSize       float64
	PositionValue      float64
	ValueAtRisk        float64
	ExpectedShortfall  float64
	ContributionToRisk float64
	Greeks             *DerivativeGreeks
}

// A single stress scenario
type StressScenario struct {
	ScenarioName         string
	PortfolioValueChange float64
	PercentageChange     float64
	PositionValueChanges map[string]float64
}

// The results of a stress test
type StressTestResults struct {
	Scenarios []StressScenario
}

// A single factor in a sensitivity analysis
type SensitivityFactor struct {
	FactorName            string
	PortfolioSensitivity  float64
	PositionSensitivities map[string]float64
}

// A sensitivity analysis for a portfolio
type SensitivityAnalysis struct {
	Factors []SensitivityFactor
}

// A set of risk metrics for a portfolio
type RiskMetrics struct {
	PortfolioID         string
	Timestamp           time.Time
	ValueAtRisk         float64
	ExpectedShortfall   float64
	PortfolioValue      float64
	PositionRisks       map[string]PositionRisk
	StressTestResults   StressTestResults
	SensitivityAnalysis SensitivityAnalysis
}

// A recommended hedging action
type HedgingRecommendation struct {
	Symbol        string
	Quantity      float64
	EstimatedCost float64
	Description   string
}

// A single action in a hedging strategy
type HedgingAction struct {
	Instrument     string
	Quantity       float64
	EstimatedPrice float64
	Reason         string
	RiskOffset     float64
}

// A hedging strategy for a portfolio
type HedgingStrategy struct {
	PortfolioID     string
	Timestamp       time.Time
	Actions         []HedgingAction
	EstimatedCost   float64
	RiskReduction   float64
	CurrentRisk     float64
	Recommendations []HedgingRecommendation
}

// A risk limit for a portfolio
type RiskLimit struct {
	PortfolioID    string
	LimitType      string
	LimitValue     float64
	ActionOnBreach string
}

// A risk alert
type RiskAlert struct {
	AlertID     string
	PortfolioID string
	Timestamp   time.Time
	AlertType   string
	Message     string
	Threshold   float64
	ActualValue float64
	Severity    AlertSeverity
}

// Creates a new DerivativeGreeks object
func NewDerivativeGreeks(delta, gamma, theta, vega, rho float64) *DerivativeGreeks {
	return &DerivativeGreeks{
		Delta: delta,
		Gamma: gamma,
		Theta: theta,
		Vega:  vega,
		Rho:   rho,
	}
}

// Creates a new PositionRisk object
func NewPositionRisk(symbol string, positionSize, positionValue, var_, es, contributionToRisk float64, greeks *DerivativeGreeks) PositionRisk {
	return PositionRisk{
		Symbol:             symbol,
		PositionSize:       positionSize,
		PositionValue:      positionValue,
		ValueAtRisk:        var_,
		ExpectedShortfall:  es,
		ContributionToRisk: contributionToRisk,
		Greeks:             greeks,
	}
}

// Creates a new RiskMetrics object
func NewRiskMetrics(portfolioID string, var_, es, portfolioValue float64) *RiskMetrics {
	return &RiskMetrics{
		PortfolioID:       portfolioID,
		Timestamp:         time.Now(),
		ValueAtRisk:       var_,
		ExpectedShortfall: es,
		PortfolioValue:    portfolioValue,
		PositionRisks:     make(map[string]PositionRisk),
		StressTestResults: StressTestResults{
			Scenarios: make([]StressScenario, 0),
		},
		SensitivityAnalysis: SensitivityAnalysis{
			Factors: make([]SensitivityFactor, 0),
		},
	}
}
