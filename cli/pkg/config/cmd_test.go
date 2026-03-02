package config

import (
	"strings"
	"testing"

	clitypes "github.com/getarcaneapp/arcane/cli/internal/types"
)

func TestApplyConfigSetArgs(t *testing.T) {
	cfg := &clitypes.Config{}

	changed, err := applyConfigSetArgs(cfg, []string{
		"server-url", "http://localhost:3552",
		"api-key", "arc_test_12345678",
		"default-limit", "25",
		"pagination.resources.images.limit", "100",
		"resource-limit.volumes", "40",
	})
	if err != nil {
		t.Fatalf("applyConfigSetArgs() error = %v", err)
	}
	if !changed {
		t.Fatal("applyConfigSetArgs() changed = false, want true")
	}

	if cfg.ServerURL != "http://localhost:3552" {
		t.Fatalf("ServerURL = %q, want %q", cfg.ServerURL, "http://localhost:3552")
	}
	if cfg.APIKey != "arc_test_12345678" {
		t.Fatalf("APIKey = %q, want %q", cfg.APIKey, "arc_test_12345678")
	}
	if cfg.JWTToken != "" {
		t.Fatalf("JWTToken = %q, want empty", cfg.JWTToken)
	}
	if cfg.Pagination.Default.Limit != 25 {
		t.Fatalf("Pagination.Default.Limit = %d, want 25", cfg.Pagination.Default.Limit)
	}
	if got := cfg.LimitFor("images"); got != 100 {
		t.Fatalf("LimitFor(images) = %d, want 100", got)
	}
	if got := cfg.LimitFor("volumes"); got != 40 {
		t.Fatalf("LimitFor(volumes) = %d, want 40", got)
	}
}

func TestApplyConfigSetArgs_OddArgs(t *testing.T) {
	cfg := &clitypes.Config{}
	changed, err := applyConfigSetArgs(cfg, []string{"server-url", "http://localhost:3552", "api-key"})
	if err == nil {
		t.Fatal("expected error for odd key/value args, got nil")
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
}

func TestApplyConfigSetArg_UnknownKey(t *testing.T) {
	cfg := &clitypes.Config{}
	changed, err := applyConfigSetArg(cfg, "not-a-real-key", "value")
	if err == nil {
		t.Fatal("expected unknown key error, got nil")
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyConfigSetArg_ResourceLimitPair(t *testing.T) {
	cfg := &clitypes.Config{}
	changed, err := applyConfigSetArg(cfg, "resource-limit", "containers=55")
	if err != nil {
		t.Fatalf("applyConfigSetArg() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := cfg.LimitFor("containers"); got != 55 {
		t.Fatalf("LimitFor(containers) = %d, want 55", got)
	}
}
