package handlers

import (
	json "encoding/json/v2"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/dockerinfo"
	"github.com/moby/moby/api/types/system"
)

func TestInfoMarshalsEmbeddedDockerFieldsAtTopLevel(t *testing.T) {
	data, err := json.Marshal(dockerinfo.Info{
		Info: system.Info{
			Name:              "arcane-host",
			NCPU:              8,
			MemTotal:          16 * 1024 * 1024 * 1024,
			ContainersRunning: 3,
		},
		Success:    true,
		APIVersion: "1.55",
	})
	if err != nil {
		t.Fatalf("marshal Docker info: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal Docker info payload: %v", err)
	}

	for name, want := range map[string]any{
		"Name":              "arcane-host",
		"NCPU":              float64(8),
		"ContainersRunning": float64(3),
		"apiVersion":        "1.55",
	} {
		if got := payload[name]; got != want {
			t.Fatalf("payload[%q] = %#v, want %#v; payload: %s", name, got, want, data)
		}
	}
	if _, nested := payload["Info"]; nested {
		t.Fatalf("Docker fields were nested under Info: %s", data)
	}
}
