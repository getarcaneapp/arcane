package generate_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	gen "github.com/getarcaneapp/arcane/cli/v2/pkg/generate"
	"github.com/stretchr/testify/require"
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
	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"secret"})

	out, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	// should not include emoji characters used previously

	require.False(t, strings.Contains(out, "📋") || strings.Contains(out, "🐳") || strings.Contains(out, "🔢") || strings.Contains(out, "⚠️"),
		"output contains emoji/box characters: %q", out)

	require.Contains(t, out, "BASE64",
		"expected BASE64 header in output, got: %q", out)

	// find ENCRYPTION_KEY= and JWT_SECRET= lines and ensure they are valid base64
	var encVal, jwtVal string
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(line, "ENCRYPTION_KEY="); ok {
			encVal = after
		}
		if after, ok := strings.CutPrefix(line, "JWT_SECRET="); ok {
			jwtVal = after
		}
	}

	require.False(t, encVal == "" || jwtVal == "",
		"missing keys in output: enc=%q jwt=%q out=%q", encVal, jwtVal, out)

	if b, err := base64.StdEncoding.DecodeString(encVal); err != nil {
		require.FailNowf(t, "unexpected failure", "ENCRYPTION_KEY is not valid base64: %v (value=%q)", err, encVal)
	} else if len(b) != 32 {
		require.Len(t, b, 32,
			"ENCRYPTION_KEY decoded length != 32 bytes: %d", len(b))
	}
	if b, err := base64.StdEncoding.DecodeString(jwtVal); err != nil {
		require.FailNowf(t, "unexpected failure", "JWT_SECRET is not valid base64: %v (value=%q)", err, jwtVal)
	} else if len(b) != 32 {
		require.Len(t, b, 32,
			"JWT_SECRET decoded length != 32 bytes: %d", len(b))
	}
}

func TestSecretAllFormatContainsSections(t *testing.T) {
	cmd := gen.GenerateCmd
	cmd.SetArgs([]string{"secret", "-f", "all"})

	out, err := captureOutput(func() error {
		_, err := cmd.ExecuteC()
		return err
	})

	require.NoError(t, err,
		"command failed: %v", err)

	require.False(t, strings.Contains(out, "📋") || strings.Contains(out, "🐳") || strings.Contains(out, "🔢") || strings.Contains(out, "⚠️"),
		"output contains emoji/box characters: %q", out)

	// verify presence of expected section headers
	mustContain := []string{
		"ENV (.env) - recommended",
		"Docker Compose (environment block)",
		"HEX",
	}
	for _, s := range mustContain {

		require.Contains(t, out, s,
			"expected section %q not found in output:\n%s", s, out)

	}

	// verify hex values decode to 32 bytes
	var hexEnc, hexJwt string
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(line, "ENCRYPTION_KEY="); ok {
			v := after
			// prefer hex if line length looks like hex (64 chars)
			if len(v) >= 64 {
				hexEnc = v
				continue
			}
		}
		if after, ok := strings.CutPrefix(line, "JWT_SECRET="); ok {
			v := after
			if len(v) >= 64 {
				hexJwt = v
				continue
			}
		}
	}

	require.False(t, hexEnc == "" || hexJwt == "",
		"hex values not found in output (enc=%q jwt=%q) output:\n%s", hexEnc, hexJwt, out)

	if b, err := hex.DecodeString(strings.TrimSpace(hexEnc)); err != nil {
		require.FailNowf(t, "unexpected failure", "ENCRYPTION_KEY hex decode failed: %v", err)
	} else if len(b) != 32 {
		require.Len(t, b, 32,
			"ENCRYPTION_KEY hex decoded length != 32: %d", len(b))
	}
	if b, err := hex.DecodeString(strings.TrimSpace(hexJwt)); err != nil {
		require.FailNowf(t, "unexpected failure", "JWT_SECRET hex decode failed: %v", err)
	} else if len(b) != 32 {
		require.Len(t, b, 32,
			"JWT_SECRET hex decoded length != 32: %d", len(b))
	}
}
