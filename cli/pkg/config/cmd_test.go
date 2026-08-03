package config

import (
	"testing"

	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, err,
		"applyConfigSetArgs() error = %v", err)

	require.True(t, changed,
		"applyConfigSetArgs() changed = false, want true")

	require.Equal(t, "http://localhost:3552", cfg.ServerURL,
		"ServerURL = %q, want %q", cfg.ServerURL, "http://localhost:3552")

	require.Equal(t, "arc_test_12345678", cfg.APIKey,
		"APIKey = %q, want %q", cfg.APIKey, "arc_test_12345678")

	require.Empty(t, cfg.JWTToken,
		"JWTToken = %q, want empty", cfg.JWTToken)

	require.Equal(t, 25, cfg.Pagination.Default.Limit,
		"Pagination.Default.Limit = %d, want 25", cfg.Pagination.Default.Limit)
	{

		got := cfg.LimitFor("images")
		require.Equal(t, 100, got,
			"LimitFor(images) = %d, want 100", got)
	}
	{

		got := cfg.LimitFor("volumes")
		require.Equal(t, 40, got,
			"LimitFor(volumes) = %d, want 40", got)
	}

}

func TestApplyConfigSetArgs_OddArgs(t *testing.T) {
	cfg := &clitypes.Config{}
	changed, err := applyConfigSetArgs(cfg, []string{"server-url", "http://localhost:3552", "api-key"})

	require.Error(t, err,
		"expected error for odd key/value args, got nil")

	require.False(t, changed,
		"changed = true, want false")

}

func TestApplyConfigSetArg_UnknownKey(t *testing.T) {
	cfg := &clitypes.Config{}
	changed, err := applyConfigSetArg(cfg, "not-a-real-key", "value")

	require.Error(t, err,
		"expected unknown key error, got nil")

	require.False(t, changed,
		"changed = true, want false")

	require.Contains(t, err.Error(), "unknown config key",
		"unexpected error: %v", err)

}

func TestApplyConfigSetArg_ResourceLimitPair(t *testing.T) {
	cfg := &clitypes.Config{}
	changed, err := applyConfigSetArg(cfg, "resource-limit", "containers=55")

	require.NoError(t, err,
		"applyConfigSetArg() error = %v", err)

	require.True(t, changed,
		"changed = false, want true")
	{

		got := cfg.LimitFor("containers")
		require.Equal(t, 55, got,
			"LimitFor(containers) = %d, want 55", got)
	}

}
