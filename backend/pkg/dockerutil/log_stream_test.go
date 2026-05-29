package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamContainerLogsNonTTYFollowDemultiplexesStdoutAndStderr(t *testing.T) {
	var stream bytes.Buffer
	writeDockerLogFrameInternal(t, &stream, 1, "stdout line\n")
	writeDockerLogFrameInternal(t, &stream, 2, "stderr line\n")

	logsChan := make(chan string, 4)

	err := StreamContainerLogs(t.Context(), io.NopCloser(bytes.NewReader(stream.Bytes())), logsChan, true, false)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"stdout line", "[STDERR] stderr line"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogsTTYFollowStreamsRawOutput(t *testing.T) {
	logsChan := make(chan string, 4)

	err := StreamContainerLogs(t.Context(), io.NopCloser(strings.NewReader("first line\nsecond line")), logsChan, true, true)
	require.NoError(t, err)

	require.Equal(t, []string{"first line", "second line"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogsNonTTYSnapshotDemultiplexesStdoutAndStderr(t *testing.T) {
	var stream bytes.Buffer
	writeDockerLogFrameInternal(t, &stream, 1, "stdout snapshot\n")
	writeDockerLogFrameInternal(t, &stream, 2, "stderr snapshot\n")

	logsChan := make(chan string, 4)

	err := StreamContainerLogs(t.Context(), io.NopCloser(bytes.NewReader(stream.Bytes())), logsChan, false, false)
	require.NoError(t, err)

	require.Equal(t, []string{"stdout snapshot", "[STDERR] stderr snapshot"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogsTTYSnapshotStreamsRawOutput(t *testing.T) {
	logsChan := make(chan string, 4)

	err := StreamContainerLogs(t.Context(), io.NopCloser(strings.NewReader("snapshot line\ntrailing line")), logsChan, false, true)
	require.NoError(t, err)

	require.Equal(t, []string{"snapshot line", "trailing line"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogsTTYHandlesLongLinesAndPartialEOF(t *testing.T) {
	longLine := strings.Repeat("a", 70*1024)
	logsChan := make(chan string, 4)

	err := StreamContainerLogs(t.Context(), io.NopCloser(strings.NewReader(longLine+"\npartial tail")), logsChan, true, true)
	require.NoError(t, err)

	require.Equal(t, []string{longLine, "partial tail"}, drainLogLinesInternal(logsChan))
}

func TestStreamContainerLogsTTYPythonLikeFollowDoesNotReturnEmptyLogs(t *testing.T) {
	logsChan := make(chan string, 4)

	err := StreamContainerLogs(
		t.Context(),
		io.NopCloser(strings.NewReader("2026-03-22 10:15:00 - INFO - Starting miner\n2026-03-22 10:15:01 - INFO - Connected")),
		logsChan,
		true,
		true,
	)
	require.NoError(t, err)

	lines := drainLogLinesInternal(logsChan)
	require.NotEmpty(t, lines)
	require.Equal(t, []string{
		"2026-03-22 10:15:00 - INFO - Starting miner",
		"2026-03-22 10:15:01 - INFO - Connected",
	}, lines)
}

func TestStreamMultiplexedLogsContextCancelDoesNotDeadlock(t *testing.T) {
	var stream bytes.Buffer
	writeDockerLogFrameInternal(t, &stream, 1, "line 1\n")
	writeDockerLogFrameInternal(t, &stream, 1, "line 2\n")
	writeDockerLogFrameInternal(t, &stream, 1, "line 3\n")

	logsChan := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- StreamMultiplexedLogs(ctx, bytes.NewReader(stream.Bytes()), logsChan)
	}()

	require.Eventually(t, func() bool {
		return len(logsChan) == 1
	}, time.Second, 10*time.Millisecond)

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("StreamMultiplexedLogs did not exit after cancellation")
	}
}

func TestReadAllLogsContextCancelClosesReader(t *testing.T) {
	logsChan := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &blockingReadCloserInternal{readStarted: make(chan struct{}), closeCalled: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- ReadAllLogs(ctx, reader, logsChan)
	}()

	select {
	case <-reader.readStarted:
	case <-time.After(time.Second):
		t.Fatal("ReadAllLogs did not start reading")
	}

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("ReadAllLogs did not exit after cancellation")
	}

	select {
	case <-reader.closeCalled:
	case <-time.After(time.Second):
		t.Fatal("ReadAllLogs did not close the reader on cancellation")
	}
}

func drainLogLinesInternal(logsChan chan string) []string {
	close(logsChan)

	lines := make([]string, 0, len(logsChan))
	for line := range logsChan {
		lines = append(lines, line)
	}

	return lines
}

func writeDockerLogFrameInternal(t *testing.T, buffer *bytes.Buffer, streamType byte, payload string) {
	t.Helper()

	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	_, err := buffer.Write(header)
	require.NoError(t, err)
	_, err = buffer.WriteString(payload)
	require.NoError(t, err)
}

type blockingReadCloserInternal struct {
	readStarted chan struct{}
	closeCalled chan struct{}
}

func (r *blockingReadCloserInternal) Read(_ []byte) (int, error) {
	select {
	case <-r.readStarted:
	default:
		close(r.readStarted)
	}

	<-r.closeCalled
	return 0, io.EOF
}

func (r *blockingReadCloserInternal) Close() error {
	select {
	case <-r.closeCalled:
	default:
		close(r.closeCalled)
	}
	return nil
}
