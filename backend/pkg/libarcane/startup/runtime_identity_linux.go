//go:build linux

package startup

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func reexecWithRuntimeIdentityInternal(ctx context.Context, req runtimeIdentityRequest) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	groups := runtimeIdentitySupplementaryGroupsInternal(req.DockerHost, resolveSocketGroupInternal)

	cmd := exec.CommandContext(ctx, executable, os.Args[1:]...) //nolint:gosec // re-executing our own binary with the same args under a different UID/GID
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    req.CredentialUID,
			Gid:    req.CredentialGID,
			Groups: groups,
		},
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start runtime identity child: %w", err)
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	for {
		select {
		case sig := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case err := <-done:
			signal.Stop(sigCh)
			if err == nil {
				os.Exit(0)
			}

			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if status.Signaled() {
						os.Exit(128 + int(status.Signal()))
					}
					os.Exit(status.ExitStatus())
				}
				os.Exit(exitErr.ExitCode())
			}

			return fmt.Errorf("wait for runtime identity child: %w", err)
		}
	}
}

func resolveSocketGroupInternal(socketPath string) (uint32, bool) {
	info, err := os.Stat(socketPath)
	if err != nil {
		return 0, false
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}

	return stat.Gid, true
}

func runtimeIdentitySupplementaryGroupsInternal(dockerHost string, resolveSocketGroup func(string) (uint32, bool)) []uint32 {
	socketPath, ok := dockerSocketPathInternal(dockerHost)
	if !ok {
		return nil
	}

	socketGID, ok := resolveSocketGroup(socketPath)
	if !ok {
		return nil
	}

	return []uint32{socketGID}
}

func dockerSocketPathInternal(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultDockerSocketPath, true
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "unix" {
		return "", false
	}

	if parsed.Host != "" || parsed.Path != "" {
		socketPath := parsed.Host + parsed.Path
		if !strings.HasPrefix(socketPath, "/") {
			socketPath = "/" + socketPath
		}
		return filepath.Clean(socketPath), true
	}

	if parsed.Opaque == "" {
		return "", false
	}

	socketPath := strings.TrimPrefix(parsed.Opaque, "//")
	if !strings.HasPrefix(socketPath, "/") {
		socketPath = "/" + socketPath
	}

	return filepath.Clean(socketPath), true
}
