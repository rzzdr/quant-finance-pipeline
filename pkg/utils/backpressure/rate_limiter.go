package backpressure

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

type RateLimiter interface {
	Allow() bool
	AllowN(n int) bool
	Wait(ctx context.Context) error
	WaitN(ctx context.Context, n int) error
	Limit() float64
	Burst() int
	TokensRemaining() int
}

type TokenBucketLimiter struct {
	rate       float64
	burst      int
	tokens     int64
	lastUpdate int64
	mutex      sync.RWMutex
	log        *logger.Logger
}

func NewTokenBucketLimiter(rate float64, burst int) *TokenBucketLimiter {
	if rate <= 0 {
		rate = 1.0
	}
	if burst <= 0 {
		burst = 1
	}

	limiter := &TokenBucketLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     int64(burst),
		lastUpdate: time.Now().UnixNano(),
		log:        logger.GetLogger("rate_limiter.token_bucket"),
	}

	limiter.log.Infof("Token bucket rate limiter created with rate=%.2f, burst=%d",
		rate, burst)

	return limiter
}

// Allow checks if a single operation is allowed
func (tb *TokenBucketLimiter) Allow() bool {
	return tb.AllowN(1)
}

// AllowN checks if n operations are allowed
func (tb *TokenBucketLimiter) AllowN(n int) bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	now := time.Now().UnixNano()
	tb.refillTokens(now)

	if atomic.LoadInt64(&tb.tokens) >= int64(n) {
		atomic.AddInt64(&tb.tokens, -int64(n))
		return true
	}

	return false
}

// Wait waits until a single operation is allowed
func (tb *TokenBucketLimiter) Wait(ctx context.Context) error {
	return tb.WaitN(ctx, 1)
}

// WaitN waits until n operations are allowed
func (tb *TokenBucketLimiter) WaitN(ctx context.Context, n int) error {
	if n > tb.burst {
		return ErrRequestTooLarge
	}

	for {
		if tb.AllowN(n) {
			return nil
		}

		// Calculate wait time
		waitTime := tb.calculateWaitTime(n)

		select {
		case <-time.After(waitTime):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// refillTokens adds tokens based on elapsed time
func (tb *TokenBucketLimiter) refillTokens(now int64) {
	last := atomic.LoadInt64(&tb.lastUpdate)
	elapsed := time.Duration(now - last)

	if elapsed <= 0 {
		return
	}

	tokensToAdd := int64(elapsed.Seconds() * tb.rate)
	if tokensToAdd > 0 {
		newTokens := atomic.LoadInt64(&tb.tokens) + tokensToAdd
		if newTokens > int64(tb.burst) {
			newTokens = int64(tb.burst)
		}
		atomic.StoreInt64(&tb.tokens, newTokens)
		atomic.StoreInt64(&tb.lastUpdate, now)
	}
}

// calculateWaitTime calculates how long to wait for n tokens
func (tb *TokenBucketLimiter) calculateWaitTime(n int) time.Duration {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()

	tokensNeeded := int64(n) - atomic.LoadInt64(&tb.tokens)
	if tokensNeeded <= 0 {
		return 0
	}

	waitTime := time.Duration(float64(tokensNeeded)/tb.rate) * time.Second
	if waitTime < time.Millisecond {
		waitTime = time.Millisecond
	}

	return waitTime
}

// Limit returns the current rate limit
func (tb *TokenBucketLimiter) Limit() float64 {
	return tb.rate
}

// Burst returns the burst capacity
func (tb *TokenBucketLimiter) Burst() int {
	return tb.burst
}

// TokensRemaining returns the number of tokens remaining
func (tb *TokenBucketLimiter) TokensRemaining() int {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()

	now := time.Now().UnixNano()
	tb.refillTokens(now)
	return int(atomic.LoadInt64(&tb.tokens))
}

// SlidingWindowLimiter implements rate limiting using sliding window algorithm
type SlidingWindowLimiter struct {
	limit    int
	window   time.Duration
	requests []int64
	mutex    sync.RWMutex
	log      *logger.Logger
}

// NewSlidingWindowLimiter creates a new sliding window rate limiter
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 100
	}
	if window <= 0 {
		window = time.Minute
	}

	limiter := &SlidingWindowLimiter{
		limit:    limit,
		window:   window,
		requests: make([]int64, 0, limit*2),
		log:      logger.GetLogger("rate_limiter.sliding_window"),
	}

	limiter.log.Infof("Sliding window rate limiter created with limit=%d, window=%v",
		limit, window)

	return limiter
}

// Allow checks if a request is allowed
func (sw *SlidingWindowLimiter) Allow() bool {
	return sw.AllowN(1)
}

// AllowN checks if n requests are allowed
func (sw *SlidingWindowLimiter) AllowN(n int) bool {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	now := time.Now().UnixNano()
	sw.cleanOldRequests(now)

	if len(sw.requests)+n <= sw.limit {
		for i := 0; i < n; i++ {
			sw.requests = append(sw.requests, now)
		}
		return true
	}

	return false
}

// Wait waits until a request is allowed
func (sw *SlidingWindowLimiter) Wait(ctx context.Context) error {
	return sw.WaitN(ctx, 1)
}

// WaitN waits until n requests are allowed
func (sw *SlidingWindowLimiter) WaitN(ctx context.Context, n int) error {
	if n > sw.limit {
		return ErrRequestTooLarge
	}

	for {
		if sw.AllowN(n) {
			return nil
		}

		waitTime := sw.calculateWaitTime()

		select {
		case <-time.After(waitTime):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// cleanOldRequests removes requests outside the window
func (sw *SlidingWindowLimiter) cleanOldRequests(now int64) {
	windowStart := now - sw.window.Nanoseconds()

	// Find first request within window
	start := 0
	for i, req := range sw.requests {
		if req >= windowStart {
			start = i
			break
		}
		start = i + 1
	}

	// Remove old requests
	if start > 0 {
		sw.requests = sw.requests[start:]
	}
}

// calculateWaitTime calculates wait time until next slot is available
func (sw *SlidingWindowLimiter) calculateWaitTime() time.Duration {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	if len(sw.requests) == 0 {
		return 0
	}

	oldest := sw.requests[0]
	waitTime := time.Duration(oldest+sw.window.Nanoseconds()-time.Now().UnixNano()) * time.Nanosecond

	if waitTime < 0 {
		waitTime = time.Millisecond
	}

	return waitTime
}

// Limit returns the current rate limit
func (sw *SlidingWindowLimiter) Limit() float64 {
	return float64(sw.limit) / sw.window.Seconds()
}

// Burst returns the burst capacity
func (sw *SlidingWindowLimiter) Burst() int {
	return sw.limit
}

// TokensRemaining returns remaining capacity
func (sw *SlidingWindowLimiter) TokensRemaining() int {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	now := time.Now().UnixNano()
	sw.cleanOldRequests(now)
	return sw.limit - len(sw.requests)
}

// AdaptiveRateLimiter automatically adjusts rate based on system load
type AdaptiveRateLimiter struct {
	baseLimiter    RateLimiter
	minRate        float64
	maxRate        float64
	currentRate    float64
	targetLoad     float64
	adjustmentRate float64
	lastAdjustment time.Time
	mutex          sync.RWMutex
	log            *logger.Logger
}

// NewAdaptiveRateLimiter creates a new adaptive rate limiter
func NewAdaptiveRateLimiter(baseRate, minRate, maxRate, targetLoad float64) *AdaptiveRateLimiter {
	if minRate <= 0 {
		minRate = 1.0
	}
	if maxRate <= minRate {
		maxRate = minRate * 10
	}
	if baseRate < minRate || baseRate > maxRate {
		baseRate = (minRate + maxRate) / 2
	}
	if targetLoad <= 0 || targetLoad > 1 {
		targetLoad = 0.8
	}

	limiter := &AdaptiveRateLimiter{
		baseLimiter:    NewTokenBucketLimiter(baseRate, int(baseRate*2)),
		minRate:        minRate,
		maxRate:        maxRate,
		currentRate:    baseRate,
		targetLoad:     targetLoad,
		adjustmentRate: 0.1,
		lastAdjustment: time.Now(),
		log:            logger.GetLogger("rate_limiter.adaptive"),
	}

	limiter.log.Infof("Adaptive rate limiter created with base=%.2f, min=%.2f, max=%.2f, target=%.2f",
		baseRate, minRate, maxRate, targetLoad)

	return limiter
}

// Allow checks if a request is allowed
func (ar *AdaptiveRateLimiter) Allow() bool {
	return ar.baseLimiter.Allow()
}

// AllowN checks if n requests are allowed
func (ar *AdaptiveRateLimiter) AllowN(n int) bool {
	return ar.baseLimiter.AllowN(n)
}

// Wait waits until a request is allowed
func (ar *AdaptiveRateLimiter) Wait(ctx context.Context) error {
	return ar.baseLimiter.Wait(ctx)
}

// WaitN waits until n requests are allowed
func (ar *AdaptiveRateLimiter) WaitN(ctx context.Context, n int) error {
	return ar.baseLimiter.WaitN(ctx, n)
}

// AdjustRate adjusts the rate based on current system load
func (ar *AdaptiveRateLimiter) AdjustRate(currentLoad float64) {
	ar.mutex.Lock()
	defer ar.mutex.Unlock()

	if time.Since(ar.lastAdjustment) < 5*time.Second {
		return // Don't adjust too frequently
	}

	loadRatio := currentLoad / ar.targetLoad

	var newRate float64
	if loadRatio > 1.2 { // System overloaded, decrease rate
		newRate = ar.currentRate * (1 - ar.adjustmentRate)
		if newRate < ar.minRate {
			newRate = ar.minRate
		}
	} else if loadRatio < 0.8 { // System underloaded, increase rate
		newRate = ar.currentRate * (1 + ar.adjustmentRate)
		if newRate > ar.maxRate {
			newRate = ar.maxRate
		}
	} else {
		return // Load is within acceptable range
	}

	ar.currentRate = newRate
	ar.baseLimiter = NewTokenBucketLimiter(newRate, int(newRate*2))
	ar.lastAdjustment = time.Now()

	ar.log.Infof("Adjusted rate to %.2f (load: %.2f, target: %.2f)",
		newRate, currentLoad, ar.targetLoad)
}

// Limit returns the current rate limit
func (ar *AdaptiveRateLimiter) Limit() float64 {
	ar.mutex.RLock()
	defer ar.mutex.RUnlock()
	return ar.currentRate
}

// Burst returns the burst capacity
func (ar *AdaptiveRateLimiter) Burst() int {
	return ar.baseLimiter.Burst()
}

// TokensRemaining returns remaining tokens
func (ar *AdaptiveRateLimiter) TokensRemaining() int {
	return ar.baseLimiter.TokensRemaining()
}

// RateLimiterManager manages multiple rate limiters
type RateLimiterManager struct {
	limiters map[string]RateLimiter
	mutex    sync.RWMutex
	log      *logger.Logger
}

// NewRateLimiterManager creates a new rate limiter manager
func NewRateLimiterManager() *RateLimiterManager {
	return &RateLimiterManager{
		limiters: make(map[string]RateLimiter),
		log:      logger.GetLogger("rate_limiter.manager"),
	}
}

// GetTokenBucketLimiter gets or creates a token bucket rate limiter
func (rm *RateLimiterManager) GetTokenBucketLimiter(name string, rate float64, burst int) RateLimiter {
	rm.mutex.RLock()
	limiter, exists := rm.limiters[name]
	rm.mutex.RUnlock()

	if exists {
		return limiter
	}

	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Double-check locking
	if limiter, exists := rm.limiters[name]; exists {
		return limiter
	}

	limiter = NewTokenBucketLimiter(rate, burst)
	rm.limiters[name] = limiter
	rm.log.Infof("Created token bucket rate limiter '%s'", name)
	return limiter
}

// GetSlidingWindowLimiter gets or creates a sliding window rate limiter
func (rm *RateLimiterManager) GetSlidingWindowLimiter(name string, limit int, window time.Duration) RateLimiter {
	rm.mutex.RLock()
	limiter, exists := rm.limiters[name]
	rm.mutex.RUnlock()

	if exists {
		return limiter
	}

	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Double-check locking
	if limiter, exists := rm.limiters[name]; exists {
		return limiter
	}

	limiter = NewSlidingWindowLimiter(limit, window)
	rm.limiters[name] = limiter
	rm.log.Infof("Created sliding window rate limiter '%s'", name)
	return limiter
}

// GetAdaptiveLimiter gets or creates an adaptive rate limiter
func (rm *RateLimiterManager) GetAdaptiveLimiter(name string, baseRate, minRate, maxRate, targetLoad float64) *AdaptiveRateLimiter {
	rm.mutex.RLock()
	limiter, exists := rm.limiters[name]
	rm.mutex.RUnlock()

	if exists {
		if adaptive, ok := limiter.(*AdaptiveRateLimiter); ok {
			return adaptive
		}
	}

	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Double-check locking
	if limiter, exists := rm.limiters[name]; exists {
		if adaptive, ok := limiter.(*AdaptiveRateLimiter); ok {
			return adaptive
		}
	}

	adaptive := NewAdaptiveRateLimiter(baseRate, minRate, maxRate, targetLoad)
	rm.limiters[name] = adaptive
	rm.log.Infof("Created adaptive rate limiter '%s'", name)
	return adaptive
}

// Remove removes a rate limiter
func (rm *RateLimiterManager) Remove(name string) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	delete(rm.limiters, name)
	rm.log.Infof("Removed rate limiter '%s'", name)
}

// Clear removes all rate limiters
func (rm *RateLimiterManager) Clear() {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	rm.limiters = make(map[string]RateLimiter)
	rm.log.Info("Cleared all rate limiters")
}

// GetStats returns statistics for all rate limiters
func (rm *RateLimiterManager) GetStats() map[string]interface{} {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	stats := make(map[string]interface{})
	for name, limiter := range rm.limiters {
		stats[name] = map[string]interface{}{
			"limit":            limiter.Limit(),
			"burst":            limiter.Burst(),
			"tokens_remaining": limiter.TokensRemaining(),
			"type":             getTypeString(limiter),
		}
	}
	return stats
}

// getTypeString returns the type of rate limiter as string
func getTypeString(limiter RateLimiter) string {
	switch limiter.(type) {
	case *TokenBucketLimiter:
		return "token_bucket"
	case *SlidingWindowLimiter:
		return "sliding_window"
	case *AdaptiveRateLimiter:
		return "adaptive"
	default:
		return "unknown"
	}
}

// Rate limiter errors
var (
	ErrRequestTooLarge = &RateLimiterError{"request size exceeds burst capacity"}
)

// RateLimiterError represents a rate limiter error
type RateLimiterError struct {
	message string
}

func (e *RateLimiterError) Error() string {
	return e.message
}
