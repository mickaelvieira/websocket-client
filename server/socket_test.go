package server

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

func TestNewServerSocket(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket := NewSocket(conn)

		if socket == nil {
			t.Fatal("Expected server to be created")
		}

		if socket.Id() == "" {
			t.Error("Expected server ID to be set")
		}

		defer func() {
			if err := socket.Close(); err != nil {
				t.Logf("Error closing server: %v", err)
			}
		}()
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection to trigger the handler
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)
}

func TestServerReceiveTextMessage(t *testing.T) {
	received := make(chan string, 1)
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(conn)

		// Read messages in goroutine
		go func() {
			for msg := range socket.ReadTextMessages() {
				received <- msg
			}
		}()
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection
	dialer := websocket.DefaultDialer
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Send a text message from client
	testMessage := "Hello from client!"
	if err := clientConn.WriteMessage(websocket.TextMessage, []byte(testMessage)); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Wait for the message to be received
	select {
	case msg := <-received:
		if msg != testMessage {
			t.Errorf("Expected message %q, got %q", testMessage, msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	if socket != nil {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing server socket: %v", err)
		}
	}
}

func TestServerReceiveBinaryMessage(t *testing.T) {
	received := make(chan []byte, 1)
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(conn)

		// Read messages in goroutine
		go func() {
			for msg := range socket.ReadBinaryMessages() {
				received <- msg
			}
		}()
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection
	dialer := websocket.DefaultDialer
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Send a binary message from client
	testData := []byte{0x01, 0x02, 0x03, 0x04}
	if err := clientConn.WriteMessage(websocket.BinaryMessage, testData); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Wait for the message to be received
	select {
	case data := <-received:
		if len(data) != len(testData) {
			t.Errorf("Expected data length %d, got %d", len(testData), len(data))
		}
		for i, b := range data {
			if b != testData[i] {
				t.Errorf("Expected byte %d at position %d, got %d", testData[i], i, b)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	if socket != nil {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing server socket: %v", err)
		}
	}
}

func TestServerSendTextMessage(t *testing.T) {
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(conn)

		// Send a message after a small delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			socket.SendTextMessage("Hello from server!")
		}()
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection
	dialer := websocket.DefaultDialer
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Read message from server
	if err := clientConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}
	messageType, message, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	if messageType != websocket.TextMessage {
		t.Errorf("Expected text message type, got %d", messageType)
	}

	expectedMessage := "Hello from server!"
	if string(message) != expectedMessage {
		t.Errorf("Expected message %q, got %q", expectedMessage, string(message))
	}

	if socket != nil {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing server socket: %v", err)
		}
	}
}

func TestServerSendBinaryMessage(t *testing.T) {
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(conn)

		// Send a binary message after a small delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			socket.SendBinaryMessage([]byte{0x05, 0x06, 0x07, 0x08})
		}()
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection
	dialer := websocket.DefaultDialer
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Read message from server
	if err := clientConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}
	messageType, message, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	if messageType != websocket.BinaryMessage {
		t.Errorf("Expected binary message type, got %d", messageType)
	}

	expectedData := []byte{0x05, 0x06, 0x07, 0x08}
	if len(message) != len(expectedData) {
		t.Errorf("Expected data length %d, got %d", len(expectedData), len(message))
	}
	for i, b := range message {
		if b != expectedData[i] {
			t.Errorf("Expected byte %d at position %d, got %d", expectedData[i], i, b)
		}
	}

	if socket != nil {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing server socket: %v", err)
		}
	}
}

func TestServerPingPong(t *testing.T) {
	pongReceived := make(chan string, 1)
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(
			conn,
			WithPingInterval(1*time.Second),
		)
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection with pong handler
	dialer := websocket.DefaultDialer
	client, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()

	// Set up pong handler on client
	client.SetPingHandler(func(appData string) error {
		t.Logf("Client received ping with data: %s", appData)
		pongReceived <- appData
		// Send pong back
		if err := client.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second)); err != nil {
			t.Logf("Failed to send pong: %v", err)
		}
		return nil
	})

	// Keep reading to process control frames
	go func() {
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				break
			}
		}
	}()

	// Wait for at least one ping/pong exchange
	select {
	case <-pongReceived:
		t.Log("Ping/pong exchange successful")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for ping")
	}

	if socket != nil {
		if err := socket.Close(); err != nil {
			t.Logf("Error closing server socket: %v", err)
		}
	}
}

func TestServerClose(t *testing.T) {
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(conn)

		// Close after a delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			if err := socket.Close(); err != nil {
				t.Logf("Close error: %v", err)
			}
		}()
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection
	dialer := websocket.DefaultDialer
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("Error closing client: %v", err)
		}
	}()
	// Give server time to initialize and close
	time.Sleep(200 * time.Millisecond)

	// Read the close frame from server
	if err := clientConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}
	_, _, err = clientConn.ReadMessage()
	if err == nil {
		t.Fatal("Expected error after server close")
	}

	// Verify it's a close error
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure) && !websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
		t.Logf("Expected close error, got: %v", err)
	} else {
		t.Log("Server closed successfully")
	}
}

func TestServerWaitChannel(t *testing.T) {
	var socket Socket

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Failed to upgrade connection: %v", err)
			return
		}

		socket = NewSocket(conn)
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Create a client connection
	dialer := websocket.DefaultDialer
	clientConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}

	// Give server time to initialize
	time.Sleep(100 * time.Millisecond)

	// Close client connection
	if err := clientConn.Close(); err != nil {
		t.Logf("Error closing client: %v", err)
	}

	// Wait for server to detect close
	if socket != nil {
		select {
		case <-socket.Wait():
			t.Log("Server detected close via Wait channel")
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for close notification")
		}
	}
}
