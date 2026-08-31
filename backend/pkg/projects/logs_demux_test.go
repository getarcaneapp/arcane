package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func frameLogEntryInternal(fd stdcopy.StdType, payload []byte) []byte {
	header := make([]byte, 8)
	header[0] = byte(fd)
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func decodeDemuxedStreamInternal(t *testing.T, stream io.Reader) string {
	t.Helper()
	decoded := &bytes.Buffer{}
	sinkErr := &bytes.Buffer{}
	_, err := stdcopy.StdCopy(decoded, sinkErr, stream)
	require.NoError(t, err)
	require.Empty(t, sinkErr.String(), "every entry must be re-framed as stdout")
	return decoded.String()
}

func TestDemultiplexedLogStreamWithStderrMarking(t *testing.T) {
	longRun := strings.Repeat("x", maxLogEntryBytesInternal+100)
	source := bytes.Join([][]byte{
		frameLogEntryInternal(stdcopy.Stdout, []byte("line-one\npar")),
		frameLogEntryInternal(stdcopy.Stderr, []byte("boom\n")),
		frameLogEntryInternal(stdcopy.Stdout, []byte("tial\n")),
		frameLogEntryInternal(stdcopy.Stderr, []byte(longRun)),
		frameLogEntryInternal(stdcopy.Stderr, []byte("\nfinal")),
	}, nil)

	resp := io.NopCloser(bytes.NewReader(source))
	stream := &stderrMarkedDemuxedStreamInternal{upstream: resp, src: bufio.NewReader(resp)}

	require.Equal(t,
		"line-one\n"+
			"[STDERR] boom\n"+
			"partial\n"+
			"[STDERR] "+longRun[:maxLogEntryBytesInternal]+"\n"+
			"[STDERR] "+longRun[maxLogEntryBytesInternal:]+"\n"+
			"[STDERR] final\n",
		decodeDemuxedStreamInternal(t, stream))
}

type fakeResponseBodyInternal struct {
	io.Reader
	closed bool
}

func (b *fakeResponseBodyInternal) Close() error {
	b.closed = true
	return nil
}

type stubLogAPIClientInternal struct {
	client.APIClient

	resp io.ReadCloser
}

func (s *stubLogAPIClientInternal) ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	return s.resp, nil
}

func TestLogsDemuxClientContainerLogsDecisionInternal(t *testing.T) {
	muxed := bytes.Join([][]byte{
		frameLogEntryInternal(stdcopy.Stdout, []byte("out\n")),
		frameLogEntryInternal(stdcopy.Stderr, []byte("err\n")),
	}, nil)

	tests := []struct {
		name           string
		ttyRecorded    bool
		containerKnown bool
		showStdout     bool
		showStderr     bool
		wantWrapped    bool
	}{
		{name: "non-tty streams are transformed", containerKnown: true, showStdout: true, showStderr: true, wantWrapped: true},
		{name: "tty passthrough", ttyRecorded: true, containerKnown: true, showStdout: true, showStderr: true, wantWrapped: false},
		{name: "unknown container passthrough", containerKnown: false, showStdout: true, showStderr: true, wantWrapped: false},
		{name: "single-stream request passthrough", containerKnown: true, showStdout: true, showStderr: false, wantWrapped: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &fakeResponseBodyInternal{Reader: bytes.NewReader(muxed)}
			inner := &logsDemuxDockerClientInternal{APIClient: &stubLogAPIClientInternal{resp: body}}
			if tt.containerKnown {
				inner.recordContainerTTYInternal(" CTR-1 ", tt.ttyRecorded)
			}

			got, err := inner.ContainerLogs(context.Background(), "ctr-1", client.ContainerLogsOptions{ShowStdout: tt.showStdout, ShowStderr: tt.showStderr})
			require.NoError(t, err)

			if !tt.wantWrapped {
				require.Same(t, body, got)
				return
			}

			require.Equal(t, "out\n[STDERR] err\n", decodeDemuxedStreamInternal(t, got))
			require.NoError(t, got.Close())
			require.True(t, body.closed, "closing the wrapped stream must close the upstream response")
		})
	}
}
