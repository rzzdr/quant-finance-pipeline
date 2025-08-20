// Package performance provides CPU profiling, memory optimization,
// and performance monitoring utilities for the trading system.
package performance

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// ProfilerConfig holds configuration for the profiler
type ProfilerConfig struct {
	EnableCPU    bool
	EnableMemory bool
	EnableMutex  bool
	EnableBlock  bool
	EnableTrace  bool
	OutputDir    string
	SampleRate   time.Duration
	AutoSave     bool
	MaxFileSize  int64 // Maximum file size in bytes
}

// Profiler manages performance profiling
type Profiler struct {
	config    ProfilerConfig
	cpuFile   *os.File
	memFile   *os.File
	running   int64
	startTime time.Time
	samples   []Sample
	mutex     sync.RWMutex
	log       *logger.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

// Sample represents a performance sample
type Sample struct {
	Timestamp      time.Time
	CPUUsage       float64
	MemoryUsage    uint64
	GoroutineCount int
	HeapSize       uint64
	GCPauseTotal   time.Duration
}

// NewProfiler creates a new performance profiler
func NewProfiler(config ProfilerConfig) *Profiler {
	if config.OutputDir == "" {
		config.OutputDir = "./profiles"
	}
	if config.SampleRate == 0 {
		config.SampleRate = time.Second
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 100 * 1024 * 1024 // 100MB
	}

	// Create output directory
	os.MkdirAll(config.OutputDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())

	profiler := &Profiler{
		config:  config,
		samples: make([]Sample, 0, 3600), // Pre-allocate for 1 hour of samples
		log:     logger.GetLogger("performance.profiler"),
		ctx:     ctx,
		cancel:  cancel,
	}

	profiler.log.Infof("Performance profiler initialized with config: %+v", config)
	return profiler
}

// Start starts the profiler
func (p *Profiler) Start() error {
	if !atomic.CompareAndSwapInt64(&p.running, 0, 1) {
		return fmt.Errorf("profiler is already running")
	}

	p.startTime = time.Now()
	timestamp := p.startTime.Format("20060102_150405")

	var err error

	// Start CPU profiling
	if p.config.EnableCPU {
		cpuFile := fmt.Sprintf("%s/cpu_%s.prof", p.config.OutputDir, timestamp)
		p.cpuFile, err = os.Create(cpuFile)
		if err != nil {
			p.Stop()
			return fmt.Errorf("failed to create CPU profile file: %w", err)
		}

		if err := pprof.StartCPUProfile(p.cpuFile); err != nil {
			p.Stop()
			return fmt.Errorf("failed to start CPU profiling: %w", err)
		}
		p.log.Infof("Started CPU profiling to %s", cpuFile)
	}

	// Start memory profiling if auto-save is enabled
	if p.config.EnableMemory && p.config.AutoSave {
		go p.periodicMemoryProfile()
	}

	// Start sampling goroutine
	go p.sampleLoop()

	p.log.Infof("Performance profiler started")
	return nil
}

// Stop stops the profiler
func (p *Profiler) Stop() error {
	if !atomic.CompareAndSwapInt64(&p.running, 1, 0) {
		return fmt.Errorf("profiler is not running")
	}

	// Cancel sampling
	p.cancel()

	// Stop CPU profiling
	if p.cpuFile != nil {
		pprof.StopCPUProfile()
		p.cpuFile.Close()
		p.cpuFile = nil
		p.log.Info("Stopped CPU profiling")
	}

	// Save final memory profile
	if p.config.EnableMemory {
		p.saveMemoryProfile("final")
	}

	// Save mutex profile
	if p.config.EnableMutex {
		p.saveMutexProfile()
	}

	// Save block profile
	if p.config.EnableBlock {
		p.saveBlockProfile()
	}

	p.log.Infof("Performance profiler stopped after %v", time.Since(p.startTime))
	return nil
}

// IsRunning returns true if the profiler is running
func (p *Profiler) IsRunning() bool {
	return atomic.LoadInt64(&p.running) == 1
}

// GetSamples returns all collected samples
func (p *Profiler) GetSamples() []Sample {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Return a copy to avoid race conditions
	samples := make([]Sample, len(p.samples))
	copy(samples, p.samples)
	return samples
}

// GetLatestSample returns the most recent sample
func (p *Profiler) GetLatestSample() *Sample {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if len(p.samples) == 0 {
		return nil
	}

	sample := p.samples[len(p.samples)-1]
	return &sample
}

// sampleLoop continuously collects performance samples
func (p *Profiler) sampleLoop() {
	ticker := time.NewTicker(p.config.SampleRate)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.collectSample()
		case <-p.ctx.Done():
			return
		}
	}
}

// collectSample collects a single performance sample
func (p *Profiler) collectSample() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	sample := Sample{
		Timestamp:      time.Now(),
		CPUUsage:       getCPUUsage(),
		MemoryUsage:    m.Alloc,
		GoroutineCount: runtime.NumGoroutine(),
		HeapSize:       m.HeapAlloc,
		GCPauseTotal:   time.Duration(m.PauseTotalNs),
	}

	p.mutex.Lock()
	p.samples = append(p.samples, sample)

	// Limit sample history to prevent memory growth
	if len(p.samples) > 10000 {
		copy(p.samples, p.samples[1000:])
		p.samples = p.samples[:9000]
	}
	p.mutex.Unlock()
}

// getCPUUsage returns current CPU usage percentage
func getCPUUsage() float64 {
	// This is a simplified implementation
	// In production, you might want to use a more sophisticated method
	runtime.Gosched()
	return float64(runtime.NumGoroutine()) / 100.0
}
	// This implementation samples process CPU time over a short interval.
	// It is more accurate than using goroutine count, but introduces a small delay.
	numCPU := float64(runtime.NumCPU())

	// Get process times at start
	startUser, startSys := getProcessTimes()
	startWall := time.Now()

	time.Sleep(100 * time.Millisecond)

	// Get process times at end
	endUser, endSys := getProcessTimes()
	endWall := time.Now()

	cpuTime := (endUser.Sub(startUser) + endSys.Sub(startSys)).Seconds()
	wallTime := endWall.Sub(startWall).Seconds()
	if wallTime == 0 {
		return 0
	}
	usage := (cpuTime / wallTime) * 100 / numCPU
	if usage > 100 {
		usage = 100
	}
	if usage < 0 {
		usage = 0
	}
	return usage
}

// getProcessTimes returns the user and system CPU times for the current process.
func getProcessTimes() (user, sys time.Duration) {
	var ru runtime.MemStats
	// Use runtime.ReadMemStats as a placeholder to avoid external dependencies.
	// For real CPU time, use syscall.Getrusage on Unix, or os.Process on Windows.
	// Here, we use time.Now() as a fallback for demonstration.
	// For production, replace with actual process CPU time retrieval.
	return time.Now(), 0
}
// periodicMemoryProfile saves memory profiles periodically
func (p *Profiler) periodicMemoryProfile() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	counter := 0
	for {
		select {
		case <-ticker.C:
			counter++
			p.saveMemoryProfile(fmt.Sprintf("periodic_%d", counter))
		case <-p.ctx.Done():
			return
		}
	}
}

// saveMemoryProfile saves a memory profile
func (p *Profiler) saveMemoryProfile(suffix string) error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s/memory_%s_%s.prof", p.config.OutputDir, timestamp, suffix)

	file, err := os.Create(filename)
	if err != nil {
		p.log.Errorf("Failed to create memory profile file: %v", err)
		return err
	}
	defer file.Close()

	runtime.GC() // Force garbage collection before profiling
	if err := pprof.WriteHeapProfile(file); err != nil {
		p.log.Errorf("Failed to write memory profile: %v", err)
		return err
	}

	p.log.Infof("Saved memory profile to %s", filename)
	return nil
}

// saveMutexProfile saves a mutex contention profile
func (p *Profiler) saveMutexProfile() error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s/mutex_%s.prof", p.config.OutputDir, timestamp)

	file, err := os.Create(filename)
	if err != nil {
		p.log.Errorf("Failed to create mutex profile file: %v", err)
		return err
	}
	defer file.Close()

	profile := pprof.Lookup("mutex")
	if profile == nil {
		p.log.Warn("Mutex profile not available")
		return nil
	}

	if err := profile.WriteTo(file, 0); err != nil {
		p.log.Errorf("Failed to write mutex profile: %v", err)
		return err
	}

	p.log.Infof("Saved mutex profile to %s", filename)
	return nil
}

// saveBlockProfile saves a block profile
func (p *Profiler) saveBlockProfile() error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s/block_%s.prof", p.config.OutputDir, timestamp)

	file, err := os.Create(filename)
	if err != nil {
		p.log.Errorf("Failed to create block profile file: %v", err)
		return err
	}
	defer file.Close()

	profile := pprof.Lookup("block")
	if profile == nil {
		p.log.Warn("Block profile not available")
		return nil
	}

	if err := profile.WriteTo(file, 0); err != nil {
		p.log.Errorf("Failed to write block profile: %v", err)
		return err
	}

	p.log.Infof("Saved block profile to %s", filename)
	return nil
}

// MemoryOptimizer provides memory optimization utilities
type MemoryOptimizer struct {
	gcThreshold     uint64
	lastGC          time.Time
	forceGCInterval time.Duration
	log             *logger.Logger
	running         int64
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewMemoryOptimizer creates a new memory optimizer
func NewMemoryOptimizer(gcThreshold uint64, forceGCInterval time.Duration) *MemoryOptimizer {
	if gcThreshold == 0 {
		gcThreshold = 100 * 1024 * 1024 // 100MB
	}
	if forceGCInterval == 0 {
		forceGCInterval = 2 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &MemoryOptimizer{
		gcThreshold:     gcThreshold,
		forceGCInterval: forceGCInterval,
		log:             logger.GetLogger("performance.memory_optimizer"),
		ctx:             ctx,
		cancel:          cancel,
	}

	optimizer.log.Infof("Memory optimizer initialized with GC threshold: %d bytes, force GC interval: %v",
		gcThreshold, forceGCInterval)

	return optimizer
}

// Start starts the memory optimizer
func (mo *MemoryOptimizer) Start() {
	if !atomic.CompareAndSwapInt64(&mo.running, 0, 1) {
		mo.log.Warn("Memory optimizer is already running")
		return
	}

	go mo.optimizeLoop()
	mo.log.Info("Memory optimizer started")
}

// Stop stops the memory optimizer
func (mo *MemoryOptimizer) Stop() {
	if !atomic.CompareAndSwapInt64(&mo.running, 1, 0) {
		mo.log.Warn("Memory optimizer is not running")
		return
	}

	mo.cancel()
	mo.log.Info("Memory optimizer stopped")
}

// optimizeLoop runs the memory optimization loop
func (mo *MemoryOptimizer) optimizeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mo.checkAndOptimize()
		case <-mo.ctx.Done():
			return
		}
	}
}

// checkAndOptimize checks memory usage and optimizes if needed
func (mo *MemoryOptimizer) checkAndOptimize() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Force GC if memory usage is above threshold
	if m.Alloc > mo.gcThreshold {
		mo.log.Infof("Memory usage (%d bytes) above threshold (%d bytes), forcing GC",
			m.Alloc, mo.gcThreshold)
		runtime.GC()
		mo.lastGC = time.Now()
		return
	}

	// Force GC if too much time has passed since last GC
	if time.Since(mo.lastGC) > mo.forceGCInterval {
		mo.log.Debug("Forcing GC due to time interval")
		runtime.GC()
		mo.lastGC = time.Now()
	}
}

// GetMemoryStats returns current memory statistics
func (mo *MemoryOptimizer) GetMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemoryStats{
		Alloc:        m.Alloc,
		TotalAlloc:   m.TotalAlloc,
		Sys:          m.Sys,
		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapIdle:     m.HeapIdle,
		HeapInuse:    m.HeapInuse,
		NumGC:        m.NumGC,
		PauseTotalNs: m.PauseTotalNs,
		NumGoroutine: runtime.NumGoroutine(),
		LastGC:       mo.lastGC,
	}
}

// MemoryStats represents memory statistics
type MemoryStats struct {
	Alloc        uint64    `json:"alloc"`
	TotalAlloc   uint64    `json:"total_alloc"`
	Sys          uint64    `json:"sys"`
	HeapAlloc    uint64    `json:"heap_alloc"`
	HeapSys      uint64    `json:"heap_sys"`
	HeapIdle     uint64    `json:"heap_idle"`
	HeapInuse    uint64    `json:"heap_inuse"`
	NumGC        uint32    `json:"num_gc"`
	PauseTotalNs uint64    `json:"pause_total_ns"`
	NumGoroutine int       `json:"num_goroutine"`
	LastGC       time.Time `json:"last_gc"`
}

// PerformanceMonitor aggregates all performance monitoring
type PerformanceMonitor struct {
	profiler  *Profiler
	optimizer *MemoryOptimizer
	metrics   *MetricsCollector
	log       *logger.Logger
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor(profilerConfig ProfilerConfig) *PerformanceMonitor {
	return &PerformanceMonitor{
		profiler:  NewProfiler(profilerConfig),
		optimizer: NewMemoryOptimizer(100*1024*1024, 2*time.Minute),
		metrics:   NewMetricsCollector(),
		log:       logger.GetLogger("performance.monitor"),
	}
}

// Start starts all performance monitoring components
func (pm *PerformanceMonitor) Start() error {
	if err := pm.profiler.Start(); err != nil {
		return fmt.Errorf("failed to start profiler: %w", err)
	}

	pm.optimizer.Start()
	pm.metrics.Start()

	pm.log.Info("Performance monitor started")
	return nil
}

// Stop stops all performance monitoring components
func (pm *PerformanceMonitor) Stop() error {
	if err := pm.profiler.Stop(); err != nil {
		pm.log.Errorf("Failed to stop profiler: %v", err)
	}

	pm.optimizer.Stop()
	pm.metrics.Stop()

	pm.log.Info("Performance monitor stopped")
	return nil
}

// GetSummary returns a performance summary
func (pm *PerformanceMonitor) GetSummary() PerformanceSummary {
	samples := pm.profiler.GetSamples()
	memStats := pm.optimizer.GetMemoryStats()
	metrics := pm.metrics.GetMetrics()

	var avgCPU, avgMemory float64
	if len(samples) > 0 {
		for _, sample := range samples {
			avgCPU += sample.CPUUsage
			avgMemory += float64(sample.MemoryUsage)
		}
		avgCPU /= float64(len(samples))
		avgMemory /= float64(len(samples))
	}

	return PerformanceSummary{
		SampleCount:    len(samples),
		AvgCPUUsage:    avgCPU,
		AvgMemoryUsage: avgMemory,
		CurrentMemory:  memStats,
		Metrics:        metrics,
		ProfilerActive: pm.profiler.IsRunning(),
	}
}

// PerformanceSummary represents a performance summary
type PerformanceSummary struct {
	SampleCount    int                    `json:"sample_count"`
	AvgCPUUsage    float64                `json:"avg_cpu_usage"`
	AvgMemoryUsage float64                `json:"avg_memory_usage"`
	CurrentMemory  MemoryStats            `json:"current_memory"`
	Metrics        map[string]interface{} `json:"metrics"`
	ProfilerActive bool                   `json:"profiler_active"`
}

// MetricsCollector collects various performance metrics
type MetricsCollector struct {
	metrics map[string]interface{}
	mutex   sync.RWMutex
	log     *logger.Logger
	running int64
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	ctx, cancel := context.WithCancel(context.Background())

	return &MetricsCollector{
		metrics: make(map[string]interface{}),
		log:     logger.GetLogger("performance.metrics"),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the metrics collector
func (mc *MetricsCollector) Start() {
	if !atomic.CompareAndSwapInt64(&mc.running, 0, 1) {
		mc.log.Warn("Metrics collector is already running")
		return
	}

	go mc.collectLoop()
	mc.log.Info("Metrics collector started")
}

// Stop stops the metrics collector
func (mc *MetricsCollector) Stop() {
	if !atomic.CompareAndSwapInt64(&mc.running, 1, 0) {
		mc.log.Warn("Metrics collector is not running")
		return
	}

	mc.cancel()
	mc.log.Info("Metrics collector stopped")
}

// collectLoop runs the metrics collection loop
func (mc *MetricsCollector) collectLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mc.collectMetrics()
		case <-mc.ctx.Done():
			return
		}
	}
}

// collectMetrics collects various system metrics
func (mc *MetricsCollector) collectMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	mc.mutex.Lock()
	mc.metrics["timestamp"] = time.Now()
	mc.metrics["goroutines"] = runtime.NumGoroutine()
	mc.metrics["memory_alloc"] = m.Alloc
	mc.metrics["memory_sys"] = m.Sys
	mc.metrics["gc_count"] = m.NumGC
	mc.metrics["gc_pause_total"] = time.Duration(m.PauseTotalNs)
	mc.mutex.Unlock()
}

// GetMetrics returns current metrics
func (mc *MetricsCollector) GetMetrics() map[string]interface{} {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	// Return a copy
	metrics := make(map[string]interface{})
	for k, v := range mc.metrics {
		metrics[k] = v
	}
	return metrics
}
