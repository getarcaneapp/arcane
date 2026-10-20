package secretsource

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	flushRecorder struct {
		bytes.Buffer
		flushed int
	}
)

func TestRedactorString(t *testing.T) {
	r := NewRedactor(map[string]string{
		"DB_PASSWORD": "hunter22",
		"PORT":        "80",
		"QUOTED":      `pa"ss\word`,
		"LONGER":      "hunter22-extended",
		"PEM":         "line-one-abc\nline-two-def",
		"HTML":        "a<b>&c",
	})
	require.NotNil(t, r)

	assert.Equal(t, "password is [REDACTED] on port 80", r.String("password is hunter22 on port 80"))
	assert.Equal(t, "[REDACTED]", r.String("hunter22-extended"), "the longer value is masked whole")
	assert.Equal(t, `{"msg":"[REDACTED]"}`, r.String(`{"msg":"pa\"ss\\word"}`), "JSON-escaped values are masked")
	assert.Equal(t, "got [REDACTED] then [REDACTED]", r.String("got line-one-abc then line-two-def"))
	assert.Equal(t, `{"v":"[REDACTED]"}`, r.String(`{"v":"a\u003cb\u003e\u0026c"}`), "encoding/json's HTML escapes are masked")
}

func TestNilRedactor(t *testing.T) {
	var r *Redactor
	assert.Nil(t, NewRedactor(map[string]string{"PORT": "80"}), "values shorter than four characters are not masked")
	assert.Equal(t, "text", r.String("text"))
	err := errors.New("boom")
	assert.Same(t, err, r.Error(err))

	var out bytes.Buffer
	w := r.Writer(&out)
	_, _ = w.Write([]byte("plain"))
	require.NoError(t, w.Close())
	assert.Equal(t, "plain", out.String())
	assert.Nil(t, r.Writer(nil))
}

func TestRedactorError(t *testing.T) {
	r := NewRedactor(map[string]string{"TOKEN": "s3cr3t-token"})
	base := errors.New("invalid value s3cr3t-token in service web")
	err := r.Error(base)
	assert.Equal(t, "invalid value [REDACTED] in service web", err.Error())
	assert.ErrorIs(t, err, base)

	clean := errors.New("nothing to hide")
	assert.Same(t, clean, r.Error(clean))
}

func (f *flushRecorder) Flush() { f.flushed++ }

func TestRedactorWriterSplitsAcrossWrites(t *testing.T) {
	r := NewRedactor(map[string]string{"TOKEN": "s3cr3t-token"})
	out := &flushRecorder{}
	w := r.Writer(out)

	// The value is split over two writes; nothing is passed on mid-line.
	_, _ = w.Write([]byte(`{"status":"using s3cr`))
	assert.Empty(t, out.String())
	_, _ = w.Write([]byte("3t-token\"}\n{\"status\":\"done"))
	assert.Equal(t, `{"status":"using [REDACTED]"}`+"\n", out.String())

	flusher, ok := w.(interface{ Flush() })
	require.True(t, ok, "the writer keeps streaming flushes working")
	flusher.Flush()
	assert.Equal(t, 1, out.flushed)
	assert.Equal(t, `{"status":"using [REDACTED]"}`+"\n"+`{"status":"done`, out.String())
	require.NoError(t, w.Close())
}

func TestRedactorWriterLongLine(t *testing.T) {
	r := NewRedactor(map[string]string{"TOKEN": "s3cr3t-token"})
	var out bytes.Buffer
	w := r.Writer(&out)
	long := bytes.Repeat([]byte("x"), maxPendingLine+10)
	_, _ = w.Write(append(long, []byte("s3cr3t-token")...))
	require.NoError(t, w.Close())
	assert.NotContains(t, out.String(), "s3cr3t-token")
	assert.Len(t, out.String(), len(long)+len(RedactedMarker))
	var _ io.WriteCloser = w
}
