package ws_client

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Test server helper
func createTestServer() *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close() // nolint:errcheck

		// Echo server - read and write back messages
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			if err := conn.WriteMessage(messageType, message); err != nil {
				break
			}
		}
	}))
}

func TestStateTransitions(t *testing.T) {
	// Create test server
	server := createTestServer()
	defer server.Close()

	// Convert http://... to ws://...
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	t.Logf("Testing with URL: %s", wsURL)

	// Create client with shorter timeouts for testing
	client := NewClient(wsURL,
		WithRetryInterval(100*time.Millisecond),
		WithMaxRetryAttempts(3),
		WithLogger(slog.Default()),
		WithDialerModifier(func(d *websocket.Dialer) {
			d.HandshakeTimeout = 5 * time.Second
		}),
	)

	// Monitor status changes in background
	connected := make(chan bool, 1)
	disconnected := make(chan bool, 1)

	go func() {
		for status := range client.Statuses() {
			t.Logf("Status change: %v (connected=%v)", status, status == StatusConnected)
			switch status {
			case StatusConnected:
				t.Logf("Sending connected signal")
				select {
				case connected <- true:
					t.Logf("Connected signal sent")
				default:
					t.Logf("Connected signal channel full")
				}
			case StatusDisconnected:
				t.Logf("Sending disconnected signal")
				select {
				case disconnected <- true:
					t.Logf("Disconnected signal sent")
				default:
					t.Logf("Disconnected signal channel full")
				}
			}
		}
		t.Logf("Status monitoring goroutine ended")
	}()

	// Test initial connection
	select {
	case <-connected:
		t.Logf("✓ Successfully connected")
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Test sending and receiving a message
	testMessage := "Hello, WebSocket!"
	if err := client.SendTextMessage(testMessage); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	select {
	case received := <-client.ReadTextMessages():
		if received != testMessage {
			t.Fatalf("Expected %q, got %q", testMessage, received)
		}
		t.Logf("✓ Successfully sent and received message: %q", received)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	// Test graceful close
	if err := client.Close(); err != nil {
		t.Fatalf("Failed to close client: %v", err)
	}

	// Wait for disconnection status
	select {
	case <-disconnected:
		t.Logf("✓ Successfully disconnected")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for disconnection")
	}
}

func TestReconnectionLogic(t *testing.T) {
	// Create client pointing to non-existent server
	client := NewClient("ws://localhost:99999",
		WithRetryInterval(100*time.Millisecond),
		WithMaxRetryAttempts(2),
		WithLogger(slog.Default()),
		WithDialerModifier(func(d *websocket.Dialer) {
			d.HandshakeTimeout = 500 * time.Millisecond
		}),
	)

	// Monitor for status changes
	statusChanges := make(chan Status, 10)

	go func() {
		for status := range client.Statuses() {
			t.Logf("Received status: %v", status)
			statusChanges <- status
		}
		close(statusChanges)
		t.Logf("Status channel closed")
	}()

	// Collect status changes and look for final disconnection
	var statuses []Status
	timeout := time.After(3 * time.Second)

	for {
		select {
		case status, ok := <-statusChanges:
			if !ok {
				// Channel closed, check if we got what we expected
				if len(statuses) > 0 && statuses[len(statuses)-1] == StatusDisconnected {
					t.Logf("✓ Client properly gave up after max retry attempts (statuses: %v)", statuses)
					return
				}
				t.Fatalf("Status channel closed unexpectedly, statuses: %v", statuses)
			}
			statuses = append(statuses, status)

			// If we get a disconnected status after some attempts, that's success
			if status == StatusDisconnected && len(statuses) > 1 {
				t.Logf("✓ Client properly gave up after max retry attempts (statuses: %v)", statuses)
				return
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for retry attempts to complete, statuses received: %v", statuses)
		}
	}
}

func TestIsConnected(t *testing.T) {
	server := createTestServer()
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL, WithLogger(slog.Default()))

	// Wait for connection
	connected := make(chan bool, 1)
	disconnected := make(chan bool, 1)

	go func() {
		for status := range client.Statuses() {
			switch status {
			case StatusConnected:
				select {
				case connected <- true:
				default:
				}
			case StatusDisconnected:
				select {
				case disconnected <- true:
				default:
				}
			}
		}
	}()

	// Wait for connection
	<-connected

	if !client.IsConnected() {
		t.Fatal("Expected client to be connected")
	}
	t.Logf("✓ IsConnected() correctly returns true when connected")

	// Close and check again
	client.Close() // nolint:errcheck

	// Wait for disconnection
	select {
	case <-disconnected:
		t.Logf("Received disconnection status")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for disconnection")
	}

	// Give a small additional delay for the state to be fully updated
	time.Sleep(10 * time.Millisecond)

	if client.IsConnected() {
		t.Fatal("Expected client to be disconnected")
	}
	t.Logf("✓ IsConnected() correctly returns false when disconnected")
}
