package cache

import (
	"sync"
	"time"

	"zip-forger/internal/source"
)

type Manifest struct {
	Entries    []source.Entry
	TotalBytes int64
	CreatedAt  time.Time
}

type ManifestCache struct {
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	items      map[string]item
}

type item struct {
	value     Manifest
	expiresAt time.Time
}

func NewManifestCache(ttl time.Duration, maxEntries int) *ManifestCache {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &ManifestCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		items:      make(map[string]item, maxEntries),
	}
}

func (c *ManifestCache) Get(key string) (Manifest, bool) {
	now := time.Now()

	c.mu.RLock()
	cached, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return Manifest{}, false
	}
	if now.After(cached.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return Manifest{}, false
	}
	return cached.value, true
}

func (c *ManifestCache) Set(key string, value Manifest) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.removeExpired(now)
	if len(c.items) >= c.maxEntries {
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}
	value.CreatedAt = now
	c.items[key] = item{
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
}

func (c *ManifestCache) removeExpired(now time.Time) {
	for key, cached := range c.items {
		if now.After(cached.expiresAt) {
			delete(c.items, key)
		}
	}
}
