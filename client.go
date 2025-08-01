package socket

import (
	"github.com/gorilla/websocket"
	"log"
	"errors"
	"sync"
	"time"
)

// Client represents a connected websocket client
type Client struct {
	ID     string
	conn   *websocket.Conn
	server *Server
	send   chan Message
	rooms  map[string]bool
	mu     sync.RWMutex
}

func (c *Client) Emit(event string, data interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msg := Message{
		Event: event,
		Data:  data,
	}
	select {
	case c.send <- msg:
	default:
		close(c.send)
		delete(c.server.clients, c.ID)
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			response := map[string]interface{}{
				"event": message.Event,
				"data":  message.Data,
			}

			if err := c.conn.WriteJSON(response); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		}
	}

}

func (c *Client) readPump() {
	defer func() {
		c.server.unregister <- c
		c.conn.Close()
	}()

	reconnectAttempt := 0
	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				if closeErr.Code == 1001 || closeErr.Code == 1006 {
					if reconnectAttempt > 15 {
						c.server.unregister <- c
						break
					}
					reconnectAttempt++
					time.Sleep(5 * time.Second)
					continue
				}
			}
			log.Printf("WebSocket read error: %v", err)
			continue
		}

		// Apply middleware
		shouldContinue := true
		for _, middleware := range c.server.middleware {
			if !middleware(c, msg) {
				shouldContinue = false
				break
			}
		}

		if !shouldContinue {
			continue
		}

		// Handle the message
		c.server.mu.RLock()
		if handler, exists := c.server.handlers[msg.Event]; exists {
			if err := handler.Handle(c, msg.Data); err != nil {
				log.Printf("Error handling event %s: %v", msg.Event, err)
				continue // Skip to the next message if there's an error
			}
		}
		c.server.mu.RUnlock()
	}
}

func (c *Client) Join(room string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rooms[room] = true

	if c.server.rooms[room] == nil {
		c.server.rooms[room] = make(map[string]*Client)
	}
	c.server.rooms[room][c.ID] = c
}

// Leave removes the client from a room
func (c *Client) Leave(room string) {
	c.server.leaveRoom(c, room)
}

func (c *Client) BroadcastToRoom(room string, event string, data interface{}) {
	c.server.mu.RLock()
	defer c.server.mu.RUnlock()
	if clients, ok := c.server.rooms[room]; ok {
		for _, client := range clients {
			if client.ID != c.ID {
				client.Emit(event, data)
			}
		}
	}
}

func (c *Client) Broadcast(event string, data interface{}) {
	c.server.mu.RLock()
	defer c.server.mu.RUnlock()
	c.server.broadcast <- Message{
		Event: event,
		Data:  data,
	}
}
