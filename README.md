
# WebSocket Client [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT) [![Go Reference](https://pkg.go.dev/badge/github.com/mickaelvieira/websocket.svg)](https://pkg.go.dev/github.com/mickaelvieira/websocket)

A Go WebSocket library providing both client and server implementations as a thin and convenient wrapper around the [Gorilla websocket package](https://github.com/gorilla/websocket).

## Features

- **Client** - The client implementation can be used to connect a backend service to a WebSocket server. It will automatically reconnect ; it can send and receive text as well as binary messages over the WebSocket connection.
- **Server** - The server implementation holds a connection with the WebSocket client. It can send and receive text and binary messages over the WebSocket connection.

## Installation

```bash
go get github.com/mickaelvieira/websocket
```

## Usage

### Client Example

```go
package main

import (
    "log"
    "log/slog"
    "os"
    "time"

    "github.com/mickaelvieira/websocket/client"
)

func main() {
    // Create logger
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))

    // Create client with custom configuration
    c := client.NewSocket(
        "wss://echo.websocket.org",
        client.WithLogger(logger),
        client.WithRetryInterval(2*time.Second),
        client.WithMaxRetryAttempts(5),
        client.WithPingInterval(30*time.Second),
    )
    defer c.Close()

    // Monitor connection status
    go func() {
        for status := range c.Statuses() {
            if status.IsConnected() {
                log.Println("✅ Connected to WebSocket server")
            } else {
                log.Println("❌ Disconnected from WebSocket server")
            }
        }
    }()

    // Handle incoming text messages
    go func() {
        for msg := range c.ReadTextMessages() {
            log.Printf("📨 Received text: %s", msg)
        }
    }()

    // Handle incoming binary messages
    go func() {
        for msg := range c.ReadBinaryMessages() {
            log.Printf("📨 Received binary: %v", msg)
        }
    }()

    // Send messages
    c.SendTextMessage("Hello WebSocket!")
    c.SendBinaryMessage([]byte{0x01, 0x02, 0x03})

    // Keep running
    time.Sleep(30 * time.Second)
}
```

### Server Example

```go
package main

import (
    "log"
    "log/slog"
    "net/http"
    "os"

    gows "github.com/gorilla/websocket"
    "github.com/mickaelvieira/websocket/server"
)

var upgrader = gows.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // Configure appropriately for production
    },
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // Upgrade HTTP connection to WebSocket
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Failed to upgrade connection: %v", err)
        return
    }

    // Create logger
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))

    // Create server socket
    s := server.NewSocket(
        conn,
        server.WithLogger(logger),
        server.WithPingInterval(30*time.Second),
    )

    log.Printf("Client connected: %s", s.Id())

    // Handle incoming text messages
    go func() {
        for msg := range s.ReadTextMessages() {
            log.Printf("📨 Received from %s: %s", s.Id(), msg)
            // Echo message back
            s.SendTextMessage("Echo: " + msg)
        }
    }()

    // Handle incoming binary messages
    go func() {
        for msg := range s.ReadBinaryMessages() {
            log.Printf("📨 Received binary from %s: %v", s.Id(), msg)
            // Echo message back
            s.SendBinaryMessage(msg)
        }
    }()

    // Wait for connection to close
    <-s.Wait()
    log.Printf("Client disconnected: %s", s.Id())
}

func main() {
    http.HandleFunc("/ws", handleWebSocket)

    log.Println("Server starting on :8080")
    if err := http.ListenAndServe(":8080", nil); err != nil {
        log.Fatal(err)
    }
}
```

### Configuration Options

#### Client Options

```go
import (
    "net/http"
    "time"

    "github.com/mickaelvieira/websocket/client"
    gows "github.com/gorilla/websocket"
)

client := client.NewSocket("wss://api.example.com/ws",
    // Retry configuration
    client.WithRetryInterval(1*time.Second),      // Wait between retries (default: 5s)
    client.WithMaxRetryAttempts(5),               // Max reconnection attempts (default: 60)

    // Ping configuration
    client.WithPingInterval(30*time.Second),      // Interval between pings (default: 60s)

    // Custom headers for authentication
    client.WithHeaders(http.Header{
        "Authorization": []string{"Bearer " + token},
        "User-Agent":    []string{"MyApp/1.0"},
    }),

    // Custom logger
    client.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil))),

    // Custom dialer configuration
    client.WithDialerModifier(func(dialer *gows.Dialer) {
        dialer.HandshakeTimeout = 10 * time.Second
        dialer.TLSClientConfig = &tls.Config{...}
    }),
)
```

#### Server Options

```go
import (
    "time"

    "github.com/mickaelvieira/websocket/server"
)

server := server.NewSocket(conn,
    // Ping configuration
    server.WithPingInterval(30*time.Second),      // Interval between pings (default: 54s)

    // Custom logger
    server.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil))),
)
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
