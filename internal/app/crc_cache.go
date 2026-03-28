package app

import (
	"context"
	"sync"

	"golang.org/x/sync/singleflight"
)

type crcCache struct {
	mu     sync.RWMutex
	values map[string]uint32
	group  singleflight.Group
}

func newCRCCache() *crcCache {
	return &crcCache{
		values: make(map[string]uint32, 1024),
	}
}

func (c *crcCache) Get(key string) (uint32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value, ok := c.values[key]
	return value, ok
}

func (c *crcCache) Set(key string, value uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.values[key] = value
}

func (c *crcCache) Resolve(ctx context.Context, key string, build func(context.Context) (uint32, error)) (uint32, error) {
	if value, ok := c.Get(key); ok {
		return value, nil
	}

	result, err, _ := c.group.Do(key, func() (any, error) {
		if value, ok := c.Get(key); ok {
			return value, nil
		}
		value, err := build(ctx)
		if err != nil {
			return uint32(0), err
		}
		c.Set(key, value)
		return value, nil
	})
	if err != nil {
		return 0, err
	}
	return result.(uint32), nil
}
