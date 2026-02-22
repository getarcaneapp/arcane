package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/pkg/stdcopy"
)

// streamMultiplexedLogs demultiplexes a Docker log stream (stdout/stderr)
// and sends lines to logsChan. Used by both container and swarm service logs.
func streamMultiplexedLogs(ctx context.Context, logs io.ReadCloser, logsChan chan<- string) error {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	go func() {
		defer func() { _ = stdoutWriter.Close() }()
		defer func() { _ = stderrWriter.Close() }()
		_, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, logs)
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Printf("Error demultiplexing logs: %v\n", err)
		}
	}()

	done := make(chan error, 2)

	go func() {
		done <- readLogsFromReader(ctx, stdoutReader, logsChan, "stdout")
	}()

	go func() {
		done <- readLogsFromReader(ctx, stderrReader, logsChan, "stderr")
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}
}

// readLogsFromReader reads logs line by line from a reader and sends them to logsChan.
func readLogsFromReader(ctx context.Context, reader io.Reader, logsChan chan<- string, source string) error {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			line := scanner.Text()
			if line != "" {
				if source == "stderr" {
					line = "[STDERR] " + line
				}

				select {
				case logsChan <- line:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return scanner.Err()
}

// readAllLogs reads all logs at once (non-follow mode) and sends them to logsChan.
func readAllLogs(logs io.ReadCloser, logsChan chan<- string) error {
	stdoutBuf := &strings.Builder{}
	stderrBuf := &strings.Builder{}

	_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, logs)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to demultiplex logs: %w", err)
	}

	if stdoutBuf.Len() > 0 {
		lines := strings.SplitSeq(strings.TrimRight(stdoutBuf.String(), "\n"), "\n")
		for line := range lines {
			if line != "" {
				logsChan <- line
			}
		}
	}

	if stderrBuf.Len() > 0 {
		lines := strings.SplitSeq(strings.TrimRight(stderrBuf.String(), "\n"), "\n")
		for line := range lines {
			if line != "" {
				logsChan <- "[STDERR] " + line
			}
		}
	}

	return nil
}
