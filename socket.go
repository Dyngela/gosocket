package socket

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
)

// Message represents a socket message
type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// Server represents the websocket server
type Server struct {
	clients        map[string]*Client
	rooms          map[string]map[string]*Client
	handlers       map[string]EventHandler
	upgrader       websocket.Upgrader
	mu             sync.RWMutex
	register       chan *Client
	unregister     chan *Client
	broadcast      chan Message
	authMiddleware func(w http.ResponseWriter, r *http.Request) bool
	middleware     []func(*Client, Message) bool
}

type ServerConfig struct {
	Upgrader websocket.Upgrader
	// Function to handle authentication at startup
	AuthMiddleware func(w http.ResponseWriter, r *http.Request) bool
}

type EventHandler interface {
	Handle(*Client, any) error
}

func (th *TypedHandler[T]) Handle(client *Client, data any) error {
	var payload T
	// Désérialisation correcte
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return err
	}
	th.handler(client, payload)
	return nil
}

type TypedHandler[T any] struct {
	handler func(*Client, T)
}

// NewServer creates a new socket server
func NewServer(config *ServerConfig) *Server {
	s := &Server{
		clients:    make(map[string]*Client),
		rooms:      make(map[string]map[string]*Client),
		handlers:   make(map[string]EventHandler),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in development
			},
		},
		authMiddleware: func(w http.ResponseWriter, r *http.Request) bool {
			return true
		},
		middleware: make([]func(*Client, Message) bool, 0),
	}

	if config != nil {
		if config.Upgrader.CheckOrigin != nil {
			s.upgrader = config.Upgrader
		}
		if config.AuthMiddleware != nil {
			s.authMiddleware = config.AuthMiddleware
		}
	}
	go s.run()

	return s
}

// Use adds middleware to the server
func (s *Server) Use(middleware func(*Client, Message) bool) {
	s.middleware = append(s.middleware, middleware)
}

// OnTyped registers a typed event handler
// T is the type of the data expected in the event
func OnTyped[T any](s *Server, event string, handler func(*Client, T)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[event] = &TypedHandler[T]{handler: handler}
}

func (s *Server) AttachToGin(router *gin.Engine, path string) {
	router.GET(path, func(c *gin.Context) {
		s.handleWebSocket(c.Writer, c.Request)
	})
}

func (s *Server) AttachToMux(router *mux.Router, path string) {
	router.HandleFunc(path, s.handleWebSocket)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.authMiddleware != nil && !s.authMiddleware(w, r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	client := &Client{
		ID:     uuid.NewString(),
		conn:   conn,
		server: s,
		send:   make(chan Message, 256),
		rooms:  make(map[string]bool),
	}

	s.register <- client

	go client.writePump()
	go client.readPump()
}

// On registers a generic event handler
// The handler receives the client and the data as an interface{}
func (s *Server) On(event string, handler func(*Client, interface{})) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[event] = &TypedHandler[interface{}]{handler: func(client *Client, data interface{}) {
		handler(client, data)
	}}
}

func (s *Server) run() {
	for {
		select {
		case client := <-s.register:
			s.mu.Lock()
			s.clients[client.ID] = client
			s.mu.Unlock()
			log.Printf("Client %s connected", client.ID)

			// Trigger connect event
			if handler, exists := s.handlers["connect"]; exists {
				handler.Handle(client, []byte{})
			}

		case client := <-s.unregister:
			s.mu.Lock()
			if _, ok := s.clients[client.ID]; ok {
				delete(s.clients, client.ID)
				close(client.send)

				// Remove client from all rooms
				for room := range client.rooms {
					s.leaveRoom(client, room)
				}
			}
			s.mu.Unlock()
			log.Printf("Client %s disconnected", client.ID)

			// Trigger disconnect event
			if handler, exists := s.handlers["disconnect"]; exists {
				handler.Handle(client, []byte{})
			}

		case message := <-s.broadcast:
			s.mu.RLock()
			for _, client := range s.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(s.clients, client.ID)
				}
			}
			s.mu.RUnlock()
		}
	}
}

func (s *Server) leaveRoom(client *Client, room string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if clients, exists := s.rooms[room]; exists {
		delete(clients, client.ID)
		if len(clients) == 0 {
			delete(s.rooms, room)
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	delete(client.rooms, room)
}
