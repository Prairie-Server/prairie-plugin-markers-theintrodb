package provider

import (
	"sync"
	"time"
)

type ttlCacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

type ttlCache[T any] struct {
	mu      sync.RWMutex
	entries map[string]ttlCacheEntry[T]
}

func newTTLCache[T any]() *ttlCache[T] {
	return &ttlCache[T]{entries: map[string]ttlCacheEntry[T]{}}
}

func (c *ttlCache[T]) Get(key string) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	now := time.Now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return zero, false
	}
	if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) Set(key string, value T, ttl time.Duration) {
	if c == nil {
		return
	}
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	c.mu.Lock()
	c.entries[key] = ttlCacheEntry[T]{value: value, expiresAt: expiresAt}
	c.mu.Unlock()
}

func (c *ttlCache[T]) Close() {}
