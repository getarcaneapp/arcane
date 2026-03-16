package docker

import (
	"strings"
	"testing"
)

func TestConsumeJSONMessageStream(t *testing.T) {
	t.Run("streams lines and succeeds without daemon error", func(t *testing.T) {
		stream := strings.NewReader(`{"status":"Pulling fs layer","id":"layer1"}` + "\n")
		seen := 0

		err := ConsumeJSONMessageStream(stream, func(_ []byte) error {
			seen++
			return nil
		})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if seen != 1 {
			t.Fatalf("expected line handler to be called once, got %d", seen)
		}
	})

	t.Run("returns structured errorDetail from daemon", func(t *testing.T) {
		stream := strings.NewReader(`{"errorDetail":{"code":401,"message":"unauthorized"}}` + "\n")
		err := ConsumeJSONMessageStream(stream, nil)
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			t.Fatalf("expected unauthorized error, got %v", err)
		}
	})

	t.Run("returns legacy top-level error string", func(t *testing.T) {
		stream := strings.NewReader(`{"error":"manifest unknown"}` + "\n")
		err := ConsumeJSONMessageStream(stream, nil)
		if err == nil || !strings.Contains(err.Error(), "manifest unknown") {
			t.Fatalf("expected manifest unknown error, got %v", err)
		}
	})
}
