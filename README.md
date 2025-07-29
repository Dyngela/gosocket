# GO Socket

A simple socket library for Go, inspired by socketio declarative style.

Its meant to fill my use cases, so it may not be suitable for all applications. 
The purpose is to provide a simple way to handle socket connections and events in Go applications while avoiding boilerplate code in application logic.

## Features

- Declarative event handling
- Middleware support
- Auth middleware at connection initialization


### Usage

Download the package:
```bash
go get github.com/Dyngela/gosocket
```

There's default upgrader that allows all origins. Same for auth middleware, which always returns true. You can customize it here as needed
Declare a socket server:
```go
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
```

You can add middleware to the server on every request:
```go
	socketServer.Use(func(client *socket.Client, msg socket.Message) bool {
		log.Printf("Client %s sent event: %s", client.ID, msg.Event)
		return true // Return false to block the message
	})
```

Finally you can register event handlers in two ways:

First using On which take event name and an any in data
```go
    socketServer.On("eventName", func(client *socket.Client, data any) {
        log.Printf("Received message from %s: %s", client.ID, msg.Data)
        client.Emit("joined", map[string]interface{}{
            "message": "You have joined the room!",
            "room":    data.Room,
        })
    })
```

Or you can use a typed handler for type safety and better code completion.
```go
    import "github.com/Dyngela/gosocket"

    type JoinRoomData struct {
		Room string `json:"room"`
	}

	socket.OnTyped[JoinRoomData](socketServer, "join", func(client *socket.Client, data JoinRoomData) {
		log.Printf("Client %s joining room: %s", client.ID, data.Room)
		client.Join(data.Room)
		client.Emit("joined", CustomResponse{
			Message: "You have joined the room!",
            Room:    data.Room,
        })
	})
```
