package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/stretchr/testify/require"
)

func TestValidateSafeRemoteURL(t *testing.T) {
	lookupIP := func(ctx context.Context, host string) ([]net.IP, error) {
		switch host {
		case "registry.example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		case "internal.example.com":
			return []net.IP{net.ParseIP("10.0.0.10")}, nil
		default:
			return nil, errors.New("unexpected host")
		}
	}

	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "allow public https", rawURL: "https://registry.example.com/templates.json"},
		{name: "allow public http", rawURL: "http://registry.example.com/templates.json"},
		{name: "reject missing host", rawURL: "https:///templates.json", wantErr: true},
		{name: "reject unsupported scheme", rawURL: "ftp://registry.example.com/templates.json", wantErr: true},
		{name: "reject schemeless", rawURL: "//registry.example.com/templates.json", wantErr: true},
		{name: "reject credentials", rawURL: "https://user:pass@registry.example.com/templates.json", wantErr: true},
		{name: "reject localhost", rawURL: "http://localhost:8080/templates.json", wantErr: true},
		{name: "reject loopback literal", rawURL: "http://127.0.0.1:8080/templates.json", wantErr: true},
		{name: "reject internal resolved host", rawURL: "https://internal.example.com/templates.json", wantErr: true},
		{name: "reject file scheme", rawURL: "file:///etc/passwd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ValidateSafeRemoteURL(context.Background(), tt.rawURL, lookupIP)
			if tt.wantErr {
				require.Error(t, err)
				var unsafeErr *common.UnsafeRemoteURLError
				require.ErrorAs(t, err, &unsafeErr)
				require.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
		})
	}
}

func TestNewSafeOutboundHTTPClient_BlocksUnsafeRedirectTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:8080/private", http.StatusFound)
	}))
	defer server.Close()

	baseClient := newRedirectTestClient(server.Listener.Addr().String())
	lookupIP := func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	client, err := NewSafeOutboundHTTPClient(baseClient, lookupIP)
	require.NoError(t, err)

	_, err = client.Get("http://registry.example.com/redirect")
	require.Error(t, err)
	var unsafeErr *common.UnsafeRemoteURLError
	require.ErrorAs(t, err, &unsafeErr)
}

func newRedirectTestClient(listenerAddr string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{}
		return dialer.DialContext(ctx, network, listenerAddr)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
}
