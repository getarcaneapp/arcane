package config

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func TestApplyConfigSetArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &types.Config{}

	err := applyConfigSetArgs(t.Context(), cfg, []string{
		"server-url", "http://localhost:3552",
		"api-key", "arc_test_12345678",
		"previews.allEnvironments", "true",
		"previews.newSelfUpdater", "true",
		"default-limit", "25",
		"pagination.resources.images.limit", "100",
		"resource-limit.volumes", "40",
	}, nil, io.Discard)

	require.NoError(t, err,
		"applyConfigSetArgs() error = %v", err)

	require.Equal(t, "http://localhost:3552", cfg.ServerURL,
		"ServerURL = %q, want %q", cfg.ServerURL, "http://localhost:3552")

	require.Equal(t, "arc_test_12345678", cfg.APIKey,
		"APIKey = %q, want %q", cfg.APIKey, "arc_test_12345678")

	require.Empty(t, cfg.JWTToken,
		"JWTToken = %q, want empty", cfg.JWTToken)

	require.True(t, cfg.Previews.AllEnvironments)
	require.True(t, cfg.Previews.NewSelfUpdater)
	for _, value := range []string{"false", "true", "invalid"} {
		setErr := applyConfigSetArg(t.Context(), cfg, "previews.allEnvironments", value, io.Discard)
		if value == "invalid" {
			require.Error(t, setErr)
			continue
		}
		require.NoError(t, setErr)
		require.Equal(t, value == "true", cfg.Previews.AllEnvironments)
	}
	require.True(t, cfg.Previews.NewSelfUpdater)
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

	path := filepath.Join(home, "config.yml")
	require.NoError(t, config.SetConfigPath(path))
	t.Cleanup(func() { require.NoError(t, config.SetConfigPath("")) })
	for _, args := range [][]string{
		{"jwt-token", "new_token", "server-url", "https://b.test"},
		{"server-url", "https://b.test", "jwt-token", "new_token"},
		{"jwt-token", "new_token", "--server-url", "https://b.test"},
		{"server-url", "https://b.test", "--jwt-token", "new_token"},
		{"jwt-token", "new_token", "server-url", "https://c.test", "--server-url", "https://b.test"},
	} {
		require.NoError(t, config.SaveTokens(t.Context(), &types.Config{ServerURL: "https://b.test", JWTToken: "saved_b"}))
		require.NoError(t, config.SaveTokens(t.Context(), &types.Config{ServerURL: "https://a.test", JWTToken: "saved_a"}))
		cmd := newConfigSetCmd()
		cmd.SetArgs(args)
		require.NoError(t, cmd.ExecuteContext(t.Context()))
		require.NoError(t, config.SetConfigPath(path))
		loaded, loadErr := config.Load()
		require.NoError(t, loadErr)
		require.Equal(t, "https://b.test", loaded.ServerURL)
		require.Equal(t, "new_token", loaded.JWTToken)
		require.NoError(t, config.SwitchServer(t.Context(), loaded, "https://a.test"))
		require.Equal(t, "saved_a", loaded.JWTToken)
	}
}

func TestNewSelfUpdaterPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yml")
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, config.SetConfigPath(path))
	t.Cleanup(func() { require.NoError(t, config.SetConfigPath("")) })
	var stdout bytes.Buffer
	output.Configure(&stdout, io.Discard, false, true, "")
	t.Cleanup(func() { output.Configure(nil, nil, true, false, "") })
	require.NoError(t, config.Save(t.Context(), &types.Config{Previews: types.PreviewsConfig{AllEnvironments: true}}))
	for _, value := range []string{"true", "false"} {
		stdout.Reset()
		cmd := newConfigSetCmd()
		cmd.SetArgs([]string{"previews.newSelfUpdater", value})
		require.NoError(t, cmd.ExecuteContext(t.Context()))
		require.Contains(t, stdout.String(), "Set previews.newSelfUpdater = "+value)
		require.NoError(t, config.SetConfigPath(path))
		loaded, err := config.Load()
		require.NoError(t, err)
		require.Equal(t, value == "true", loaded.Previews.NewSelfUpdater)
		require.True(t, loaded.Previews.AllEnvironments)
		stdout.Reset()
		require.NoError(t, newConfigShowCmd().ExecuteContext(t.Context()))
		require.Regexp(t, `(?m)^\s*newSelfUpdater\s+`+value+`\s*$`, stdout.String())
	}
	cfg, err := config.Load()
	require.NoError(t, err)
	require.ErrorContains(t, applyConfigSetArg(t.Context(), cfg, "previews.newSelfUpdater", "invalid", io.Discard), "invalid previews.newSelfUpdater")
	require.False(t, cfg.Previews.NewSelfUpdater)
	require.True(t, cfg.Previews.AllEnvironments)
	require.Contains(t, newConfigSetCmd().Long, "arcane config set previews.newSelfUpdater true")
}

func TestApplyConfigSetArgs_OddArgs(t *testing.T) {
	cfg := &types.Config{}
	err := applyConfigSetArgs(t.Context(), cfg, []string{"server-url", "http://localhost:3552", "api-key"}, nil, io.Discard)

	require.Error(t, err,
		"expected error for odd key/value args, got nil")
}

func TestApplyConfigSetArg_UnknownKey(t *testing.T) {
	cfg := &types.Config{}
	err := applyConfigSetArg(t.Context(), cfg, "not-a-real-key", "value", io.Discard)

	require.Error(t, err,
		"expected unknown key error, got nil")

	require.Contains(t, err.Error(), "unknown config key",
		"unexpected error: %v", err)
}

func TestApplyConfigSetArg_ResourceLimitPair(t *testing.T) {
	cfg := &types.Config{}
	err := applyConfigSetArg(t.Context(), cfg, "resource-limit", "containers=55", io.Discard)

	require.NoError(t, err,
		"applyConfigSetArg() error = %v", err)

	{

		got := cfg.LimitFor("containers")
		require.Equal(t, 55, got,
			"LimitFor(containers) = %d, want 55", got)
	}
}
