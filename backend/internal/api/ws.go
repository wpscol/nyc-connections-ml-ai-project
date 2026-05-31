package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type client struct {
	conn      *websocket.Conn
	send      chan []byte
	sessionID string
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*client]bool
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*client]bool)}
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.sessionID] == nil {
		h.clients[c.sessionID] = make(map[*client]bool)
	}
	h.clients[c.sessionID][c] = true
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[c.sessionID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.sessionID)
		}
	}
}

// ActiveSessions returns session IDs that currently have at least one WS client.
func (h *Hub) ActiveSessions() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id, set := range h.clients {
		if len(set) > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func (h *Hub) Broadcast(sessionID string, event interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := h.clients[sessionID]
	h.mu.RUnlock()
	for c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session", http.StatusBadRequest)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("ws upgrade:", err)
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 32), sessionID: sessionID}
	h.register(c)

	go c.writePump(h)
	c.readPump(h) // blocks until disconnect
}

func (c *client) readPump(h *Hub) {
	defer func() {
		h.unregister(c)
		c.conn.Close()
	}()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (c *client) writePump(h *Hub) {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}
