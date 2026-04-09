package integrationtest

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	clipkg "github.com/getarcaneapp/arcane/cli/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var stdoutCaptureMuInternal sync.Mutex

func writeCLIIntegrationConfigInternal(t *testing.T, serverURL string) string {
	t.Helper()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "arcanecli.yml")
	configContent := strings.Join([]string{
		"server_url: " + serverURL,
		"api_key: arc_test_key",
		"default_environment: \"0\"",
		"log_level: info",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return configPath
}

func executeCLIIntegrationCommandInternal(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	root := clipkg.RootCommand()
	resetCommandFlagsInternal(t, root)
	errOut := &strings.Builder{}
	root.SetErr(errOut)
	root.SetArgs(args)

	stdoutCaptureMuInternal.Lock()
	defer stdoutCaptureMuInternal.Unlock()

	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		_ = writer.Close()
		os.Stdout = stdout
	}()

	runErr := root.Execute()

	_ = writer.Close()
	stdoutBytes, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}

	outBuf := string(stdoutBytes)
	if runtime.GOOS == "windows" {
		outBuf = strings.ReplaceAll(outBuf, "\r\n", "\n")
	}

	return outBuf, errOut.String(), runErr
}

func resetCommandFlagsInternal(t *testing.T, root *cobra.Command) {
	t.Helper()

	if root == nil {
		return
	}

	resetFlagSetInternal(t, root.PersistentFlags())
	resetFlagSetInternal(t, root.Flags())
	for _, cmd := range root.Commands() {
		resetCommandFlagsInternal(t, cmd)
	}
}

func resetFlagSetInternal(t *testing.T, flagSet *pflag.FlagSet) {
	t.Helper()

	if flagSet == nil {
		return
	}

	flagSet.VisitAll(func(flag *pflag.Flag) {
		defValue := flag.DefValue
		switch flag.Value.Type() {
		case "stringSlice", "stringArray":
			if defValue == "[]" {
				defValue = ""
			}
		}

		if err := flag.Value.Set(defValue); err != nil {
			t.Fatalf("reset flag %s: %v", flag.Name, err)
		}
		flag.Changed = false
	})
}
