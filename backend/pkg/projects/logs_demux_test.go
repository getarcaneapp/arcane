package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	mobyStdCopy "github.com/moby/moby/api/pkg/stdcopy"
	mobyClient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type (
	fakeResponseBody struct {
		io.Reader
		closed bool
	}

	stubLogAPIClient struct {
		mobyClient.APIClient

		resp io.ReadCloser
	}
)

func frameLogEntry(fd mobyStdCopy.StdType, payload []byte) []byte {
	header := make([]byte, 8)
	header[0] = byte(fd)
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func decodeDemuxedStream(t *testing.T, stream io.Reader) string {
	t.Helper()
	decoded := &bytes.Buffer{}
	sinkErr := &bytes.Buffer{}
	_, err := mobyStdCopy.StdCopy(decoded, sinkErr, stream)
	require.NoError(t, err)
	require.Empty(t, sinkErr.String(), "every entry must be re-framed as stdout")
	return decoded.String()
}

func TestDemultiplexedLogStreamWithStderrMarking(t *testing.T) {
	longRun := strings.Repeat("x", maxLogEntryBytes+100)
	source := bytes.Join([][]byte{
		frameLogEntry(mobyStdCopy.Stdout, []byte("line-one\npar")),
		frameLogEntry(mobyStdCopy.Stderr, []byte("boom\n")),
		frameLogEntry(mobyStdCopy.Stdout, []byte("tial\n")),
		frameLogEntry(mobyStdCopy.Stderr, []byte(longRun)),
		frameLogEntry(mobyStdCopy.Stderr, []byte("\nfinal")),
	}, nil)

	resp := io.NopCloser(bytes.NewReader(source))
	stream := &stderrMarkedDemuxedStream{upstream: resp, src: bufio.NewReader(resp)}

	require.Equal(t, "line-one\n[STDERR] boom\npartial\n[STDERR] "+
		longRun[:maxLogEntryBytes]+"\n"+
		"[STDERR] "+longRun[maxLogEntryBytes:]+"\n"+
		"[STDERR] final\n", decodeDemuxedStream(t, stream))
}

func (b *fakeResponseBody) Close() error {
	b.closed = true
	return nil
}

func (s *stubLogAPIClient) ContainerLogs(ctx context.Context, containerID string, options mobyClient.ContainerLogsOptions) (mobyClient.ContainerLogsResult, error) {
	return s.resp, nil
}

func TestLogsDemuxClientContainerLogsDecision(t *testing.T) {
	muxed := bytes.Join([][]byte{
		frameLogEntry(mobyStdCopy.Stdout, []byte("out\n")),
		frameLogEntry(mobyStdCopy.Stderr, []byte("err\n")),
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
			body := &fakeResponseBody{Reader: bytes.NewReader(muxed)}
			inner := &logsDemuxDockerClient{
				APIClient:    &stubLogAPIClient{resp: body},
				containerTty: make(map[string]bool, 8),
			}
			if tt.containerKnown {
				inner.recordContainerTTY(" CTR-1 ", tt.ttyRecorded)
			}

			got, err := inner.ContainerLogs(t.Context(), "ctr-1", mobyClient.ContainerLogsOptions{ShowStdout: tt.showStdout, ShowStderr: tt.showStderr})
			require.NoError(t, err)

			if !tt.wantWrapped {
				require.Same(t, body, got)
				return
			}

			require.Equal(t, "out\n[STDERR] err\n", decodeDemuxedStream(t, got))
			require.NoError(t, got.Close())
			require.True(t, body.closed, "closing the wrapped stream must close the upstream response")
		})
	}
}
