package docker

import (
	"bytes"
	json "encoding/json/v2"
	"io"
	"strings"
	"sync"
)

// ProgressWriterKey can be set on a context to stream operation output to the
// client. The value must be an io.Writer (typically an activity writer wrapping
// the HTTP response writer).
type ProgressWriterKey struct{}

type flusher interface{ Flush() }

// NewLogLineWriter returns a writer that splits everything written to it on
// newlines and emits each line as a {"log":"<line>"} NDJSON frame to dst,
// flushing after each frame when dst supports it. Close flushes any trailing
// partial line as a final frame. Writers such as compose's plain progress
// renderer and jsonmessage.DisplayJSONMessagesStream write raw CLI text into
// this to put docker-CLI-parity output on the wire.
func NewLogLineWriter(dst io.Writer) io.WriteCloser {
	if dst == nil || dst == io.Discard {
		return nopWriteCloser{io.Discard}
	}
	return &logLineWriter{dst: dst}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

type logLineWriter struct {
	dst io.Writer

	// Writers such as compose's plain progress renderer write from multiple
	// goroutines concurrently; the line buffer must be guarded.
	mu     sync.Mutex
	buffer []byte
}

func (w *logLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer = append(w.buffer, p...)
	for {
		idx := bytes.IndexByte(w.buffer, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buffer[:idx])
		w.buffer = w.buffer[idx+1:]
		w.emitLine(line)
	}
	return len(p), nil
}

func (w *logLineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buffer) > 0 {
		w.emitLine(string(w.buffer))
		w.buffer = nil
	}
	return nil
}

func (w *logLineWriter) emitLine(line string) {
	line = strings.TrimSuffix(line, "\r")
	frame, err := json.Marshal(map[string]string{"log": line})
	if err != nil {
		return
	}
	_, _ = w.dst.Write(append(frame, '\n'))
	if f, ok := w.dst.(flusher); ok {
		f.Flush()
	}
}
