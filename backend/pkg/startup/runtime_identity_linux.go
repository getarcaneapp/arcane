//go:build linux

package startup

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/samber/mo"
)

func reexecWithRuntimeIdentity(_ context.Context, req runtimeIdentityRequest) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	groups := runtimeIdentitySupplementaryGroups(req.DockerHost, resolveSocketGroup)
	gids := make([]int, 0, len(groups))
	for _, group := range groups {
		gids = append(gids, int(group))
	}

	if setgroupsErr := syscall.Setgroups(gids); setgroupsErr != nil {
		return fmt.Errorf("set runtime identity groups: %w", setgroupsErr)
	}
	if setgidErr := syscall.Setgid(int(req.CredentialGID)); setgidErr != nil {
		return fmt.Errorf("set runtime identity gid: %w", setgidErr)
	}
	if setuidErr := syscall.Setuid(int(req.CredentialUID)); setuidErr != nil {
		return fmt.Errorf("set runtime identity uid: %w", setuidErr)
	}

	if setenvErr := os.Setenv("DOCKER_HOST", req.DockerHost); setenvErr != nil {
		return fmt.Errorf("set runtime identity docker host: %w", setenvErr)
	}
	//nolint:gosec // re-executing our own binary with the same args under the dropped UID/GID
	if execErr := syscall.Exec(executable, append([]string{executable}, os.Args[1:]...), os.Environ()); execErr != nil {
		return fmt.Errorf("exec runtime identity: %w", execErr)
	}
	return nil
}

func resolveSocketGroup(socketPath string) mo.Option[uint32] {
	// os.* rather than acfs: the docker socket is a host-configured system path
	// probed during identity bootstrap, not under any acfs root.
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
