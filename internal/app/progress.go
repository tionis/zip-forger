package app

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type ProgressManager struct {
	mu        sync.Mutex
	listeners map[string][]chan progressUpdate // key: owner/repo/commit
	lastCount map[string]int64
}

type progressUpdate struct {
	Count int64
	Phase string
}

func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		listeners: make(map[string][]chan progressUpdate),
		lastCount: make(map[string]int64),
	}
}

func (m *ProgressManager) key(owner, repo, commit string) string {
	return fmt.Sprintf("%s/%s/%s", owner, repo, commit)
}

func (m *ProgressManager) Notify(owner, repo, commit string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(owner, repo, commit)
	m.lastCount[k] = count

	channels := m.listeners[k]
	for _, ch := range channels {
		select {
		case ch <- progressUpdate{Count: count, Phase: "indexing"}:
		default:
			// Buffer full, skip this update
		}
	}
}

func (m *ProgressManager) Finalize(owner, repo, commit string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(owner, repo, commit)
	m.lastCount[k] = count

	channels := m.listeners[k]
	for _, ch := range channels {
		select {
		case ch <- progressUpdate{Count: count, Phase: "finalizing"}:
		default:
			// Buffer full, skip this update
		}
	}
}

func (m *ProgressManager) HandleSSE(w http.ResponseWriter, r *http.Request, owner, repo string, refs ...string) {
	keys := m.keys(owner, repo, refs...)
	if owner == "" || repo == "" || len(keys) == 0 {
		http.Error(w, "missing parameters", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan progressUpdate, 10)

	m.mu.Lock()
	initialCount := int64(0)
	for _, key := range keys {
		if count := m.lastCount[key]; count > initialCount {
			initialCount = count
		}
		m.listeners[key] = append(m.listeners[key], ch)
	}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, key := range keys {
			channels := m.listeners[key]
			for i, c := range channels {
				if c == ch {
					m.listeners[key] = append(channels[:i], channels[i+1:]...)
					break
				}
			}
			if len(m.listeners[key]) == 0 {
				delete(m.listeners, key)
			}
		}
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial/cached count immediately
	fmt.Fprintf(w, "data: {\"count\": %d, \"phase\": %q}\n\n", initialCount, "indexing")
	flusher.Flush()

	for {
		select {
		case update := <-ch:
			phase := update.Phase
			if phase == "" {
				phase = "indexing"
			}
			fmt.Fprintf(w, "data: {\"count\": %d, \"phase\": %q}\n\n", update.Count, phase)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (m *ProgressManager) keys(owner, repo string, refs ...string) []string {
	seen := make(map[string]struct{}, len(refs))
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		key := m.key(owner, repo, ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}
