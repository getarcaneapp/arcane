package secretsource

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"slices"
	"strings"
	"sync"
)

type (
	// Redactor masks known secret values in text. The zero value and a nil
	// Redactor mask nothing.
	Redactor struct {
		replacer *strings.Replacer
		longest  int
	}

	redactedError struct {
		err     error
		message string
	}

	nopWriteCloser struct{ io.Writer }

	redactingWriter struct {
		mu  sync.Mutex
		r   *Redactor
		w   io.Writer
		buf []byte
	}
)

const (
	// RedactedMarker replaces secret values in output and messages.
	RedactedMarker = "[REDACTED]"

	// minRedactLength is the shortest value that is masked. Shorter values (a
	// port, "true") would mask unrelated text and say little about the secret.
	minRedactLength = 4

	// maxPendingLine bounds how much of an unterminated line is held back.
	maxPendingLine = 64 * 1024
)

var (
	htmlEscaper = strings.NewReplacer("<", `\u003c`, ">", `\u003e`, "&", `\u0026`, "\u2028", `\u2028`, "\u2029", `\u2029`)
)

// NewRedactor builds a redactor for values. Each value is also matched as it
// appears inside a JSON string, because deploy progress is streamed as JSON.
func NewRedactor(values map[string]string) *Redactor {
	seen := make(map[string]struct{})
	var needles []string
	add := func(needle string) {
		if len(needle) < minRedactLength {
			return
		}
		if _, ok := seen[needle]; ok {
			return
		}
		seen[needle] = struct{}{}
		needles = append(needles, needle)
	}
	for _, value := range values {
		add(value)
		if encoded, err := json.Marshal(value); err == nil && len(encoded) >= 2 {
			inner := string(encoded[1 : len(encoded)-1])
			add(inner)
			// encoding/json (v1), which Docker uses, also escapes <, >, and &.
			add(htmlEscaper.Replace(inner))
		}
		// Multi-line values (keys, certificates) can surface one line at a time.
		if strings.Contains(value, "\n") {
			for line := range strings.SplitSeq(value, "\n") {
				add(strings.TrimSpace(line))
			}
		}
	}
	if len(needles) == 0 {
		return nil
	}
	// Longest first, so a value that contains another is masked whole.
	slices.SortFunc(needles, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, len(needles)*2)
	for _, needle := range needles {
		pairs = append(pairs, needle, RedactedMarker)
	}
	return &Redactor{replacer: strings.NewReplacer(pairs...), longest: len(needles[0])}
}

// String masks every known value in text.
func (r *Redactor) String(text string) string {
	if r == nil || text == "" {
		return text
	}
	return r.replacer.Replace(text)
}

// Error returns err with its message masked. errors.Is and errors.As still
// see the original error.
func (r *Redactor) Error(err error) error {
	if r == nil || err == nil {
		return err
	}
	message := err.Error()
	masked := r.String(message)
	if masked == message {
		return err
	}
	return &redactedError{err: err, message: masked}
}

func (e *redactedError) Error() string { return e.message }

// Unwrap keeps the error's identity. Code that prints an unwrapped error
// could see the original text, so callers should print the outer error.
func (e *redactedError) Unwrap() error { return e.err }

// Writer masks values in everything written to w. Output is passed on one
// line at a time so a value split across writes is still caught; Close and
// Flush pass on a final partial line. A nil Redactor returns w unchanged.
func (r *Redactor) Writer(w io.Writer) io.WriteCloser {
	if w == nil {
		return nil
	}
	if r == nil {
		return nopWriteCloser{w}
	}
	return &redactingWriter{r: r, w: w}
}

func (nopWriteCloser) Close() error { return nil }

func (rw *redactingWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.buf = append(rw.buf, p...)
	if end := bytes.LastIndexByte(rw.buf, '\n'); end >= 0 {
		if _, err := io.WriteString(rw.w, rw.r.String(string(rw.buf[:end+1]))); err != nil {
			return 0, err
		}
		rw.buf = append(rw.buf[:0], rw.buf[end+1:]...)
	}
	if len(rw.buf) > maxPendingLine {
		// A long run without a newline: mask it, then hold back only a tail
		// short enough to be the start of a value that is not complete yet.
		masked := rw.r.String(string(rw.buf))
		if cut := len(masked) - (rw.r.longest - 1); cut > 0 {
			if _, err := io.WriteString(rw.w, masked[:cut]); err != nil {
				return 0, err
			}
			rw.buf = append(rw.buf[:0], masked[cut:]...)
		}
	}
	return len(p), nil
}

// Flush passes on any held-back partial line, then flushes w when it can.
func (rw *redactingWriter) Flush() {
	_ = rw.flushInternal()
	if flusher, ok := rw.w.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}

func (rw *redactingWriter) flushInternal() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if len(rw.buf) == 0 {
		return nil
	}
	_, err := io.WriteString(rw.w, rw.r.String(string(rw.buf)))
	rw.buf = rw.buf[:0]
	return err
}

func (rw *redactingWriter) Close() error { return rw.flushInternal() }

// Redactor returns a redactor for the deploy's secret values.
func (e DeployEnv) Redactor() *Redactor {
	return NewRedactor(e.Values)
}
