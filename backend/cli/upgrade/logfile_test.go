package upgrade

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupMessageOnlyLogFile(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	logFile, err := SetupMessageOnlyLogFile(t.TempDir(), "upgrade-test", slog.LevelInfo)

	require.NoError(t, err,
		"SetupMessageOnlyLogFile() error = %v", err)

	// Each entry is written atomically as it is logged, so the file is readable
	// in full without any flush or close.
	slog.Info("container updated", "container", "web")

	content, err := os.ReadFile(logFile.Name())

	require.NoError(t, err,
		"read log file: %v", err)

	got := string(content)

	require.False(t, !strings.Contains(got, "container updated") || !strings.Contains(got, `container="web"`),
		"log file content = %q, want message-only entry with attrs", got)

}
