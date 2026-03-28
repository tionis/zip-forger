package app

import (
	"fmt"
	"net/http"
	"sync"
)

type ProgressManager struct {
	mu        sync.Mutex
	listeners map[string][]chan int64 // key: owner/repo/commit
}

func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		listeners: make(map[string][]chan int64),
	}
}

func (m *ProgressManager) key(owner, repo, commit string) string {
	return fmt.Sprintf("%s/%s/%s", owner, repo, commit)
}

func (m *ProgressManager) Notify(owner, repo, commit string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(owner, repo, commit)
	channels := m.listeners[k]
	for _, ch := range channels {
		select {
		case ch <- count:
		default:
			// Buffer full, skip this update
		}
	}
}

func (m *ProgressManager) HandleSSE(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	commit := r.URL.Query().Get("commit")

	if owner == "" || repo == "" || commit == "" {
		http.Error(w, "missing parameters", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan int64, 10)
	k := m.key(owner, repo, commit)

	m.mu.Lock()
	m.listeners[k] = append(m.listeners[k], ch)
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		channels := m.listeners[k]
		for i, c := range channels {
			if c == ch {
				m.listeners[k] = append(channels[:i], channels[i+1:]...)
				break
			}
		}
		if len(m.listeners[k]) == 0 {
			delete(m.listeners, k)
		}
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial heartbeat
	fmt.Fprintf(w, "data: {\"count\": 0}\n\n")
	flusher.Flush()

	for {
		select {
		case count := <-ch:
			fmt.Fprintf(w, "data: {\"count\": %d}\n\n", count)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
