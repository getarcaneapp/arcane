package generate

import (
	"strings"
	"testing"
)

func TestAPIKeyCommandAvailableInBackendCLI(t *testing.T) {
	cmd := GenerateCmd
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
