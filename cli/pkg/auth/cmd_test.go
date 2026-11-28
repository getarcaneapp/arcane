package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"

	"getarcane.dev/cli/v3/internal/client"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

func TestPollDeviceToken_ResumesAfterRateLimit(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"token":"tok"}`))
	}))
	defer srv.Close()

	c, err := client.NewUnauthenticated(&clitypes.Config{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	result, err := pollDeviceToken(cmd, c, types.OidcDeviceAuthResponse{DeviceCode: "code", Interval: 1, ExpiresIn: 30})
	if err != nil {
		t.Fatalf("expected polling to resume after 429, got: %v", err)
	}
	if result.Token != "tok" || calls != 2 {
		t.Fatalf("token=%q calls=%d", result.Token, calls)
	}
}
