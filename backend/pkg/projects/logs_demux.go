package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/docker/cli/cli/command"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	kit "kit.getarcane.app"
	"kit.getarcane.app/docker/compat"
)

// logsDemuxCompatibleDockerCli exposes an API client that keeps
// stderr metadata alive in non-TTY container log streams. Used only by
// ComposeLogs; other compose operations keep the unmodified client.
type logsDemuxCompatibleDockerCli struct {
	command.Cli

	apiClient client.APIClient
}

func (c *logsDemuxCompatibleDockerCli) Client() client.APIClient {
	return c.apiClient
}

func wrapDockerCLIWithLogsDemux(cli command.Cli) command.Cli {
	if cli == nil {
		return nil
	}
	return &logsDemuxCompatibleDockerCli{
		Cli:       cli,
		apiClient: &logsDemuxDockerClient{APIClient: compat.WrapDockerAPIClientForInspectCompatibility(cli.Client())},
	}
}

// logsDemuxDockerClient records each container's TTY state from the
// inspect calls Compose already performs, and reworks non-TTY multiplexed log
// responses so stderr entries stay identifiable: stderr lines are prefixed
// with Arcane's stderr marker and every entry is re-framed as a stdout
// stdcopy frame, which Compose merges through its stdout consumer path. TTY
// containers pass through untouched because their stream carries no stderr
// metadata.
type logsDemuxDockerClient struct {
	client.APIClient

	containerTty map[string]bool

	ttyMu sync.RWMutex
}

func (c *logsDemuxDockerClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	result, err := c.APIClient.ContainerInspect(ctx, containerID, options)
	if err == nil && result.Container.Config != nil {
		c.recordContainerTTY(containerID, result.Container.Config.Tty)
	}
	return result, err
}

func (c *logsDemuxDockerClient) ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	resp, err := c.APIClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return resp, err
	}

	tty, known := c.containerTTY(containerID)
	if tty || !known || !options.ShowStdout || !options.ShowStderr {
		return resp, nil
	}

	return &stderrMarkedDemuxedStream{upstream: resp, src: bufio.NewReader(resp)}, nil
}

func (c *logsDemuxDockerClient) recordContainerTTY(containerID string, tty bool) {
	key := strings.ToLower(strings.TrimSpace(containerID))
	c.ttyMu.Lock()
	defer c.ttyMu.Unlock()
	if c.containerTty == nil {
		c.containerTty = make(map[string]bool, 8)
	}
	c.containerTty[key] = tty
}

func (c *logsDemuxDockerClient) containerTTY(containerID string) (tty, known bool) {
	key := strings.ToLower(strings.TrimSpace(containerID))
	c.ttyMu.RLock()
	defer c.ttyMu.RUnlock()
	tty, known = c.containerTty[key]
	return tty, known
}

// maxLogEntryBytes caps re-framed entries: a newline-less run is
// split into cap-sized entries so per-line buffering stays bounded end to end.
const maxLogEntryBytes = 64 * 1024

// stderrMarkedDemuxedStream transforms a multiplexed Docker log
// response, pulled frame by frame on demand, into stdout stdcopy frames of
// one log line each, with stderr lines prefixed by Arcane's stderr marker.
// Lines are assembled per source stream before merging so frames interleaved
// mid-line cannot corrupt each other. Closing it closes the upstream
// response, which also unblocks any pending read.
type stderrMarkedDemuxedStream struct {
	upstream io.ReadCloser
	err      error
	src      *bufio.Reader
	pending  [2][]byte
	out      bytes.Buffer
}

func (s *stderrMarkedDemuxedStream) Read(p []byte) (int, error) {
	for s.out.Len() == 0 {
		if s.err != nil {
			return 0, s.err
		}
		s.ingestFrame()
	}
	return s.out.Read(p)
}

func (s *stderrMarkedDemuxedStream) Close() error {
	return s.upstream.Close()
}

func (s *stderrMarkedDemuxedStream) ingestFrame() {
	var header [8]byte
	if _, err := io.ReadFull(s.src, header[:]); err != nil {
		s.finish(err)
		return
	}
	payload := make([]byte, binary.BigEndian.Uint32(header[4:]))
	if _, err := io.ReadFull(s.src, payload); err != nil {
		s.finish(err)
		return
	}
	if header[0] == byte(stdcopy.Systemerr) {
		s.finish(fmt.Errorf("error from daemon in stream: %s", payload))
		return
	}

	stream := kit.Ternary(header[0] == byte(stdcopy.Stderr), 1, 0)
	s.pending[stream] = append(s.pending[stream], payload...)
	buf := s.pending[stream]
	for len(buf) > 0 {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 || nl > maxLogEntryBytes {
			if len(buf) < maxLogEntryBytes {
				break
			}
			s.emitLine(stream, buf[:maxLogEntryBytes])
			buf = buf[maxLogEntryBytes:]
			continue
		}
		s.emitLine(stream, buf[:nl])
		buf = buf[nl+1:]
	}
	s.pending[stream] = buf
}

func (s *stderrMarkedDemuxedStream) finish(err error) {
	for stream, rest := range s.pending {
		if len(rest) > 0 {
			s.emitLine(stream, rest)
			s.pending[stream] = nil
		}
	}
	s.err = err
}

func (s *stderrMarkedDemuxedStream) emitLine(stream int, line []byte) {
	size := len(line) + 1
	if stream == 1 {
		size += len(stderrLogLinePrefix)
	}
	var header [8]byte
	header[0] = byte(stdcopy.Stdout)
	binary.BigEndian.PutUint32(header[4:], uint32(size)) //nolint:gosec // entries are capped at maxLogEntryBytes
	s.out.Write(header[:])
	if stream == 1 {
		s.out.WriteString(stderrLogLinePrefix)
	}
	s.out.Write(line)
	s.out.WriteByte('\n')
}
