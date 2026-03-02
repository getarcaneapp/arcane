package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	clitypes "github.com/getarcaneapp/arcane/cli/internal/types"
)

func BenchmarkClientListMediumPayload(b *testing.B) {
	payload := make(map[string]any, 0)
	items := make([]map[string]any, 0, 200)
	for i := range 200 {
		items = append(items, map[string]any{
			"id":    fmt.Sprintf("id-%d", i),
			"name":  fmt.Sprintf("name-%d", i),
			"state": "running",
		})
	}
	payload["success"] = true
	payload["data"] = items
	payload["pagination"] = map[string]any{
		"currentPage":  1,
		"itemsPerPage": 200,
		"totalItems":   200,
		"totalPages":   1,
	}
	raw, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	cfg := &clitypes.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := client.New(cfg)
	if err != nil {
		b.Fatalf("new client: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := c.Get(context.Background(), "/api/environments/0/containers?limit=200")
		if err != nil {
			b.Fatalf("request: %v", err)
		}
		_ = resp.Body.Close()
	}
}
