package circuit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateHalfOpen:
		return "HALF_OPEN"
	case StateOpen:
		return "OPEN"
	default:
		return "UNKNOWN"
	}
}

type Config struct {
	MaxFailures   int                               // Maximum failures before opening
	Timeout       time.Duration                     // Timeout for open state
	MaxRequests   int                               // Maximum requests in half-open state
	IsSuccessful  func(error) bool                  // Function to determine if error should count as failure
	OnStateChange func(name string, from, to State) // Callback for state changes
}

func DefaultConfig() Config {
	return Config{
		MaxFailures: 5,
		Timeout:     60 * time.Second,
		MaxRequests: 1,
		IsSuccessful: func(err error) bool {
			return err == nil
		},
		OnStateChange: func(name string, from, to State) {},
	}
}

type CircuitBreaker struct {
	name            string
	config          Config
	state           State
	failures        int
	requests        int
	lastFailureTime time.Time
	mutex           sync.RWMutex
	log             *logger.Logger
}

type Counts struct {
	Requests   int
	Failures   int
	Successes  int
	TimeWindow time.Duration
}

func NewCircuitBreaker(name string, config Config) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:   name,
		config: config,
		state:  StateClosed,
		log:    logger.GetLogger(fmt.Sprintf("circuit.%s", name)),
	}

	cb.log.Infof("Circuit breaker '%s' initialized in CLOSED state", name)
	return cb
}

func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	if err := cb.beforeRequest(); err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.afterRequest(false)
			panic(r)
		}
	}()

	result, err := fn()
	cb.afterRequest(cb.config.IsSuccessful(err))
	return result, err
}

func (cb *CircuitBreaker) ExecuteWithContext(ctx context.Context, fn func(ctx context.Context) (interface{}, error)) (interface{}, error) {
	if err := cb.beforeRequest(); err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.afterRequest(false)
			panic(r)
		}
	}()

	result, err := fn(ctx)
	cb.afterRequest(cb.config.IsSuccessful(err))
	return result, err
}

func (cb *CircuitBreaker) beforeRequest() error {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosed:
		return nil
	case StateOpen:
		if time.Since(cb.lastFailureTime) >= cb.config.Timeout {
			cb.toHalfOpen()
			return nil
		}
		return ErrCircuitBreakerOpen
	case StateHalfOpen:
		if cb.requests >= cb.config.MaxRequests {
			return ErrTooManyRequests
		}
		cb.requests++
		return nil
	default:
		return ErrCircuitBreakerOpen
	}
}

func (cb *CircuitBreaker) afterRequest(success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if success {
		cb.onSuccess()
	} else {
		cb.onFailure()
	}
}

func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		cb.failures = 0
	case StateHalfOpen:
		cb.toClosed()
	}
}

func (cb *CircuitBreaker) onFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.config.MaxFailures {
			cb.toOpen()
		}
	case StateHalfOpen:
		cb.toOpen()
	}
}

func (cb *CircuitBreaker) toClosed() {
	oldState := cb.state
	cb.state = StateClosed
	cb.failures = 0
	cb.requests = 0
	cb.log.Infof("Circuit breaker '%s' transitioned from %s to CLOSED", cb.name, oldState)
	cb.config.OnStateChange(cb.name, oldState, StateClosed)
}

func (cb *CircuitBreaker) toOpen() {
	oldState := cb.state
	cb.state = StateOpen
	cb.requests = 0
	cb.log.Warnf("Circuit breaker '%s' transitioned from %s to OPEN", cb.name, oldState)
	cb.config.OnStateChange(cb.name, oldState, StateOpen)
}

func (cb *CircuitBreaker) toHalfOpen() {
	oldState := cb.state
	cb.state = StateHalfOpen
	cb.requests = 0
	cb.log.Infof("Circuit breaker '%s' transitioned from %s to HALF_OPEN", cb.name, oldState)
	cb.config.OnStateChange(cb.name, oldState, StateHalfOpen)
}

func (cb *CircuitBreaker) State() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) Counts() Counts {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return Counts{
		Requests:  cb.requests,
		Failures:  cb.failures,
		Successes: cb.requests - cb.failures,
	}
}

func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	oldState := cb.state
	cb.toClosed()
	cb.log.Infof("Circuit breaker '%s' manually reset from %s", cb.name, oldState)
}

func (cb *CircuitBreaker) Name() string {
	return cb.name
}

var (
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrTooManyRequests    = errors.New("too many requests")
)

type Manager struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
	log      *logger.Logger
}

func NewManager() *Manager {
	return &Manager{
		breakers: make(map[string]*CircuitBreaker),
		log:      logger.GetLogger("circuit.manager"),
	}
}

func (m *Manager) GetBreaker(name string, config Config) *CircuitBreaker {
	m.mutex.RLock()
	breaker, exists := m.breakers[name]
	m.mutex.RUnlock()

	if exists {
		return breaker
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if breaker, exists := m.breakers[name]; exists {
		return breaker
	}

	breaker = NewCircuitBreaker(name, config)
	m.breakers[name] = breaker
	m.log.Infof("Created circuit breaker '%s'", name)
	return breaker
}

func (m *Manager) GetBreakerStats() map[string]BreakerStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	stats := make(map[string]BreakerStats)
	for name, breaker := range m.breakers {
		counts := breaker.Counts()
		stats[name] = BreakerStats{
			Name:      name,
			State:     breaker.State().String(),
			Requests:  counts.Requests,
			Failures:  counts.Failures,
			Successes: counts.Successes,
		}
	}

	return stats
}

type BreakerStats struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Requests  int    `json:"requests"`
	Failures  int    `json:"failures"`
	Successes int    `json:"successes"`
}

func (m *Manager) Reset() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for name, breaker := range m.breakers {
		breaker.Reset()
		m.log.Infof("Reset circuit breaker '%s'", name)
	}
}

func (m *Manager) ResetBreaker(name string) error {
	m.mutex.RLock()
	breaker, exists := m.breakers[name]
	m.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("circuit breaker '%s' not found", name)
	}

	breaker.Reset()
	return nil
}
