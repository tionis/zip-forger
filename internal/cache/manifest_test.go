package cache

import (
	"testing"
	"time"

	"zip-forger/internal/source"
)

func TestGetSetBasic(t *testing.T) {
	c := NewManifestCache(time.Minute, 10)

	_, ok := c.Get("missing")
	if ok {
		t.Fatal("expected cache miss for unknown key")
	}

	m := Manifest{
		Entries:    []source.Entry{{Path: "a.txt", Size: 10}},
		TotalBytes: 10,
	}
	c.Set("key1", m)

	got, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if len(got.Entries) != 1 || got.Entries[0].Path != "a.txt" {
		t.Fatalf("unexpected cached manifest: %#v", got)
	}
}

func TestGetExpired(t *testing.T) {
	c := NewManifestCache(1*time.Millisecond, 10)

	c.Set("key1", Manifest{TotalBytes: 1})
	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected cache miss for expired entry")
	}
}

func TestEvictionWhenAtCapacity(t *testing.T) {
	c := NewManifestCache(time.Minute, 2)

	c.Set("a", Manifest{TotalBytes: 1})
	c.Set("b", Manifest{TotalBytes: 2})
	c.Set("c", Manifest{TotalBytes: 3})

	// One of {a, b} should have been evicted, c should exist
	_, okC := c.Get("c")
	if !okC {
		t.Fatal("expected 'c' to be present after eviction")
	}

	// At most 2 entries should remain
	count := 0
	for _, key := range []string{"a", "b", "c"} {
		if _, ok := c.Get(key); ok {
			count++
		}
	}
	if count > 2 {
		t.Fatalf("expected at most 2 entries, got %d", count)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewManifestCache(time.Minute, 100)
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			key := string(rune('a' + id))
			c.Set(key, Manifest{TotalBytes: int64(id)})
			c.Get(key)
			c.Get("nonexistent")
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
