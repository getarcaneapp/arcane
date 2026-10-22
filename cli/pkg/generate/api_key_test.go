package generate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/pkg/generate"
)

func TestAPIKeyDefaultOutput(t *testing.T) {
	cmd := generate.Command()
	cmd.SetArgs([]string{"api-key"})

	out, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	require.NotEmpty(t, strings.TrimSpace(out),
		"expected raw arc_ key in output, got: %q", out)

	require.True(t, strings.HasPrefix(strings.TrimSpace(out), "arc_"),
		"expected raw arc_ key in output, got: %q", out)

	require.NotContains(t, out, "ARCANE_ADMIN_STATIC_API_KEY",
		"expected raw arc_ key in output, got: %q", out)
}

func TestGenerateAPIKeyProducesArcanePrefix(t *testing.T) {
	apiKey, err := generate.GenerateAPIKey()

	require.NoError(t, err,
		"GenerateAPIKey failed: %v", err)

	require.True(t, strings.HasPrefix(apiKey, "arc_"),
		"expected arc_ prefix, got %q", apiKey)

	require.Len(t, apiKey, 68,
		"expected 68-character key, got %d (%q)", len(apiKey), apiKey)
}
