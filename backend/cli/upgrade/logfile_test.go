package upgrade

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSetupMessageOnlyLogFile(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	logFile, err := SetupMessageOnlyLogFile(t.TempDir(), "upgrade-test", slog.LevelInfo)
	if err != nil {
		t.Fatalf("SetupMessageOnlyLogFile() error = %v", err)
	}

	// Each entry is written atomically as it is logged, so the file is readable
	// in full without any flush or close.
	slog.Info("container updated", "container", "web")

	content, err := os.ReadFile(logFile.Name())
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "container updated") || !strings.Contains(got, `container="web"`) {
		t.Fatalf("log file content = %q, want message-only entry with attrs", got)
	}
}
