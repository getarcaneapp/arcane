package generate_test

import (
	"strings"
	"testing"

	gen "github.com/getarcaneapp/arcane/cli/pkg/generate"
)

func TestAPIKeyDefaultOutput(t *testing.T) {
	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"api-key"})

	out, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected raw arc_ key in output, got: %q", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "arc_") {
		t.Fatalf("expected raw arc_ key in output, got: %q", out)
	}
	if strings.Contains(out, "ADMIN_STATIC_API_KEY") {
		t.Fatalf("expected raw arc_ key in output, got: %q", out)
	}
}

func TestGenerateAPIKeyProducesArcanePrefix(t *testing.T) {
	apiKey, err := gen.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}

	if !strings.HasPrefix(apiKey, "arc_") {
		t.Fatalf("expected arc_ prefix, got %q", apiKey)
	}
	if len(apiKey) != 68 {
		t.Fatalf("expected 68-character key, got %d (%q)", len(apiKey), apiKey)
	}
}
