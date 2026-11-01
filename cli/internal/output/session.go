package output

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	kit "kit.getarcane.app"
)

// fileDescriptor supplies a writer's descriptor for terminal detection and sizing.
type fileDescriptor interface {
	Fd() uintptr
}

var (
	sessionMu        sync.RWMutex
	resultWriter     io.Writer
	diagnosticWriter io.Writer
	plainMode        = true
	commandLabel     = "Loading"
)

// Configure binds output to the current command's writers and output mode.
func Configure(stdout, stderr io.Writer, jsonMode, noColor bool, label string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	resultWriter, diagnosticWriter = stdout, stderr
	plainMode = jsonMode
	colorEnabled = !noColor
	commandLabel = label
	setTheme(detectDarkBackground(stdout, noColor))
}

// Stdout returns the current command's result writer.
func Stdout() io.Writer {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	if resultWriter != nil {
		return resultWriter
	}
	return os.Stdout
}

// Stderr returns the current command's diagnostic writer.
func Stderr() io.Writer {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	if diagnosticWriter != nil {
		return diagnosticWriter
	}
	return os.Stderr
}

// humanWriter routes human-oriented output to stderr when stdout is reserved for JSON.
func humanWriter() io.Writer {
	sessionMu.RLock()
	jsonMode := plainMode
	sessionMu.RUnlock()
	return kit.Ternary(jsonMode, Stderr(), Stdout())
}

// IsTerminal checks the actual writer, including terminal capability.
func IsTerminal(value any) bool {
	if writer, ok := value.(*colorprofile.Writer); ok {
		value = writer.Forward
	}
	fd, ok := value.(fileDescriptor)
	return ok && os.Getenv("TERM") != "dumb" && term.IsTerminal(fd.Fd())
}

func colorsEnabled() bool {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	_, noColor := os.LookupEnv("NO_COLOR")
	return colorEnabled && !noColor
}

func renderFor(w io.Writer, style lipgloss.Style, value string) string {
	if !colorsEnabled() || !IsTerminal(w) {
		return value
	}
	return style.Render(value)
}

// StartLoading tracks the current command while a request is in progress.
func StartLoading(ctx context.Context) *tracker {
	sessionMu.RLock()
	enabled, label := !plainMode, commandLabel
	sessionMu.RUnlock()
	trackersMu.Lock()
	enabled = enabled && len(activeTrackers) == 0
	trackersMu.Unlock()
	t := StartTracker(ctx, Stderr(), enabled, label)
	t.NextStep()
	return t
}

// CoordinatedWriter clears loading while another writer uses the terminal.
func CoordinatedWriter(w io.Writer) io.Writer { return coordinatedWriter{out: w} }

type coordinatedWriter struct{ out io.Writer }

func (w coordinatedWriter) Write(p []byte) (int, error) {
	defer SuspendProgress()()
	return w.out.Write(p)
}

// WriteError renders the command error once through Fang's error handler.
func WriteError(w io.Writer, err error) {
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(w, "\n  %s\n  %s\n", renderFor(w, headerStyle.Foreground(statusOffline), "Error"), err.Error())
}

func detectDarkBackground(out io.Writer, noColor bool) bool {
	if noColor || !IsTerminal(out) {
		return true
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return true
	}
	if value := os.Getenv("COLORFGBG"); value != "" {
		fields := strings.Split(value, ";")
		if background, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
			return background != 7 && background != 15
		}
	}
	if file, ok := out.(term.File); ok && IsTerminal(os.Stdin) {
		return lipgloss.HasDarkBackground(os.Stdin, file)
	}
	return true
}
