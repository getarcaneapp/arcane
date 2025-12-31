package generate

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
)

// captureOutput captures stdout produced by fn and returns it as a string.
func captureOutput(fn func() error) (string, error) {
	// Save original
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	// Run function
	runErr := fn()

	// Restore
	_ = w.Close()
	os.Stdout = oldOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), runErr
}

func TestSecretDefaultBase64(t *testing.T) {
	cmd := GenerateCmd
	cmd.SetArgs([]string{"secret"})

	out, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	// should not include emoji characters used previously
	if strings.Contains(out, "📋") || strings.Contains(out, "🐳") || strings.Contains(out, "🔢") || strings.Contains(out, "⚠️") {
		t.Fatalf("output contains emoji/box characters: %q", out)
	}

	if !strings.Contains(out, "BASE64") {
		t.Fatalf("expected BASE64 header in output, got: %q", out)
	}

	// find ENCRYPTION_KEY= and JWT_SECRET= lines and ensure they are valid base64
	var encVal, jwtVal string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ENCRYPTION_KEY=") {
			encVal = strings.TrimPrefix(line, "ENCRYPTION_KEY=")
		}
		if strings.HasPrefix(line, "JWT_SECRET=") {
			jwtVal = strings.TrimPrefix(line, "JWT_SECRET=")
		}
	}
	if encVal == "" || jwtVal == "" {
		t.Fatalf("missing keys in output: enc=%q jwt=%q out=%q", encVal, jwtVal, out)
	}

	if b, err := base64.StdEncoding.DecodeString(encVal); err != nil {
		t.Fatalf("ENCRYPTION_KEY is not valid base64: %v (value=%q)", err, encVal)
	} else if len(b) != 32 {
		t.Fatalf("ENCRYPTION_KEY decoded length != 32 bytes: %d", len(b))
	}
	if b, err := base64.StdEncoding.DecodeString(jwtVal); err != nil {
		t.Fatalf("JWT_SECRET is not valid base64: %v (value=%q)", err, jwtVal)
	} else if len(b) != 32 {
		t.Fatalf("JWT_SECRET decoded length != 32 bytes: %d", len(b))
	}
}

func TestSecretAllFormatContainsSections(t *testing.T) {
	cmd := GenerateCmd
	cmd.SetArgs([]string{"secret", "-f", "all"})

	out, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	if strings.Contains(out, "📋") || strings.Contains(out, "🐳") || strings.Contains(out, "🔢") || strings.Contains(out, "⚠️") {
		t.Fatalf("output contains emoji/box characters: %q", out)
	}

	// verify presence of expected section headers
	mustContain := []string{
		"ENV (.env) - recommended",
		"Docker Compose (environment block)",
		"HEX",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Fatalf("expected section %q not found in output:\n%s", s, out)
		}
	}

	// verify hex values decode to 32 bytes
	var hexEnc, hexJwt string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ENCRYPTION_KEY=") {
			v := strings.TrimPrefix(line, "ENCRYPTION_KEY=")
			// prefer hex if line length looks like hex (64 chars)
			if len(v) >= 64 {
				hexEnc = v
				continue
			}
		}
		if strings.HasPrefix(line, "JWT_SECRET=") {
			v := strings.TrimPrefix(line, "JWT_SECRET=")
			if len(v) >= 64 {
				hexJwt = v
				continue
			}
		}
	}
	if hexEnc == "" || hexJwt == "" {
		t.Fatalf("hex values not found in output (enc=%q jwt=%q) output:\n%s", hexEnc, hexJwt, out)
	}
	if b, err := hex.DecodeString(strings.TrimSpace(hexEnc)); err != nil {
		t.Fatalf("ENCRYPTION_KEY hex decode failed: %v", err)
	} else if len(b) != 32 {
		t.Fatalf("ENCRYPTION_KEY hex decoded length != 32: %d", len(b))
	}
	if b, err := hex.DecodeString(strings.TrimSpace(hexJwt)); err != nil {
		t.Fatalf("JWT_SECRET hex decode failed: %v", err)
	} else if len(b) != 32 {
		t.Fatalf("JWT_SECRET hex decoded length != 32: %d", len(b))
	}
}
