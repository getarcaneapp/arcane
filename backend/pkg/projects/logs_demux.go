package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"sync"

	"github.com/docker/cli/cli/command"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
)

// logsDemuxCompatibleDockerCliInternal exposes an API client that keeps
// stderr metadata alive in non-TTY container log streams. Used only by
// ComposeLogs; other compose operations keep the unmodified client.
type logsDemuxCompatibleDockerCliInternal struct {
	command.Cli

	apiClient client.APIClient
}

func (c *logsDemuxCompatibleDockerCliInternal) Client() client.APIClient {
	return c.apiClient
}

func wrapDockerCLIWithLogsDemuxInternal(cli command.Cli) command.Cli {
	if cli == nil {
		return nil
	}
	return &logsDemuxCompatibleDockerCliInternal{
		Cli:       cli,
		apiClient: &logsDemuxDockerClientInternal{APIClient: libarcane.WrapDockerAPIClientForInspectCompatibility(cli.Client())},
	}
}

// logsDemuxDockerClientInternal records each container's TTY state from the
// inspect calls Compose already performs, and reworks non-TTY multiplexed log
// responses so stderr entries stay identifiable: stderr lines are prefixed
// with Arcane's stderr marker and every entry is re-framed as a stdout
// stdcopy frame, which Compose merges through its stdout consumer path. TTY
// containers pass through untouched because their stream carries no stderr
// metadata.
type logsDemuxDockerClientInternal struct {
	client.APIClient

	ttyMu        sync.RWMutex
	containerTty map[string]bool
}

func (c *logsDemuxDockerClientInternal) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	result, err := c.APIClient.ContainerInspect(ctx, containerID, options)
	if err == nil && result.Container.Config != nil {
		c.recordContainerTTYInternal(containerID, result.Container.Config.Tty)
	}
	return result, err
}

func (c *logsDemuxDockerClientInternal) ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	resp, err := c.APIClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return resp, err
	}

	tty, known := c.containerTTYInternal(containerID)
	if tty || !known || !options.ShowStdout || !options.ShowStderr {
		return resp, nil
	}

	return &stderrMarkedDemuxedStreamInternal{upstream: resp, src: bufio.NewReader(resp)}, nil
}

func (c *logsDemuxDockerClientInternal) recordContainerTTYInternal(containerID string, tty bool) {
	key := strings.ToLower(strings.TrimSpace(containerID))
	c.ttyMu.Lock()
	defer c.ttyMu.Unlock()
	if c.containerTty == nil {
		c.containerTty = make(map[string]bool, 8)
	}
	c.containerTty[key] = tty
}

func (c *logsDemuxDockerClientInternal) containerTTYInternal(containerID string) (tty bool, known bool) {
	key := strings.ToLower(strings.TrimSpace(containerID))
	c.ttyMu.RLock()
	defer c.ttyMu.RUnlock()
	tty, known = c.containerTty[key]
	return tty, known
}

// maxLogEntryBytesInternal caps re-framed entries: a newline-less run is
// split into cap-sized entries so per-line buffering stays bounded end to end.
const maxLogEntryBytesInternal = 64 * 1024

// stderrMarkedDemuxedStreamInternal transforms a multiplexed Docker log
// response, pulled frame by frame on demand, into stdout stdcopy frames of
// one log line each, with stderr lines prefixed by Arcane's stderr marker.
// Lines are assembled per source stream before merging so frames interleaved
// mid-line cannot corrupt each other. Closing it closes the upstream
// response, which also unblocks any pending read.
type stderrMarkedDemuxedStreamInternal struct {
	upstream io.ReadCloser
	src      *bufio.Reader
	out      bytes.Buffer
	pending  [2][]byte
	err      error
}

func (s *stderrMarkedDemuxedStreamInternal) Read(p []byte) (int, error) {
	for s.out.Len() == 0 {
		if s.err != nil {
			return 0, s.err
		}
		s.ingestFrameInternal()
	}
	return s.out.Read(p)
}

func (s *stderrMarkedDemuxedStreamInternal) Close() error {
	return s.upstream.Close()
}

func (s *stderrMarkedDemuxedStreamInternal) ingestFrameInternal() {
	var header [8]byte
	if _, err := io.ReadFull(s.src, header[:]); err != nil {
		s.finishInternal(err)
		return
	}
	payload := make([]byte, binary.BigEndian.Uint32(header[4:]))
	if _, err := io.ReadFull(s.src, payload); err != nil {
		s.finishInternal(err)
		return
	}

	stream := 0
	if header[0] == byte(stdcopy.Stderr) {
		stream = 1
	}
	s.pending[stream] = append(s.pending[stream], payload...)
	buf := s.pending[stream]
	for len(buf) > 0 {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 || nl > maxLogEntryBytesInternal {
			if len(buf) < maxLogEntryBytesInternal {
				break
			}
			s.emitLineInternal(stream, buf[:maxLogEntryBytesInternal])
			buf = buf[maxLogEntryBytesInternal:]
			continue
		}
		s.emitLineInternal(stream, buf[:nl])
		buf = buf[nl+1:]
	}
	s.pending[stream] = buf
}

func (s *stderrMarkedDemuxedStreamInternal) finishInternal(err error) {
	for stream, rest := range s.pending {
		if len(rest) > 0 {
			s.emitLineInternal(stream, rest)
			s.pending[stream] = nil
		}
	}
	s.err = err
}

func (s *stderrMarkedDemuxedStreamInternal) emitLineInternal(stream int, line []byte) {
	size := len(line) + 1
	if stream == 1 {
		size += len(stderrLogLinePrefixInternal)
	}
	var header [8]byte
	header[0] = byte(stdcopy.Stdout)
	binary.BigEndian.PutUint32(header[4:], uint32(size)) //nolint:gosec // entries are capped at maxLogEntryBytesInternal
	s.out.Write(header[:])
	if stream == 1 {
		s.out.WriteString(stderrLogLinePrefixInternal)
	}
	s.out.Write(line)
	s.out.WriteByte('\n')
}
