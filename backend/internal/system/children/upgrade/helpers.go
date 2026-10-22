package upgrade

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
)

const (
	upgradeJobLogLimit       = 200
	progressFeedHistoryLimit = 1000
	progressFeedBuffer       = 256
	pullPercentCeiling       = 60
)

type progressFeed struct {
	mu          sync.Mutex
	active      bool
	fromVersion string
	toVersion   string
	percent     int
	history     []types.UpgradeProgressEvent
	subscribers map[chan types.UpgradeProgressEvent]struct{}
}

func newProgressFeed() *progressFeed {
	return &progressFeed{subscribers: map[chan types.UpgradeProgressEvent]struct{}{}}
}

func (f *progressFeed) start(fromVersion, toVersion string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active, f.fromVersion, f.toVersion, f.percent, f.history = true, fromVersion, toVersion, 0, nil
}

func (f *progressFeed) emit(eventType string, percent int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.active {
		return
	}
	f.broadcastLocked(eventType, percent, message)
}

func (f *progressFeed) finish(message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.active {
		return
	}
	f.broadcastLocked(types.UpgradeProgressEventDone, f.percent, message)
	f.active = false
	for ch := range f.subscribers {
		close(ch)
		delete(f.subscribers, ch)
	}
}

func (f *progressFeed) broadcastLocked(eventType string, percent int, message string) {
	f.percent = max(f.percent, percent)
	event := types.UpgradeProgressEvent{
		Type:        eventType,
		Percent:     f.percent,
		Message:     message,
		FromVersion: f.fromVersion,
		ToVersion:   f.toVersion,
		At:          time.Now(),
	}
	f.history = append(f.history, event)
	if len(f.history) > progressFeedHistoryLimit {
		f.history = slices.Clone(f.history[len(f.history)-progressFeedHistoryLimit:])
	}
	for ch := range f.subscribers {
		select {
		case ch <- event:
		default:
			close(ch)
			delete(f.subscribers, ch)
		}
	}
}

func (f *progressFeed) subscribe() ([]types.UpgradeProgressEvent, <-chan types.UpgradeProgressEvent, bool, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan types.UpgradeProgressEvent, progressFeedBuffer)
	f.subscribers[ch] = struct{}{}
	history := make([]types.UpgradeProgressEvent, len(f.history))
	for i, event := range f.history {
		history[i] = types.UpgradeProgressEvent{
			Type:        event.Type,
			Percent:     event.Percent,
			Message:     event.Message,
			FromVersion: event.FromVersion,
			ToVersion:   event.ToVersion,
			At:          event.At,
			Replay:      true,
		}
	}
	return history, ch, f.active, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.subscribers[ch]; ok {
			delete(f.subscribers, ch)
			close(ch)
		}
	}
}

func parseUpgraderLine(line string) (string, int, string) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "PROGRESS:")
	if !ok {
		return types.UpgradeProgressEventLog, 0, line
	}
	value, message, found := strings.Cut(rest, ":")
	percent, err := strconv.Atoi(value)
	if !found || err != nil {
		return types.UpgradeProgressEventLog, 0, line
	}
	return types.UpgradeProgressEventProgress, min(max(percent, 0), 100), message
}

func trackPullLine(layers map[string]bool, line string) int {
	id, status, found := strings.Cut(line, ": ")
	if found && id != "" && !strings.ContainsAny(id, " \t") {
		switch {
		case strings.HasPrefix(status, "Pull complete"), strings.HasPrefix(status, "Already exists"):
			layers[id] = true
		case strings.HasPrefix(status, "Pulling fs layer"), strings.HasPrefix(status, "Waiting"):
			if _, seen := layers[id]; !seen {
				layers[id] = false
			}
		}
	}
	if len(layers) == 0 {
		return 0
	}
	done := 0
	for _, complete := range layers {
		if complete {
			done++
		}
	}
	return done * pullPercentCeiling / len(layers)
}

func appendLogs(logs []string, lines ...string) []string {
	combined := append(slices.Clone(logs), lines...)
	return combined[max(len(combined)-upgradeJobLogLimit, 0):]
}

func readUpgradeLog(dir string, since time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read upgrade log directory: %w", err)
	}
	newest, newestAt := "", since
	for _, entry := range entries {
		if !upgradeLogName.MatchString(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.ModTime().Before(newestAt) {
			continue
		}
		newest, newestAt = entry.Name(), info.ModTime()
	}
	if newest == "" {
		return nil, nil
	}
	content, err := os.ReadFile(filepath.Join(dir, newest))
	if err != nil {
		return nil, fmt.Errorf("read upgrade log %s: %w", newest, err)
	}
	return slices.DeleteFunc(strings.Split(string(content), "\n"), func(line string) bool {
		return strings.TrimSpace(line) == ""
	}), nil
}

func readProgressStream(r io.Reader, deliver func(types.UpgradeProgressEvent) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		payload, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		var event types.UpgradeProgressEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &event); err != nil {
			return fmt.Errorf("decode upgrade progress event: %w", err)
		}
		if !deliver(event) {
			return nil
		}
	}
	return scanner.Err()
}

func seedUpgradeResults(requested []string, manager UpgradeJobResult, remotes []remoteAgent) (UpgradeJobResults, error) {
	wanted := func(id string) bool {
		return len(requested) == 0 || slices.Contains(requested, id)
	}
	results := make(UpgradeJobResults, 0, len(remotes)+1)
	if wanted(environment.LocalEnvironmentID) {
		results = append(results, manager)
	}
	for _, remote := range remotes {
		if wanted(remote.ID) {
			results = append(results, UpgradeJobResult{
				EnvironmentID:   remote.ID,
				EnvironmentName: remote.Name,
				Status:          UpgradeResultStatusPending,
			})
		}
	}
	unknown := slices.DeleteFunc(slices.Clone(requested), func(id string) bool {
		return slices.ContainsFunc(results, func(r UpgradeJobResult) bool {
			return r.EnvironmentID == id
		})
	})
	if len(unknown) > 0 {
		return nil, common.Classify(common.ErrValidation, fmt.Errorf("unknown or disabled environments: %s", strings.Join(unknown, ", ")))
	}
	if len(results) == 0 {
		return nil, common.Classify(common.ErrValidation, errors.New("no environments to upgrade"))
	}
	return results, nil
}
