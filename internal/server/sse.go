package server

import (
	"fmt"
	"net/http"
	"sync"
)

// SSEBroker manages Server-Sent Events connections and broadcasts events
// to all connected clients for live reload support.
type SSEBroker struct {
	clients    map[chan string]struct{}
	mu         sync.RWMutex
	register   chan chan string
	unregister chan chan string
	broadcast  chan string
	done       chan struct{}
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients:    make(map[chan string]struct{}),
		register:   make(chan chan string),
		unregister: make(chan chan string),
		broadcast:  make(chan string, 64),
		done:       make(chan struct{}),
	}
}

// Start begins the broker event loop in a background goroutine.
func (b *SSEBroker) Start() {
	go b.run()
}

// Stop shuts down the broker and closes all client connections.
func (b *SSEBroker) Stop() {
	close(b.done)
}

// Broadcast sends an event to all connected clients.
func (b *SSEBroker) Broadcast(event string) {
	select {
	case b.broadcast <- event:
	case <-b.done:
	}
}

func (b *SSEBroker) run() {
	for {
		select {
		case ch := <-b.register:
			b.mu.Lock()
			b.clients[ch] = struct{}{}
			b.mu.Unlock()

		case ch := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[ch]; ok {
				delete(b.clients, ch)
				close(ch)
			}
			b.mu.Unlock()

		case event := <-b.broadcast:
			b.mu.RLock()
			for ch := range b.clients {
				select {
				case ch <- event:
				default:
					// Client is slow; skip this event.
				}
			}
			b.mu.RUnlock()

		case <-b.done:
			b.mu.Lock()
			for ch := range b.clients {
				delete(b.clients, ch)
				close(ch)
			}
			b.mu.Unlock()
			return
		}
	}
}

// ServeHTTP handles SSE connections from clients.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 16)
	b.register <- ch

	defer func() {
		b.unregister <- ch
	}()

	// Send initial connection event.
	fmt.Fprintf(w, "data: connected\n\n")
	flusher.Flush()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}
