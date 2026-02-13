package adapters

import "sync"

type SimulatedClock struct {
	mu   sync.Mutex
	now  int64
	step int64
}

func NewSimulatedClock(start int64, step int64) *SimulatedClock {
	return &SimulatedClock{now: start, step: step}
}

func (c *SimulatedClock) UnixNano() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += c.step
	return c.now
}
