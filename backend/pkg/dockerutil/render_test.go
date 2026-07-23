package docker

import (
	"strings"
	"sync"
	"testing"
)

func TestRenderJSONMessageStream(t *testing.T) {
	t.Run("renders docker CLI text for pull messages", func(t *testing.T) {
		stream := strings.NewReader(
			`{"status":"Pulling from library/nginx","id":"stable-alpine"}` + "\n" +
				`{"status":"Pull complete","id":"abc123"}` + "\n" +
				`{"status":"Status: Downloaded newer image for nginx:stable-alpine"}` + "\n")
		var out strings.Builder

		if err := RenderJSONMessageStream(stream, &out); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := "stable-alpine: Pulling from library/nginx\n" +
			"abc123: Pull complete\n" +
			"Status: Downloaded newer image for nginx:stable-alpine\n"
		if out.String() != want {
			t.Fatalf("expected CLI-parity output %q, got %q", want, out.String())
		}
	})

	t.Run("returns daemon errorDetail verbatim", func(t *testing.T) {
		stream := strings.NewReader(`{"errorDetail":{"code":401,"message":"unauthorized"}}` + "\n")
		err := RenderJSONMessageStream(stream, nil)
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			t.Fatalf("expected unauthorized error, got %v", err)
		}
	})
}

// Compose's plain progress renderer writes from multiple goroutines; the
// writer must tolerate concurrent writes without corrupting its buffer.
func TestLogLineWriterConcurrentWrites(t *testing.T) {
	var out syncBuffer
	w := NewLogLineWriter(&out)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_, _ = w.Write([]byte("Container test Created\n"))
			}
		}()
	}
	wg.Wait()
	_ = w.Close()

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 8*200 {
		t.Fatalf("expected %d frames, got %d", 8*200, len(lines))
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLogLineWriter(t *testing.T) {
	var out strings.Builder
	w := NewLogLineWriter(&out)

	_, _ = w.Write([]byte("Container arcane-test Creating\nContainer arcane"))
	_, _ = w.Write([]byte("-test Created\npartial"))
	_ = w.Close()

	want := `{"log":"Container arcane-test Creating"}` + "\n" +
		`{"log":"Container arcane-test Created"}` + "\n" +
		`{"log":"partial"}` + "\n"
	if out.String() != want {
		t.Fatalf("expected framed lines %q, got %q", want, out.String())
	}
}
