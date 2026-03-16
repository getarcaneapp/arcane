package ws

import (
	"context"
	"log/slog"
	"sync"
)

type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	onEmpty    func()
}

func NewHub(buffer int) *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, buffer),
	}
}

func (h *Hub) SetOnEmpty(fn func()) {
	h.mu.Lock()
	h.onEmpty = fn
	h.mu.Unlock()
}

func (h *Hub) Run(ctx context.Context) {
	defer h.closeAll()

	for {
		select {
		case <-ctx.Done():
			return
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unregister:
			// remove() handles the onEmpty callback when the last client disconnects.
			h.remove(c)
		case msg := <-h.broadcast:
			h.mu.RLock()
			var slowClients []*Client
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// backpressure: drop slow client
					// Collect them to remove outside the lock to avoid spawning goroutines
					slowClients = append(slowClients, c)
				}
			}
			h.mu.RUnlock()

			for _, c := range slowClients {
				h.remove(c)
			}
		}
	}
}

func (h *Hub) Broadcast(msg []byte) {
	select {
	case h.broadcast <- msg:
	default:
		// prevent global stall if hub buffer fills
		// This indicates the hub is not processing messages fast enough
		slog.Warn("websocket hub broadcast buffer full; dropping message")
	}
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	_, exists := h.clients[c]
	if exists {
		delete(h.clients, c)
		close(c.send)
		_ = c.conn.Close()
	}
	// Capture onEmpty under the lock so we can call it outside.
	var onEmpty func()
	if exists && len(h.clients) == 0 && h.onEmpty != nil {
		onEmpty = h.onEmpty
	}
	h.mu.Unlock()

	// Call outside the lock to avoid holding the mutex during cancel().
	if onEmpty != nil {
		slog.Debug("websocket hub empty after client removal, triggering cleanup")
		onEmpty()
	}
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	for c := range h.clients {
		close(c.send)
		_ = c.conn.Close()
		delete(h.clients, c)
	}
	h.mu.Unlock()
}
