// Package kit
// Reference: https://github.com/puzpuzpuz/xsync/blob/main/counter.go
package kit

import (
	"sync"
	"sync/atomic"
)

// pool for P tokens
var ptokenPool sync.Pool

// a P token is used to point at the current OS thread (P)
// on which the goroutine is run; exact identity of the thread,
// as well as P migration tolerance, is not important since
// it's used to as a best effort mechanism for assigning
// concurrent operations (goroutines) to different stripes of
// the counter
type ptoken struct {
	idx uint32
	// Padding to prevent false sharing.
	_ [cacheLineSize - 4]byte
}

// Counter is a striped int64 counter that supports negative values.
//
// Should be preferred over a single atomically updated int64
// counter in high contention scenarios.
//
// A Counter must not be copied after first use.
type Counter struct {
	stripes []cstripe
	mask    uint32
}

// UCounter is a striped uint64 counter that only supports non-negative values.
//
// Should be preferred over a single atomically updated uint64
// counter in high contention scenarios.
//
// A UCounter must not be copied after first use.
type UCounter struct {
	stripes []uStripe
	mask    uint32
}

type cstripe struct {
	c atomic.Int64
	// Padding to prevent false sharing.
	_ [cacheLineSize - 8]byte
}

type uStripe struct {
	c atomic.Uint64
	// Padding to prevent false sharing.
	_ [cacheLineSize - 8]byte
}

// NewCounter creates a new Counter instance.
func NewCounter() *Counter {
	nstripes := NextPowOf2(Parallelism())
	c := Counter{
		stripes: make([]cstripe, nstripes),
		mask:    nstripes - 1,
	}
	return &c
}

// NewUCounter creates a new UCounter instance.
func NewUCounter() *UCounter {
	nstripes := NextPowOf2(Parallelism())
	c := UCounter{
		stripes: make([]uStripe, nstripes),
		mask:    nstripes - 1,
	}
	return &c
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.Add(1)
}

// Dec decrements the counter by 1.
func (c *Counter) Dec() {
	c.Add(-1)
}

// Add adds the delta to the counter.
func (c *Counter) Add(delta int64) {
	if delta == 0 {
		return
	}

	t, ok := ptokenPool.Get().(*ptoken)
	if !ok {
		t = new(ptoken)
		t.idx = runtime_cheaprand()
	}
	for {
		stripe := &c.stripes[t.idx&c.mask]
		cnt := stripe.c.Load()
		if stripe.c.CompareAndSwap(cnt, cnt+delta) {
			break
		}
		// Give a try with another randomly selected stripe.
		t.idx = runtime_cheaprand()
	}
	ptokenPool.Put(t)
}

// Load returns the current counter value.
// It is equivalent to Value(), added for API consistency with atomic types.
func (c *Counter) Load() int64 {
	return c.Value()
}

// Store sets the counter value to the given newValue.
// This operation resets all stripes and stores the entire value in the first stripe.
// Note: This operation is not atomic and should be used with care in concurrent environments.
func (c *Counter) Store(val int64) {
	for i := 0; i < len(c.stripes); i++ {
		stripe := &c.stripes[i]
		if i == 0 {
			stripe.c.Store(val)
		} else {
			stripe.c.Store(0)
		}
	}
}

// Value returns the current counter value.
// The returned value may not include all the latest operations in
// presence of concurrent modifications of the counter.
func (c *Counter) Value() int64 {
	v := int64(0)
	for i := 0; i < len(c.stripes); i++ {
		stripe := &c.stripes[i]
		v += stripe.c.Load()
	}
	return v
}

// Reset resets the counter to zero.
// This method should only be used when it is known that there are
// no concurrent modifications of the counter.
func (c *Counter) Reset() {
	for i := 0; i < len(c.stripes); i++ {
		stripe := &c.stripes[i]
		stripe.c.Store(0)
	}
}

// Inc increments the counter by 1.
func (c *UCounter) Inc() {
	c.Add(1)
}

// Add adds the delta to the counter.
// It panics if delta is negative.
func (c *UCounter) Add(delta uint64) {
	if delta == 0 {
		return
	}

	t, ok := ptokenPool.Get().(*ptoken)
	if !ok {
		t = new(ptoken)
		t.idx = runtime_cheaprand()
	}
	for {
		stripe := &c.stripes[t.idx&c.mask]
		cnt := stripe.c.Load()
		if stripe.c.CompareAndSwap(cnt, cnt+delta) {
			break
		}
		// Give a try with another randomly selected stripe.
		t.idx = runtime_cheaprand()
	}
	ptokenPool.Put(t)
}

// Load returns the current counter value.
// It is equivalent to Value(), added for API consistency with atomic types.
func (c *UCounter) Load() uint64 {
	return c.Value()
}

// Store sets the counter value to the given newValue.
// This operation resets all stripes and stores the entire value in the first stripe.
// Note: This operation is not atomic and should be used with care in concurrent environments.
func (c *UCounter) Store(val uint64) {
	for i := 0; i < len(c.stripes); i++ {
		stripe := &c.stripes[i]
		if i == 0 {
			stripe.c.Store(val)
		} else {
			stripe.c.Store(0)
		}
	}
}

// Value returns the current counter value.
// The returned value may not include all the latest operations in
// presence of concurrent modifications of the counter.
func (c *UCounter) Value() uint64 {
	v := uint64(0)
	for i := 0; i < len(c.stripes); i++ {
		stripe := &c.stripes[i]
		v += stripe.c.Load()
	}
	return v
}

// Reset resets the counter to zero.
// This method should only be used when it is known that there are
// no concurrent modifications of the counter.
func (c *UCounter) Reset() {
	for i := 0; i < len(c.stripes); i++ {
		stripe := &c.stripes[i]
		stripe.c.Store(0)
	}
}
