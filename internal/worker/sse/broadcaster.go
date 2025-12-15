// Package sse provides Server-Sent Events broadcasting for claude-mnemonic.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/rs/zerolog/log"
)

// Client represents a connected SSE client.
type Client struct {
	ID      string
	Writer  http.ResponseWriter
	Flusher http.Flusher
	Done    chan struct{}
}

// Broadcaster manages SSE client connections and message broadcasting.
type Broadcaster struct {
	clients map[string]*Client
	mu      sync.RWMutex
	nextID  int
}

// NewBroadcaster creates a new SSE broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[string]*Client),
	}
}

// AddClient adds a new SSE client connection.
func (b *Broadcaster) AddClient(w http.ResponseWriter) (*Client, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}

	b.mu.Lock()
	b.nextID++
	id := fmt.Sprintf("client-%d", b.nextID)
	client := &Client{
		ID:      id,
		Writer:  w,
		Flusher: flusher,
		Done:    make(chan struct{}),
	}
	b.clients[id] = client
	clientCount := len(b.clients)
	b.mu.Unlock()

	log.Debug().
		Str("clientId", id).
		Int("totalClients", clientCount).
		Msg("SSE client connected")

	return client, nil
}

// RemoveClient removes a client connection.
func (b *Broadcaster) RemoveClient(client *Client) {
	b.mu.Lock()
	delete(b.clients, client.ID)
	clientCount := len(b.clients)
	b.mu.Unlock()

	close(client.Done)

	log.Debug().
		Str("clientId", client.ID).
		Int("totalClients", clientCount).
		Msg("SSE client disconnected")
}

// removeClientByID removes a client by ID (for dead client cleanup).
func (b *Broadcaster) removeClientByID(id string) {
	b.mu.Lock()
	client, exists := b.clients[id]
	if exists {
		delete(b.clients, id)
	}
	clientCount := len(b.clients)
	b.mu.Unlock()

	if exists && client.Done != nil {
		select {
		case <-client.Done:
			// Already closed
		default:
			close(client.Done)
		}
	}

	log.Debug().
		Str("clientId", id).
		Int("totalClients", clientCount).
		Msg("Dead SSE client removed")
}

// Broadcast sends a message to all connected clients.
func (b *Broadcaster) Broadcast(data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal SSE data")
		return
	}

	message := fmt.Sprintf("data: %s\n\n", jsonData)

	b.mu.RLock()
	clients := make([]*Client, 0, len(b.clients))
	for _, client := range b.clients {
		clients = append(clients, client)
	}
	b.mu.RUnlock()

	// Track dead clients for removal
	var deadClients []*Client

	for _, client := range clients {
		select {
		case <-client.Done:
			continue
		default:
			_, err := client.Writer.Write([]byte(message))
			if err != nil {
				log.Debug().
					Str("clientId", client.ID).
					Err(err).
					Msg("Failed to write to SSE client, marking for removal")
				deadClients = append(deadClients, client)
				continue
			}
			client.Flusher.Flush()
		}
	}

	// Remove dead clients outside the iteration
	for _, client := range deadClients {
		b.removeClientByID(client.ID)
	}
}

// ClientCount returns the number of connected clients.
func (b *Broadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// HandleSSE handles an SSE connection request.
func (b *Broadcaster) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client, err := b.AddClient(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer b.RemoveClient(client)

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"clientId\":\"%s\"}\n\n", client.ID)
	client.Flusher.Flush()

	// Wait for client disconnect
	<-r.Context().Done()
}
