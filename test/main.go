package main

import (
	"github.com/gorilla/websocket"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	socket "gosocket"
)

type TEST struct {
	Message string `json:"message"`
}

func main() {
	// Create a new socket server
	socketServer := socket.NewServer(&socket.ServerConfig{
		AuthMiddleware: func(w http.ResponseWriter, r *http.Request) bool {
			return true
		},
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	})

	socketServer.On("test", func(client *socket.Client, data any) {
		type TestData struct {
			Message string `json:"message"`
		}
		log.Printf("Received test event from client %s: %v", client.ID, data)
		client.BroadcastToRoom("a", "test_response", map[string]interface{}{
			"message": "Test event received successfully!",
			"data": &TestData{
				Message: "Hello from the server!",
			},
		})
	})

	socketServer.Use(func(client *socket.Client, msg socket.Message) bool {
		log.Printf("Client %s sent event: %s", client.ID, msg.Event)
		return true // Return false to block the message
	})

	type JoinRoomData struct {
		Room string `json:"room"`
	}

	socket.OnTyped[JoinRoomData](socketServer, "join", func(client *socket.Client, data JoinRoomData) {
		log.Printf("Client %s joining room: %s", client.ID, data.Room)
		client.Join(data.Room)
		client.Emit("joined", map[string]interface{}{
			"message": "You have joined the room!",
			"room":    data.Room,
		})
	})

	// Handle connection events
	socket.OnTyped[TEST](socketServer, "connect", func(client *socket.Client, data TEST) {
		log.Printf("Client %s connected", client.ID)
		client.Emit("welcome", map[string]interface{}{
			"message":  "Welcome to the server!",
			"clientId": client.ID,
		})
	})

	// Handle disconnection events
	socketServer.On("disconnect", func(client *socket.Client, data interface{}) {
		log.Printf("Client %s disconnected", client.ID)
	})

	router := mux.NewRouter()

	// Serve static files for the client
	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))),
	)

	// Basic HTTP routes
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Health check endpoint
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Attach the socket server to the router
	socketServer.AttachToMux(router, "/socket")

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}
