package upgrade

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

func TestParseUpgraderLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantType    string
		wantPercent int
		wantMessage string
	}{
		{name: "progress marker", line: "PROGRESS:65:Renaming old container", wantType: types.UpgradeProgressEventProgress, wantPercent: 65, wantMessage: "Renaming old container"},
		{name: "message keeps colons", line: "PROGRESS:95:Upgrade complete: ok", wantType: types.UpgradeProgressEventProgress, wantPercent: 95, wantMessage: "Upgrade complete: ok"},
		{name: "clamped percent", line: "PROGRESS:150:Done", wantType: types.UpgradeProgressEventProgress, wantPercent: 100, wantMessage: "Done"},
		{name: "plain log", line: `level=INFO msg="Stopping old container"`, wantType: types.UpgradeProgressEventLog, wantMessage: `level=INFO msg="Stopping old container"`},
		{name: "malformed marker", line: "PROGRESS:abc:oops", wantType: types.UpgradeProgressEventLog, wantMessage: "PROGRESS:abc:oops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventType, percent, message := parseUpgraderLine(tt.line)
			require.Equal(t, tt.wantType, eventType)
			require.Equal(t, tt.wantPercent, percent)
			require.Equal(t, tt.wantMessage, message)
		})
	}
}

func TestReadUpgradeLog(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name, content string, modTime time.Time) {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.NoError(t, os.Chtimes(path, modTime, modTime))
	}
	write("arcane-upgrade-100.log", "old run\n", now.Add(-2*time.Hour))
	write("arcane-upgrade-200.log", "Pulling image\n\nUpgrade complete\n", now.Add(-time.Minute))
	write("notes.log", "ignored\n", now)

	lines, err := readUpgradeLog(dir, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, []string{"Pulling image", "Upgrade complete"}, lines)

	lines, err = readUpgradeLog(dir, now.Add(time.Hour))
	require.NoError(t, err)
	require.Empty(t, lines)

	lines, err = readUpgradeLog(filepath.Join(dir, "missing"), time.Time{})
	require.NoError(t, err)
	require.Empty(t, lines)
}

func TestProgressFeedRetainsFinishedRun(t *testing.T) {
	feed := newProgressFeed()
	feed.start("v1.0.0", "v1.1.0")
	feed.emit(types.UpgradeProgressEventStage, 0, string(UpgradeStagePulling))
	feed.finish("pull upgrader image: denied")

	history, _, active, unsubscribe := feed.subscribe()
	defer unsubscribe()
	require.False(t, active)
	require.Len(t, history, 2)
	require.Equal(t, types.UpgradeProgressEventDone, history[1].Type)
	require.Equal(t, "pull upgrader image: denied", history[1].Message)
	require.True(t, history[0].Replay)
	require.True(t, history[1].Replay)

	feed.start("v1.0.0", "v1.1.0")
	history, _, active, unsubscribe = feed.subscribe()
	defer unsubscribe()
	require.True(t, active)
	require.Empty(t, history)
}
