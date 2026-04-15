package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Event types pushed to dashboard clients
type Event struct {
	Type    string      `json:"type"` // ride_update | match_update | av_update | stats
	Payload interface{} `json:"payload"`
}

// Hub manages all connected WebSocket clients
type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]bool)}
}

// HandleWS is the Gin handler for WebSocket upgrade
func (h *Hub) HandleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()
	log.Printf("[ws] client connected (%d total)", len(h.clients))

	// Keep connection alive with pings
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Read loop (discard incoming messages, just detect disconnect)
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
			log.Printf("[ws] client disconnected (%d remaining)", len(h.clients))
		}()
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Ping loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			h.mu.RLock()
			_, ok := h.clients[conn]
			h.mu.RUnlock()
			if !ok {
				return
			}
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}()
}

// Broadcast sends an event to all connected clients
func (h *Hub) Broadcast(evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.clients {
		err := conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			log.Printf("[ws] write error: %v", err)
			conn.Close()
			go func(c *websocket.Conn) {
				h.mu.Lock()
				delete(h.clients, c)
				h.mu.Unlock()
			}(conn)
		}
	}
}

// BroadcastRideUpdate pushes a ride status change
func (h *Hub) BroadcastRideUpdate(rideID, status, avID string) {
	h.Broadcast(Event{
		Type: "ride_update",
		Payload: map[string]string{
			"ride_id": rideID,
			"status":  status,
			"av_id":   avID,
		},
	})
}

// BroadcastMatchUpdate pushes matching state change
func (h *Hub) BroadcastMatchUpdate(rideID, status string, cursor int, candidates []string) {
	h.Broadcast(Event{
		Type: "match_update",
		Payload: map[string]interface{}{
			"ride_id":    rideID,
			"status":     status,
			"cursor":     cursor,
			"candidates": candidates,
		},
	})
}

// BroadcastAVUpdate pushes AV status change
func (h *Hub) BroadcastAVUpdate(avID, status string, lat, lng float64) {
	h.Broadcast(Event{
		Type: "av_update",
		Payload: map[string]interface{}{
			"av_id":  avID,
			"status": status,
			"lat":    lat,
			"lng":    lng,
		},
	})
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
