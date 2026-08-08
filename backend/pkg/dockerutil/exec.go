package docker

import (
	"context"
	"io"
	"net"
	"strings"

	"emperror.dev/errors"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// IsExpectedStreamEndError reports whether err is the ordinary way a Docker
// attach/log stream ends rather than a real failure.
func IsExpectedStreamEndError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "use of closed network connection") ||
		strings.Contains(errMsg, "context canceled") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset by peer")
}

// StartStdCopy demultiplexes a Docker stream into stdout/stderr on a goroutine
// and reports the copy result on the returned channel. Pair with WaitStdCopy.
func StartStdCopy(stream io.Reader, stdout, stderr io.Writer) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, stream)
		done <- err
	}()
	return done
}

// WaitStdCopy waits for a StartStdCopy goroutine, treating an ordinary stream
// end as success.
func WaitStdCopy(done <-chan error) error {
	err := <-done
	if err == nil || IsExpectedStreamEndError(err) {
		return nil
	}
	return err
}

// ExecInContainer runs an exec in containerID, demultiplexing its output into
// stdout and stderr, and returns the exec's exit code. A non-zero exit code is
// not an error — callers decide how to report it.
func ExecInContainer(ctx context.Context, dockerClient *client.Client, containerID string, opts client.ExecCreateOptions, stdout, stderr io.Writer) (int, error) {
	execResp, err := dockerClient.ExecCreate(ctx, containerID, opts)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to create exec")
	}

	attachResp, err := dockerClient.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return 0, errors.WrapIf(err, "failed to attach to exec")
	}
	defer attachResp.Close()

	if err := WaitStdCopy(StartStdCopy(attachResp.Reader, stdout, stderr)); err != nil {
		return 0, errors.WrapIf(err, "failed to read exec output")
	}

	inspect, err := dockerClient.ExecInspect(ctx, execResp.ID, client.ExecInspectOptions{})
	if err != nil {
		return 0, errors.WrapIf(err, "failed to inspect exec")
	}

	return inspect.ExitCode, nil
}
