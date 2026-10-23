package testutil

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/pkg"
)

var stdoutCaptureMu sync.Mutex

// WriteConfig writes isolated configuration for command tests.
func WriteConfig(t *testing.T, serverURL string) string {
	t.Helper()

	tempDir := t.TempDir()
	// Keep commands away from the developer's ~/.arcanecli/auth.json.
	t.Setenv("HOME", tempDir)
	t.Setenv("USERPROFILE", tempDir)
	configPath := filepath.Join(tempDir, "arcanecli.yml")
	configContent := strings.Join([]string{
		"server_url: " + serverURL,
		"api_key: arc_test_key",
		"log_level: info",
		"",
	}, "\n")
	{
		err := os.WriteFile(configPath, []byte(configContent), 0o600)
		require.NoError(t, err,
			"write config: %v", err)
	}

	return configPath
}

// Execute runs a freshly built root command and captures its output.
func Execute(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	root := cli.NewRootCommand()
	errOut := &strings.Builder{}
	root.SetErr(errOut)
	root.SetArgs(args)

	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()

	stdout := os.Stdout
	reader, writer, err := os.Pipe()

	require.NoError(t, err,
		"pipe: %v", err)

	os.Stdout = writer
	defer func() {
		_ = writer.Close()
		os.Stdout = stdout
	}()

	runErr := root.Execute()

	_ = writer.Close()
	stdoutBytes, readErr := io.ReadAll(reader)

	require.NoError(t, readErr,
		"read stdout: %v", readErr)

	outBuf := string(stdoutBytes)
	if runtime.GOOS == "windows" {
		outBuf = strings.ReplaceAll(outBuf, "\r\n", "\n")
	}

	return outBuf, errOut.String(), runErr
}
