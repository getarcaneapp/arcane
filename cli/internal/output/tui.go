package output

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/docker/go-units"
	"github.com/mattn/go-runewidth"
	kit "go.getarcane.app/kit/pkg"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	trackersMu     sync.Mutex
	activeTrackers = make(map[*tracker]struct{})
	terminalMu     sync.Mutex
)

const (
	stepPending = iota
	stepRunning
	stepSucceeded
	stepFailed
)

type trackerStep struct {
	key, label     string
	status         int
	current, total int64
	bytes          bool
}

type tracker struct {
	out                           io.Writer
	stop, done                    chan struct{}
	resumeOuter                   func()
	steps                         []trackerStep
	current, drawn, frame, paused int
	once                          sync.Once
	mu                            sync.Mutex
	live                          bool
	stopped                       bool
}

// StartTracker renders a checklist only for interactive text output.
func StartTracker(ctx context.Context, out io.Writer, enabled bool, labels ...string) *tracker {
	out = cmp.Or(out, io.Discard)
	t := &tracker{out: out, live: enabled && IsTerminal(out) && IsTerminal(Stdout()) && ctx.Err() == nil, current: -1}
	for _, label := range labels {
		t.steps = append(t.steps, trackerStep{label: label, status: stepPending})
	}
	if !t.live {
		return t
	}
	fd, hasFD := t.out.(fileDescriptor)
	t.stop, t.done = make(chan struct{}), make(chan struct{})
	t.resumeOuter = SuspendProgress()
	trackersMu.Lock()
	activeTrackers[t] = struct{}{}
	trackersMu.Unlock()
	go func() {
		defer close(t.done)
		defer func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.stopped = true
			if t.drawn == 0 {
				return
			}
			terminalMu.Lock()
			defer terminalMu.Unlock()
			_, _ = io.WriteString(t.out, strings.Repeat("\x1b[A\r\x1b[2K", t.drawn))
			t.drawn = 0
		}()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stop:
				return
			case <-ticker.C:
			}
			t.mu.Lock()
			t.frame++
			if t.paused > 0 {
				t.mu.Unlock()
				continue
			}
			width, height := 80, 24
			if hasFD {
				w, h, err := term.GetSize(fd.Fd())
				width = max(kit.Ternary(err == nil, w, width), 1)
				height = max(kit.Ternary(err == nil, h, height), 2)
			}
			var frame strings.Builder
			frame.WriteString(kit.Ternary(t.drawn > 0, fmt.Sprintf("\x1b[%dA", t.drawn), ""))
			start := max(len(t.steps)-max(height-2, 1), 0)
			icons := [...]string{stepPending: "○", stepRunning: spinnerFrames[t.frame%len(spinnerFrames)], stepSucceeded: "✓", stepFailed: "✗"}
			styles := [...]lipgloss.Style{stepPending: statusMutedStyle, stepRunning: infoStyle, stepSucceeded: successStyle, stepFailed: statusOfflineStyle}
			for _, step := range t.steps[start:] {
				line := "  " + renderFor(t.out, styles[step.status], icons[step.status]) + " " + renderFor(t.out, valueStyle, step.label)
				if step.status == stepRunning && step.total > 0 {
					current := min(step.current, step.total)
					var count string
					if step.bytes {
						count = Bytes(current) + "/" + Bytes(step.total)
					} else {
						count = fmt.Sprintf("%d/%d", current, step.total)
					}
					barWidth := min(24, max(width-runewidth.StringWidth(ansiRegexp.ReplaceAllString(step.label, ""))-len(count)-7, 1))
					bar := progress.New(progress.WithWidth(barWidth), progress.WithColors(arcanePurple))
					showColor := colorsEnabled()
					bar.FullColor = kit.Ternary(showColor, bar.FullColor, nil)
					bar.EmptyColor = kit.Ternary(showColor, bar.EmptyColor, nil)
					bar.PercentageStyle = kit.Ternary(showColor, bar.PercentageStyle, tablePlainCell.UnsetPadding())
					line = fmt.Sprintf("  %s %s %s", renderFor(t.out, infoStyle, step.label), bar.ViewAs(float64(current)/float64(step.total)), count)
					if !showColor {
						line = ansi.Strip(line)
					}
				}
				frame.WriteString("\r\x1b[2K")
				frame.WriteString(ansi.Truncate(line, width, "…"))
				frame.WriteByte('\n')
			}
			// Erase rows left over after a terminal resize.
			count := len(t.steps) - start
			if extra := t.drawn - count; extra > 0 {
				frame.WriteString(strings.Repeat("\r\x1b[2K\n", extra))
				_, _ = fmt.Fprintf(&frame, "\x1b[%dA", extra)
			}
			t.drawn = count
			terminalMu.Lock()
			_, _ = io.WriteString(t.out, frame.String())
			terminalMu.Unlock()
			t.mu.Unlock()
		}
	}()
	return t
}

// NextStep completes the active step and starts the next pending step.
func (t *tracker) NextStep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current >= 0 && t.current < len(t.steps) {
		t.steps[t.current].status = stepSucceeded
	}
	t.current++
	if t.current < len(t.steps) {
		t.steps[t.current].status = stepRunning
	}
}

// SetProgress updates a distinct step, retaining interleaved layer totals.
func (t *tracker) SetProgress(key, label string, current, total int64, byteCount bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var index int
	if key == "" && t.current >= 0 && t.current < len(t.steps) {
		index = t.current
	} else {
		index = slices.IndexFunc(t.steps, func(step trackerStep) bool { return step.key == key })
	}
	if index < 0 {
		t.steps = append(t.steps, trackerStep{key: key})
		index = len(t.steps) - 1
	}
	step := &t.steps[index]
	step.label, step.current, step.total, step.bytes = label, max(current, 0), max(total, 0), byteCount
	step.status = stepRunning
}

// Finish marks one tracked step complete or failed.
func (t *tracker) Finish(key string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.steps {
		if (key == "" && i == t.current) || (key != "" && t.steps[i].key == key) {
			t.steps[i].status = kit.Ternary(err != nil, stepFailed, stepSucceeded)
		}
	}
}

// Stop clears loading and waits for the renderer. It is safe to repeat.
func (t *tracker) Stop() {
	if t == nil || !t.live {
		return
	}
	t.once.Do(func() {
		close(t.stop)
		<-t.done
		trackersMu.Lock()
		delete(activeTrackers, t)
		trackersMu.Unlock()
		t.resumeOuter()
	})
}

// SuspendProgress pauses every active tracker until the returned function runs.
func SuspendProgress() func() {
	trackersMu.Lock()
	trackers := make([]*tracker, 0, len(activeTrackers))
	for t := range activeTrackers {
		trackers = append(trackers, t)
	}
	trackersMu.Unlock()
	for _, t := range trackers {
		t.mu.Lock()
		t.paused++
		if t.drawn > 0 {
			terminalMu.Lock()
			_, _ = io.WriteString(t.out, strings.Repeat("\x1b[A\r\x1b[2K", t.drawn))
			t.drawn = 0
			terminalMu.Unlock()
		}
		t.mu.Unlock()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, t := range trackers {
				t.mu.Lock()
				t.paused = max(t.paused-1, 0)
				t.mu.Unlock()
			}
		})
	}
}

// Bytes renders a signed byte count in human-readable form.
func Bytes(value int64) string { return UnsignedBytes(uint64(max(value, 0))) }

// UnsignedBytes renders an unsigned byte count in human-readable form.
func UnsignedBytes(value uint64) string {
	if value < units.KiB {
		return fmt.Sprintf("%d B", value)
	}

	return units.CustomSize("%.1f %s", float64(value), units.KiB, []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"})
}
