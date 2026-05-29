package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/moby/moby/api/pkg/stdcopy"
)

// StreamContainerLogs streams Docker container logs, handling TTY raw streams
// and non-TTY multiplexed stdout/stderr streams.
func StreamContainerLogs(ctx context.Context, logs io.ReadCloser, logsChan chan<- string, follow bool, isTTY bool) error {
	if isTTY {
		return readLogLinesInternal(ctx, logs, logsChan, "")
	}
	if follow {
		return StreamMultiplexedLogs(ctx, logs, logsChan)
	}
	return ReadAllLogs(ctx, logs, logsChan)
}

// StreamMultiplexedLogs demultiplexes a Docker stdout/stderr log stream and
// sends non-empty lines to logsChan.
func StreamMultiplexedLogs(ctx context.Context, logs io.Reader, logsChan chan<- string) error {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	var closeOnce sync.Once
	closeInputs := func() {
		closeOnce.Do(func() {
			err := ctx.Err()
			if err == nil {
				err = io.ErrClosedPipe
			}
			_ = stdoutReader.CloseWithError(err)
			_ = stderrReader.CloseWithError(err)
			if closer, ok := logs.(io.Closer); ok {
				_ = closer.Close()
			}
		})
	}

	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		defer func() { _ = stdoutWriter.Close() }()
		defer func() { _ = stderrWriter.Close() }()
		_, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, logs)
		if err != nil && !errors.Is(err, io.EOF) && ctx.Err() == nil {
			slog.Error("error demultiplexing logs", "error", err)
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			closeInputs()
		case <-copyDone:
		}
	}()

	done := make(chan error, 2)

	go func() {
		done <- readLogLinesInternal(ctx, stdoutReader, logsChan, "")
	}()

	go func() {
		done <- readLogLinesInternal(ctx, stderrReader, logsChan, "[STDERR] ")
	}()

	select {
	case <-ctx.Done():
		closeInputs()
		return ctx.Err()
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			closeInputs()
			return err
		}
		select {
		case <-ctx.Done():
			closeInputs()
			return ctx.Err()
		case <-done:
			return nil
		}
	}
}

// ReadAllLogs reads a non-follow Docker multiplexed log stream and sends
// non-empty stdout/stderr lines to logsChan.
func ReadAllLogs(ctx context.Context, logs io.ReadCloser, logsChan chan<- string) error {
	stdoutBuf := &strings.Builder{}
	stderrBuf := &strings.Builder{}
	stdCopyDone := make(chan struct{})
	defer close(stdCopyDone)

	go func() {
		select {
		case <-ctx.Done():
			_ = logs.Close()
		case <-stdCopyDone:
		}
	}()

	_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, logs)
	if err != nil && !errors.Is(err, io.EOF) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("failed to demultiplex logs: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if stdoutBuf.Len() > 0 {
		if err := readLogLinesInternal(ctx, strings.NewReader(stdoutBuf.String()), logsChan, ""); err != nil {
			return err
		}
	}

	if stderrBuf.Len() > 0 {
		if err := readLogLinesInternal(ctx, strings.NewReader(stderrBuf.String()), logsChan, "[STDERR] "); err != nil {
			return err
		}
	}

	return nil
}

func readLogLinesInternal(ctx context.Context, reader io.Reader, logsChan chan<- string, prefix string) error {
	bufferedReader := bufio.NewReader(reader)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := bufferedReader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed != "" {
				if prefix != "" {
					trimmed = prefix + trimmed
				}

				select {
				case logsChan <- trimmed:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
