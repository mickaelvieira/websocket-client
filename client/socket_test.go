package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// setupEchoServer creates a test WebSocket server that echoes messages back
func setupEchoServer(t *testing.T) (*httptest.Server, string) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("Error closing connection: %v", err)
			}
		}()

		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					t.Logf("Unexpected close error: %v", err)
				}
				break
			}

			// Echo the message back
			if err := conn.WriteMessage(messageType, message); err != nil {
				t.Logf("Write error: %v", err)
				break
			}
		}
	})

	server := httptest.NewServer(handler)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

func TestNewClientSocket(t *testing.T) {
	server, wsURL := setupEchoServer(t)
	defer server.Close()

	socket := NewSocket(wsURL)
	if socket == nil {
		t.Fatal("Expected client to be created")
	}
	defer func() {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	if socket.Id() == "" {
		t.Error("Expected client ID to be set")
	}
}

func TestClientConnection(t *testing.T) {
	server, wsURL := setupEchoServer(t)
	defer server.Close()

	socket := NewSocket(wsURL)
	defer func() {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Wait for connection
	connected := false
	timeout := time.After(5 * time.Second)

	for !connected {
		select {
		case status := <-socket.Statuses():
			if status.IsConnected() {
				connected = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for connection")
		}
	}

	if !socket.IsConnected() {
		t.Error("Expected client to be connected")
	}
}

func TestClientSendTextMessage(t *testing.T) {
	server, wsURL := setupEchoServer(t)
	defer server.Close()

	socket := NewSocket(wsURL)
	defer func() {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Wait for connection
	waitForConnection(t, socket)

	testMessage := "Hello, WebSocket!"

	// Send message
	socket.SendTextMessage(testMessage)

	// Read echoed message
	select {
	case msg := <-socket.ReadTextMessages():
		if msg != testMessage {
			t.Errorf("Expected message %q, got %q", testMessage, msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for echo message")
	}
}

func TestClientSendBinaryMessage(t *testing.T) {
	server, wsURL := setupEchoServer(t)
	defer server.Close()

	socket := NewSocket(wsURL)
	defer func() {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Wait for connection
	waitForConnection(t, socket)

	testData := []byte{0x01, 0x02, 0x03, 0x04}

	// Send binary message
	socket.SendBinaryMessage(testData)

	// Read echoed message
	select {
	case data := <-socket.ReadBinaryMessages():
		if len(data) != len(testData) {
			t.Errorf("Expected data length %d, got %d", len(testData), len(data))
		}
		for i, b := range data {
			if b != testData[i] {
				t.Errorf("Expected byte %d at position %d, got %d", testData[i], i, b)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for echo message")
	}
}

func TestClientGracefulClose(t *testing.T) {
	server, wsURL := setupEchoServer(t)
	defer server.Close()

	socket := NewSocket(wsURL)

	// Wait for connection
	waitForConnection(t, socket)

	// Close gracefully
	if err := socket.Close(); err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}

	// Wait for disconnection status
	timeout := time.After(5 * time.Second)
	disconnected := false

	for !disconnected {
		select {
		case status, ok := <-socket.Statuses():
			if !ok {
				// Channel closed
				disconnected = true
			} else if !status.IsConnected() {
				disconnected = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for disconnection after close")
		}
	}
}

// Helper function to wait for client connection
func waitForConnection(t *testing.T, client Socket) {
	timeout := time.After(5 * time.Second)
	for {
		select {
		case status := <-client.Statuses():
			if status.IsConnected() {
				return
			}
		case <-timeout:
			t.Fatal("Timeout waiting for connection")
		}
	}
}
