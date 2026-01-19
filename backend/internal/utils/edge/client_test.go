package edge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelClient_HandleRequest(t *testing.T) {
	// 1. Setup Local Service (that agent proxies TO)
	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/local/api" {
			w.Header().Set("X-Local", "true")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("local response"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// 2. Setup Mock Manager (that agent connects TO)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// Send a request to the agent
		reqMsg := &TunnelMessage{
			ID:     "req-1",
			Type:   MessageTypeRequest,
			Method: "GET",
			Path:   "/local/api",
		}
		data, _ := json.Marshal(reqMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Wait for response
		_, respData, _ := conn.ReadMessage()
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)

		// Validate response from agent
		assert.Equal(t, "req-1", resp.ID)
		assert.Equal(t, MessageTypeResponse, resp.Type)
		assert.Equal(t, http.StatusOK, resp.Status)
		assert.Equal(t, "true", resp.Headers["X-Local"])
		assert.Equal(t, "local response", string(resp.Body))
	}))
	defer managerServer.Close()

	// 3. Configure and Start Agent Client
	cfg := &config.Config{
		ManagerApiUrl:         managerServer.URL,
		AgentToken:            "test-token",
		EdgeReconnectInterval: 1,
	}

	client := NewTunnelClient(cfg, localHandler)
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run client in background
	go client.Start(ctx)

	// Wait for process to finish or timeout
	time.Sleep(100 * time.Millisecond)
}

func TestTunnelClient_WebSocketProxy(t *testing.T) {
	// 1. Setup Local Service with WS
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo
			_ = conn.WriteMessage(mt, append([]byte("local echo: "), data...))
		}
	}))
	defer localServer.Close()

	localPort := strings.Split(localServer.Listener.Addr().String(), ":")[1]

	// 2. Setup Mock Manager
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// Send WS Start
		startMsg := &TunnelMessage{
			ID:   "ws-1",
			Type: MessageTypeWebSocketStart,
			Path: "/", // Connect to root of local server
		}
		data, _ := json.Marshal(startMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Send Data
		dataMsg := &TunnelMessage{
			ID:            "ws-1",
			Type:          MessageTypeWebSocketData,
			Body:          []byte("hello"),
			WSMessageType: websocket.TextMessage,
		}
		data, _ = json.Marshal(dataMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Read Echo
		_, respData, _ := conn.ReadMessage()
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)

		assert.Equal(t, MessageTypeWebSocketData, resp.Type)
		assert.Equal(t, "local echo: hello", string(resp.Body))
	}))
	defer managerServer.Close()

	// 3. Configure Agent
	cfg := &config.Config{
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "test-token",
		Port:          localPort, // Tell agent where local service is
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler()) // Handler ignored for WS
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go client.Start(ctx)
	time.Sleep(100 * time.Millisecond)
}

func TestTunnelClient_HandleRequest_Errors(t *testing.T) {
	// Setup Mock Manager
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// 1. Send request with invalid URL to trigger error
		reqMsg := &TunnelMessage{
			ID:     "req-err",
			Type:   MessageTypeRequest,
			Method: "GET",
			Path:   "://invalid-url",
		}
		data, _ := json.Marshal(reqMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Expect error response
		_, respData, _ := conn.ReadMessage()
		var resp TunnelMessage
		_ = json.Unmarshal(respData, &resp)

		assert.Equal(t, "req-err", resp.ID)
		assert.Equal(t, 500, resp.Status)

		// 2. Send unknown message type
		unknownMsg := &TunnelMessage{
			ID:   "unknown",
			Type: "unknown_type",
		}
		data, _ = json.Marshal(unknownMsg)
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}))
	defer managerServer.Close()

	cfg := &config.Config{
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "test-token",
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go client.Start(ctx)
	time.Sleep(100 * time.Millisecond)
}

func TestTunnelClient_InternalHelpers(t *testing.T) {
	// Mock connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		ManagerApiUrl: server.URL,
		AgentToken:    "test-token",
	}
	client := NewTunnelClient(cfg, nil)

	// Manually connect
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	client.conn = NewTunnelConn(conn)

	// Test sendWebSocketData
	err = client.sendWebSocketData("stream-1", websocket.TextMessage, []byte("data"))
	require.NoError(t, err)

	// Test sendWebSocketClose
	client.sendWebSocketClose("stream-1")

	// Test sendErrorResponse
	client.sendErrorResponse("req-1", 500, "error")
}

func TestStartTunnelClient(t *testing.T) {
	// Just test the configuration checks
	ctx := context.Background()

	// Disabled
	StartTunnelClient(ctx, &config.Config{EdgeAgent: false}, nil)

	// Missing URL
	StartTunnelClient(ctx, &config.Config{EdgeAgent: true, ManagerApiUrl: ""}, nil)

	// Missing Token
	StartTunnelClient(ctx, &config.Config{EdgeAgent: true, ManagerApiUrl: "http://x", AgentToken: ""}, nil)
}
