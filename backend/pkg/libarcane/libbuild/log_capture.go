package libbuild

import (
	"bytes"

	buildtypes "github.com/getarcaneapp/arcane/types/builds"
)

// logCapture stores build output up to a max byte size.
// It implements io.Writer so it can be used with io.MultiWriter.
type logCapture struct {
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func NewLogCapture(maxBytes int) buildtypes.LogCapture {
	return &logCapture{maxBytes: maxBytes}
}

func (l *logCapture) Write(p []byte) (int, error) {
	if l.maxBytes <= 0 {
		return len(p), nil
	}

	remaining := l.maxBytes - l.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			_, _ = l.buf.Write(p)
		} else {
			_, _ = l.buf.Write(p[:remaining])
			l.truncated = true
		}
	} else {
		l.truncated = true
	}

	return len(p), nil
}

func (l *logCapture) String() string {
	return l.buf.String()
}

func (l *logCapture) Truncated() bool {
	return l.truncated
}
