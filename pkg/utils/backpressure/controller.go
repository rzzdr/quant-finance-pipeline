// Package backpressure provides mechanisms to handle system overload
// and manage data flow rates in high-throughput trading systems.
package backpressure

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

type Strategy int

const (
	DropOldest Strategy = iota
	DropNewest
	Block
	Reject
)

type Controller struct {
	name            string
	strategy        Strategy
	maxQueueSize    int
	highWaterMark   int
	lowWaterMark    int
	currentLoad     int64
	droppedMessages int64
	processedCount  int64
	rejectedCount   int64
	log             *logger.Logger

	queue         chan interface{}
	blockingQueue chan interface{}

	lastProcessed time.Time

	onDrop      func(interface{})
	onReject    func(interface{})
	onOverload  func(int64)
	onUnderload func(int64)
}

type Config struct {
	Name          string
	Strategy      Strategy
	MaxQueueSize  int
	HighWaterMark int // Percentage (0-100)
	LowWaterMark  int // Percentage (0-100)
	OnDrop        func(interface{})
	OnReject      func(interface{})
	OnOverload    func(int64)
	OnUnderload   func(int64)
}

func NewController(config Config) *Controller {
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 10000
	}
	if config.HighWaterMark <= 0 {
		config.HighWaterMark = 80
	}
	if config.LowWaterMark <= 0 {
		config.LowWaterMark = 20
	}

	controller := &Controller{
		name:          config.Name,
		strategy:      config.Strategy,
		maxQueueSize:  config.MaxQueueSize,
		highWaterMark: (config.HighWaterMark * config.MaxQueueSize) / 100,
		lowWaterMark:  (config.LowWaterMark * config.MaxQueueSize) / 100,
		queue:         make(chan interface{}, config.MaxQueueSize),
		blockingQueue: make(chan interface{}),
		lastProcessed: time.Now(),
		log:           logger.GetLogger("backpressure." + config.Name),
		onDrop:        config.OnDrop,
		onReject:      config.OnReject,
		onOverload:    config.OnOverload,
		onUnderload:   config.OnUnderload,
	}

	controller.log.Infof("Backpressure controller '%s' initialized with strategy %v",
		config.Name, config.Strategy)

	return controller
}

func (c *Controller) Submit(ctx context.Context, data interface{}) error {
	currentLoad := atomic.LoadInt64(&c.currentLoad)

	if int(currentLoad) > c.highWaterMark {
		if c.onOverload != nil {
			c.onOverload(currentLoad)
		}

		switch c.strategy {
		case DropOldest:
			return c.submitDropOldest(ctx, data)
		case DropNewest:
			return c.submitDropNewest(ctx, data)
		case Block:
			return c.submitBlocking(ctx, data)
		case Reject:
			return c.submitReject(ctx, data)
		}
	}

	select {
	case c.queue <- data:
		atomic.AddInt64(&c.currentLoad, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		switch c.strategy {
		case DropOldest:
			return c.submitDropOldest(ctx, data)
		case DropNewest:
			return c.submitDropNewest(ctx, data)
		case Block:
			return c.submitBlocking(ctx, data)
		case Reject:
			return c.submitReject(ctx, data)
		}
	}

	return nil
}

func (c *Controller) Receive(ctx context.Context) (interface{}, error) {
	var data interface{}
	var ok bool

	if c.strategy == Block {
		select {
		case data, ok = <-c.blockingQueue:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		select {
		case data, ok = <-c.queue:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if !ok {
		return nil, ErrQueueClosed
	}

	currentLoad := atomic.AddInt64(&c.currentLoad, -1)
	atomic.AddInt64(&c.processedCount, 1)
	c.lastProcessed = time.Now()

	if int(currentLoad) < c.lowWaterMark && c.onUnderload != nil {
		c.onUnderload(currentLoad)
	}

	return data, nil
}

func (c *Controller) submitDropOldest(ctx context.Context, data interface{}) error {
	for {
		select {
		case c.queue <- data:
			atomic.AddInt64(&c.currentLoad, 1)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			select {
			case dropped := <-c.queue:
				atomic.AddInt64(&c.droppedMessages, 1)
				if c.onDrop != nil {
					c.onDrop(dropped)
				}
			default:
				select {
				case c.queue <- data:
					atomic.AddInt64(&c.currentLoad, 1)
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

func (c *Controller) submitDropNewest(ctx context.Context, data interface{}) error {
	select {
	case c.queue <- data:
		atomic.AddInt64(&c.currentLoad, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		atomic.AddInt64(&c.droppedMessages, 1)
		if c.onDrop != nil {
			c.onDrop(data)
		}
		return ErrMessageDropped
	}
}

func (c *Controller) submitBlocking(ctx context.Context, data interface{}) error {
	select {
	case c.blockingQueue <- data:
		atomic.AddInt64(&c.currentLoad, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Controller) submitReject(ctx context.Context, data interface{}) error {
	select {
	case c.queue <- data:
		atomic.AddInt64(&c.currentLoad, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		atomic.AddInt64(&c.rejectedCount, 1)
		if c.onReject != nil {
			c.onReject(data)
		}
		return ErrMessageRejected
	}
}

func (c *Controller) Stats() Stats {
	return Stats{
		Name:           c.name,
		Strategy:       c.strategy.String(),
		CurrentLoad:    atomic.LoadInt64(&c.currentLoad),
		MaxQueueSize:   int64(c.maxQueueSize),
		HighWaterMark:  int64(c.highWaterMark),
		LowWaterMark:   int64(c.lowWaterMark),
		ProcessedCount: atomic.LoadInt64(&c.processedCount),
		DroppedCount:   atomic.LoadInt64(&c.droppedMessages),
		RejectedCount:  atomic.LoadInt64(&c.rejectedCount),
		LastProcessed:  c.lastProcessed,
		UtilizationPct: float64(atomic.LoadInt64(&c.currentLoad)) / float64(c.maxQueueSize) * 100,
	}
}

type Stats struct {
	Name           string    `json:"name"`
	Strategy       string    `json:"strategy"`
	CurrentLoad    int64     `json:"current_load"`
	MaxQueueSize   int64     `json:"max_queue_size"`
	HighWaterMark  int64     `json:"high_water_mark"`
	LowWaterMark   int64     `json:"low_water_mark"`
	ProcessedCount int64     `json:"processed_count"`
	DroppedCount   int64     `json:"dropped_count"`
	RejectedCount  int64     `json:"rejected_count"`
	LastProcessed  time.Time `json:"last_processed"`
	UtilizationPct float64   `json:"utilization_percent"`
}

func (c *Controller) IsOverloaded() bool {
	return int(atomic.LoadInt64(&c.currentLoad)) > c.highWaterMark
}

func (c *Controller) IsUnderloaded() bool {
	return int(atomic.LoadInt64(&c.currentLoad)) < c.lowWaterMark
}

func (c *Controller) Close() {
	close(c.queue)
	if c.strategy == Block {
		close(c.blockingQueue)
	}
	c.log.Infof("Backpressure controller '%s' closed", c.name)
}

func (s Strategy) String() string {
	switch s {
	case DropOldest:
		return "DROP_OLDEST"
	case DropNewest:
		return "DROP_NEWEST"
	case Block:
		return "BLOCK"
	case Reject:
		return "REJECT"
	default:
		return "UNKNOWN"
	}
}

var (
	ErrMessageDropped  = &BackpressureError{"message dropped due to backpressure"}
	ErrMessageRejected = &BackpressureError{"message rejected due to backpressure"}
	ErrQueueClosed     = &BackpressureError{"queue is closed"}
)

type BackpressureError struct {
	message string
}

func (e *BackpressureError) Error() string {
	return e.message
}

type AdaptiveController struct {
	*Controller
	targetLatency    time.Duration
	currentLatency   time.Duration
	adjustmentFactor float64
	minInterval      time.Duration
	maxInterval      time.Duration
	currentInterval  time.Duration
	lastAdjustment   time.Time
	mutex            sync.RWMutex
}

func NewAdaptiveController(config Config, targetLatency time.Duration) *AdaptiveController {
	return &AdaptiveController{
		Controller:       NewController(config),
		targetLatency:    targetLatency,
		adjustmentFactor: 0.1,
		minInterval:      time.Millisecond,
		maxInterval:      time.Second,
		currentInterval:  100 * time.Millisecond,
		lastAdjustment:   time.Now(),
	}
}

func (ac *AdaptiveController) AdjustRate(observedLatency time.Duration) {
	ac.mutex.Lock()
	defer ac.mutex.Unlock()

	ac.currentLatency = observedLatency

	if time.Since(ac.lastAdjustment) < time.Second {
		return // Don't adjust too frequently
	}

	ratio := float64(observedLatency) / float64(ac.targetLatency)

	if ratio > 1.2 { // Too slow, increase interval (reduce rate)
		newInterval := time.Duration(float64(ac.currentInterval) * (1 + ac.adjustmentFactor))
		if newInterval <= ac.maxInterval {
			ac.currentInterval = newInterval
		}
	} else if ratio < 0.8 { // Too fast, decrease interval (increase rate)
		newInterval := time.Duration(float64(ac.currentInterval) * (1 - ac.adjustmentFactor))
		if newInterval >= ac.minInterval {
			ac.currentInterval = newInterval
		}
	}

	ac.lastAdjustment = time.Now()
	ac.log.Debugf("Adjusted processing interval to %v (latency: %v, target: %v)",
		ac.currentInterval, observedLatency, ac.targetLatency)
}

func (ac *AdaptiveController) GetCurrentInterval() time.Duration {
	ac.mutex.RLock()
	defer ac.mutex.RUnlock()
	return ac.currentInterval
}

type Manager struct {
	controllers map[string]*Controller
	mutex       sync.RWMutex
	log         *logger.Logger
}

func NewManager() *Manager {
	return &Manager{
		controllers: make(map[string]*Controller),
		log:         logger.GetLogger("backpressure.manager"),
	}
}

func (m *Manager) GetController(name string, config Config) *Controller {
	m.mutex.RLock()
	controller, exists := m.controllers[name]
	m.mutex.RUnlock()

	if exists {
		return controller
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if controller, exists := m.controllers[name]; exists {
		return controller
	}

	config.Name = name
	controller = NewController(config)
	m.controllers[name] = controller
	m.log.Infof("Created backpressure controller '%s'", name)
	return controller
}

func (m *Manager) GetAllStats() map[string]Stats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	stats := make(map[string]Stats)
	for name, controller := range m.controllers {
		stats[name] = controller.Stats()
	}
	return stats
}

func (m *Manager) CloseAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for name, controller := range m.controllers {
		controller.Close()
		m.log.Infof("Closed backpressure controller '%s'", name)
	}
	m.controllers = make(map[string]*Controller)
}
