//go:build linux

package startup

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"emperror.dev/errors"

	"github.com/samber/mo"
)

func reexecWithRuntimeIdentityInternal(ctx context.Context, req runtimeIdentityRequest) error {
	executable, err := os.Executable()
	if err != nil {
		return errors.WrapIf(err, "resolve executable")
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
		return errors.WrapIf(err, "start runtime identity child")
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

			return errors.WrapIf(err, "wait for runtime identity child")
		}
	}
}

func resolveSocketGroupInternal(socketPath string) mo.Option[uint32] {
	info, err := os.Stat(socketPath)
	if err != nil {
		return mo.None[uint32]()
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return mo.None[uint32]()
	}

	return mo.Some(stat.Gid)
}
