package api

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Message represents a WebSocket message
type Message struct {
	Type string      `json:"type"` // "share" or "snapshot"
	Data interface{} `json:"data"`
}

// WebSocketHub manages WebSocket connections and broadcasts
type WebSocketHub struct {
	clients    map[*websocket.Conn]bool
	clientsMu  sync.RWMutex
	broadcast  chan Message
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	done       chan struct{}
}

// NewWebSocketHub creates a new WebSocketHub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan Message, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		done:       make(chan struct{}),
	}
}

// Run starts the hub's main loop to handle register/unregister/broadcast
func (h *WebSocketHub) Run() {
	for {
		select {
		case <-h.done:
			// Close all connections on shutdown
			h.clientsMu.Lock()
			for conn := range h.clients {
				conn.Close()
				delete(h.clients, conn)
			}
			h.clientsMu.Unlock()
			return

		case conn := <-h.register:
			h.clientsMu.Lock()
			h.clients[conn] = true
			h.clientsMu.Unlock()
			log.Printf("WebSocket client connected, total clients: %d", len(h.clients))

		case conn := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.clientsMu.Unlock()
			log.Printf("WebSocket client disconnected, total clients: %d", len(h.clients))

		case msg := <-h.broadcast:
			h.clientsMu.RLock()
			for conn := range h.clients {
				err := conn.WriteJSON(msg)
				if err != nil {
					log.Printf("WebSocket write error: %v", err)
					// Queue for unregister
					go func(c *websocket.Conn) {
						h.unregister <- c
					}(conn)
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

// Stop stops the hub
func (h *WebSocketHub) Stop() {
	close(h.done)
}

// Broadcast sends a message to all connected clients
func (h *WebSocketHub) Broadcast(msg Message) {
	select {
	case h.broadcast <- msg:
	default:
		// Drop message if buffer is full
		log.Printf("WebSocket broadcast buffer full, dropping message")
	}
}

// handleWebSocket handles WebSocket upgrade and connection
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	s.hub.register <- conn

	// Read loop to detect client disconnect
	go func() {
		defer func() {
			s.hub.unregister <- conn
		}()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()
}
