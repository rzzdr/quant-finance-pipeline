package pools

import (
	"sync"
	"time"
)

// ByteSlicePool is a pool of byte slices
type ByteSlicePool struct {
	pool sync.Pool
	size int
}

// NewByteSlicePool creates a new ByteSlicePool
func NewByteSlicePool(size int) *ByteSlicePool {
	return &ByteSlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, size)
			},
		},
		size: size,
	}
}

// Get retrieves a byte slice from the pool
func (p *ByteSlicePool) Get() []byte {
	return p.pool.Get().([]byte)[:0]
}

// Put returns a byte slice to the pool
func (p *ByteSlicePool) Put(b []byte) {
	if cap(b) >= p.size {
		p.pool.Put(b[:0])
	}
	// If capacity is less than expected, let GC handle it
}

// TimeSlicePool is a pool of time.Time slices
type TimeSlicePool struct {
	pool sync.Pool
	size int
}

// NewTimeSlicePool creates a new TimeSlicePool
func NewTimeSlicePool(size int) *TimeSlicePool {
	return &TimeSlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]time.Time, 0, size)
			},
		},
		size: size,
	}
}

// Get retrieves a time.Time slice from the pool
func (p *TimeSlicePool) Get() []time.Time {
	return p.pool.Get().([]time.Time)[:0]
}

// Put returns a time.Time slice to the pool
func (p *TimeSlicePool) Put(t []time.Time) {
	if cap(t) >= p.size {
		p.pool.Put(t[:0])
	}
}

// Float64SlicePool is a pool of float64 slices
type Float64SlicePool struct {
	pool sync.Pool
	size int
}

// NewFloat64SlicePool creates a new Float64SlicePool
func NewFloat64SlicePool(size int) *Float64SlicePool {
	return &Float64SlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]float64, 0, size)
			},
		},
		size: size,
	}
}

// Get retrieves a float64 slice from the pool
func (p *Float64SlicePool) Get() []float64 {
	return p.pool.Get().([]float64)[:0]
}

// Put returns a float64 slice to the pool
func (p *Float64SlicePool) Put(f []float64) {
	if cap(f) >= p.size {
		p.pool.Put(f[:0])
	}
}

// ObjectPool is a generic pool for any object
type ObjectPool struct {
	pool sync.Pool
}

// NewObjectPool creates a new ObjectPool with the given factory function
func NewObjectPool(factory func() interface{}) *ObjectPool {
	return &ObjectPool{
		pool: sync.Pool{
			New: factory,
		},
	}
}

// Get retrieves an object from the pool
func (p *ObjectPool) Get() interface{} {
	return p.pool.Get()
}

// Put returns an object to the pool
func (p *ObjectPool) Put(obj interface{}) {
	p.pool.Put(obj)
}
