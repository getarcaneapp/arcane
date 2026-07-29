package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestIsWebSocketUpgradeRequest(t *testing.T) {
	tests := []struct {
		name       string
		connection string
		upgrade    string
		want       bool
	}{
		{"websocket upgrade", "Upgrade", "websocket", true},
		{"case insensitive with token list", "keep-alive, UPGRADE", "WebSocket", true},
		{"plain request", "", "", false},
		{"upgrade to h2c", "Upgrade", "h2c", false},
		{"only upgrade header from reverse proxy", "", "websocket", false},
		{"only connection upgrade from reverse proxy", "upgrade, keep-alive", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.connection != "" {
				r.Header.Set("Connection", tt.connection)
			}
			if tt.upgrade != "" {
				r.Header.Set("Upgrade", tt.upgrade)
			}
			if got := IsWebSocketUpgradeRequest(r); got != tt.want {
				t.Errorf("IsWebSocketUpgradeRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}
