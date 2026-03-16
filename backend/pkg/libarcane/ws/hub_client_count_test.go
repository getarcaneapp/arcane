package ws

// ClientCount is test-only and intentionally compiled from *_test.go to avoid
// deadcode in production builds.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
