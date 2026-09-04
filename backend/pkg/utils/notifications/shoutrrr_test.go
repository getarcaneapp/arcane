package notifications

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendGotifyDeliveryInternal(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- string(body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"id":1,"error":"delivery failed"}`))
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)
			err = SendGotify(t.Context(), GotifyConfig{Host: endpoint.Host, Token: "A12345678901234", DisableTLS: true}, "consolidated delivery")
			if status == http.StatusOK {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "failed to send Gotify message via shoutrrr")
			}
			select {
			case body := <-requests:
				require.Contains(t, body, "consolidated delivery")
			default:
				t.Fatal("notification was not delivered to the HTTP server")
			}
		})
	}
}

func TestSendShoutrrrConstructionErrorInternal(t *testing.T) {
	err := sendShoutrrrInternal("Slack", "unsupported-provider://localhost", "message", nil)
	require.ErrorContains(t, err, "failed to create shoutrrr Slack sender")
}
