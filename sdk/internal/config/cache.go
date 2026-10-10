// Package config contains immutable configuration snapshot cache primitives.
package config

import (
	"errors"
	"sync"
	"time"
)

// Cache stores successful snapshots for a fixed TTL. Values are cloned both
// when inserted and returned so callers cannot mutate cached state.
type Cache[T any] struct {
	mu      sync.Mutex
	entries map[string]entry[T]
	ttl     time.Duration
	clone   func(T) T
	now     func() time.Time
}

type entry[T any] struct {
	value   T
	expires time.Time
}

var (
	errNilCache = errors.New("luban: configuration cache is nil")
	errNilLoad  = errors.New("luban: configuration cache loader is nil")
)

// NewCache creates a cache with the supplied TTL and clone function. A nil
// clock uses time.Now; a nil clone function treats values as immutable.
func NewCache[T any](ttl time.Duration, clone func(T) T, clock func() time.Time) *Cache[T] {
	if clock == nil {
		clock = time.Now
	}
	if clone == nil {
		clone = func(value T) T { return value }
	}
	return &Cache[T]{
		entries: make(map[string]entry[T]),
		ttl:     ttl,
		clone:   clone,
		now:     clock,
	}
}

// Get returns an unexpired cached value or loads and caches a successful
// snapshot. Concurrent cache misses may call load more than once.
func (c *Cache[T]) Get(key string, load func() (T, error)) (T, error) {
	var zero T
	if c == nil {
		return zero, errNilCache
	}
	if load == nil {
		return zero, errNilLoad
	}

	c.mu.Lock()
	now := c.now()
	cached, ok := c.entries[key]
	if ok && now.Before(cached.expires) {
		c.mu.Unlock()
		return c.clone(cached.value), nil
	}
	if ok {
		delete(c.entries, key)
	}
	c.mu.Unlock()

	value, err := load()
	if err != nil {
		return zero, err
	}

	now = c.now()
	c.mu.Lock()
	if existing, exists := c.entries[key]; exists && now.Before(existing.expires) {
		value = existing.value
	} else {
		c.entries[key] = entry[T]{
			value:   c.clone(value),
			expires: now.Add(c.ttl),
		}
	}
	c.mu.Unlock()
	return c.clone(value), nil
}
