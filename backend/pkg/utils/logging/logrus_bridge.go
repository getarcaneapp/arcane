package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	logrusBridgeOnce sync.Once

	logrusMirrorMu     sync.Mutex
	logrusMirrorNextID int
	logrusMirrors      = map[int]io.Writer{}
)

// InstallLogrusBridge routes the global logrus logger — used by embedded
// libraries such as Copacetic — through slog so their output follows Arcane's
// log format instead of logrus' own stderr formatting. Safe to call more than
// once.
func InstallLogrusBridge() {
	logrusBridgeOnce.Do(func() {
		logrus.SetOutput(io.Discard)
		logrus.AddHook(logrusSlogHook{})
	})
}

// AddLogrusMirror additionally streams bridged logrus messages (info and
// above) to the given writer, one line per entry, until the returned function
// is called. Used to surface an embedded library's log output in the activity
// feed of the operation that drives it.
func AddLogrusMirror(w io.Writer) func() {
	logrusMirrorMu.Lock()
	logrusMirrorNextID++
	id := logrusMirrorNextID
	logrusMirrors[id] = w
	logrusMirrorMu.Unlock()

	return func() {
		logrusMirrorMu.Lock()
		delete(logrusMirrors, id)
		logrusMirrorMu.Unlock()
	}
}

type logrusSlogHook struct{}

func (logrusSlogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (logrusSlogHook) Fire(entry *logrus.Entry) error {
	level := slog.LevelDebug
	switch entry.Level {
	case logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel:
		level = slog.LevelError
	case logrus.WarnLevel:
		level = slog.LevelWarn
	case logrus.InfoLevel:
		level = slog.LevelInfo
	case logrus.DebugLevel, logrus.TraceLevel:
		level = slog.LevelDebug
	}

	args := make([]any, 0, len(entry.Data)*2)
	for key, value := range entry.Data {
		args = append(args, key, value)
	}

	ctx := entry.Context
	if ctx == nil {
		ctx = context.Background()
	}
	message := strings.TrimSpace(entry.Message)
	slog.Log(ctx, level, message, args...)

	if level >= slog.LevelInfo && message != "" {
		// Snapshot under the lock, write outside it: a writer that logs through
		// logrus itself would otherwise re-enter Fire and deadlock.
		logrusMirrorMu.Lock()
		writers := make([]io.Writer, 0, len(logrusMirrors))
		for _, w := range logrusMirrors {
			writers = append(writers, w)
		}
		logrusMirrorMu.Unlock()

		for _, w := range writers {
			// Best-effort mirror: the activity writer buffers internally and a
			// failed write must never fail or re-enter the logging path.
			_, _ = w.Write([]byte(message + "\n"))
		}
	}
	return nil
}
