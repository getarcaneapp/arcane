package startup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/mo"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/fs"
)

type (
	runtimeIdentityRequest struct {
		DockerHost    string
		UID           int
		GID           int
		CredentialUID uint32
		CredentialGID uint32
		Enabled       bool
	}

	RuntimeIdentityConfig struct {
		PUID              string
		PGID              string
		DockerHost        string
		DockerConfig      string
		DatabaseURL       string
		ProjectsDirectory string
		InContainer       bool
	}
)

const (
	defaultDataDirectory    = "/app/data"
	defaultBuildsDirectory  = "/builds"
	defaultDatabaseURL      = "file:data/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_txlock=immediate"
	defaultDockerConfigDir  = "/app/data/.docker"
	defaultDockerSocketPath = "/var/run/docker.sock"
	defaultRuntimeUID       = 65532
	defaultRuntimeGID       = 65532
	mountInfoPath           = "/proc/self/mountinfo"
)

var lchownFn = acfs.Host{}.Lchown

func ApplyRequestedRuntimeIdentity(ctx context.Context, cfg *RuntimeIdentityConfig) error {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	projectsDir := ""
	if strings.TrimSpace(cfg.ProjectsDirectory) != "" {
		projectsDir = filepath.Clean(strings.TrimSpace(cfg.ProjectsDirectory))
	}

	inContainer := runningInContainer(ctx, cfg.InContainer, os.Getenv, acfs.Host{}.Exists)
	req, warning, err := loadRuntimeIdentityRequest(cfg, inContainer)
	if warning != "" {
		slog.WarnContext(ctx, "Runtime identity warning", "warning", warning)
	}
	if err != nil {
		return err
	}
	if !req.Enabled {
		return ensureSQLiteFilesExist(ctx, cfg.DatabaseURL)
	}

	if configErr := ensureRuntimeDockerConfig(ctx, cfg, os.Setenv, req.UID, req.GID, inContainer); configErr != nil {
		return configErr
	}

	switch {
	case os.Geteuid() == req.UID && os.Getegid() == req.GID:
		// Avoid re-execing forever when the requested runtime identity is already active.
		return ensureSQLiteFilesExist(ctx, cfg.DatabaseURL)
	case os.Geteuid() != 0:
		slog.WarnContext(ctx, "Runtime identity warning: process is not root, continuing as current user",
			"euid", os.Geteuid(), "puid", req.UID, "pgid", req.GID)
		return ensureSQLiteFilesExist(ctx, cfg.DatabaseURL)
	}

	if inContainer {
		mountInfo, readErr := acfs.Host{}.ReadFile(ctx, mountInfoPath)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return fmt.Errorf("load mountpoints: %w", readErr)
		}
		if prepareErr := prepareWritablePathsWithRoots(ctx, req.UID, req.GID, parseMountpoints(string(mountInfo)), projectsDir, defaultDataDirectory, defaultBuildsDirectory); prepareErr != nil {
			return prepareErr
		}
	}

	return reexecWithRuntimeIdentity(ctx, req)
}

func loadRuntimeIdentityRequest(cfg *RuntimeIdentityConfig, inContainer bool) (runtimeIdentityRequest, string, error) {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	puid := strings.TrimSpace(cfg.PUID)
	pgid := strings.TrimSpace(cfg.PGID)

	if puid == "" || pgid == "" {
		// Without both overrides, containers run as the default non-root user.
		req := runtimeIdentityRequest{DockerHost: cfg.DockerHost}
		if inContainer {
			req = runtimeIdentityRequest{
				Enabled:       true,
				UID:           defaultRuntimeUID,
				GID:           defaultRuntimeGID,
				CredentialUID: uint32(defaultRuntimeUID),
				CredentialGID: uint32(defaultRuntimeGID),
				DockerHost:    cfg.DockerHost,
			}
		}
		if puid == "" && pgid == "" {
			return req, "", nil
		}
		warning := kit.Ternary(
			inContainer,
			"PUID and PGID must both be set to override the default non-root runtime user; continuing with the default non-root runtime user",
			"PUID and PGID must both be set to enable runtime identity outside containers; continuing without runtime identity",
		)
		return req, warning, nil
	}

	uid, credentialUID, err := parseRuntimeIdentityValue(puid, "PUID")
	if err != nil {
		return runtimeIdentityRequest{}, "", fmt.Errorf("invalid PUID %q: %w", puid, err)
	}

	gid, credentialGID, err := parseRuntimeIdentityValue(pgid, "PGID")
	if err != nil {
		return runtimeIdentityRequest{}, "", fmt.Errorf("invalid PGID %q: %w", pgid, err)
	}

	return runtimeIdentityRequest{
		Enabled:       true,
		UID:           uid,
		GID:           gid,
		CredentialUID: credentialUID,
		CredentialGID: credentialGID,
		DockerHost:    cfg.DockerHost,
	}, "", nil
}

func runningInContainer(ctx context.Context, arcaneInContainer bool, getenv func(string) string, exists func(context.Context, string) (bool, error)) bool {
	if arcaneInContainer || strings.TrimSpace(getenv("container")) != "" {
		return true
	}

	for _, markerPath := range []string{"/.dockerenv", "/run/.containerenv"} {
		if found, err := exists(ctx, markerPath); err == nil && found {
			return true
		}
	}

	return false
}

func ensureRuntimeDockerConfig(ctx context.Context, cfg *RuntimeIdentityConfig, setenv func(string, string) error, uid, gid int, inContainer bool) error {
	configDir, err := configureRuntimeDockerConfigEnv(cfg, setenv, uid, gid, inContainer)
	if err != nil {
		return err
	}
	if configDir == "" {
		return nil
	}

	configAbs, err := filepath.Abs(configDir)
	if err != nil {
		return fmt.Errorf("resolve docker config directory: %w", err)
	}
	if mkdirAllErr := (acfs.Host{}).MkdirAll(ctx, configAbs, fs.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("create docker config directory: %w", mkdirAllErr)
	}

	if configDir == defaultDockerConfigDir && os.Geteuid() == 0 {
		if chownErr := (acfs.Host{}).Chown(ctx, configDir, uid, gid); chownErr != nil {
			return fmt.Errorf("chown docker config directory: %w", chownErr)
		}
	}

	return nil
}

func configureRuntimeDockerConfigEnv(cfg *RuntimeIdentityConfig, setenv func(string, string) error, uid, gid int, inContainer bool) (string, error) {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	if uid == 0 && gid == 0 {
		// Both UID and GID are root; Docker uses /root/.docker by default.
		return "", nil
	}

	configDir := strings.TrimSpace(cfg.DockerConfig)
	if configDir != "" {
		return configDir, nil
	}

	if !inContainer {
		return "", nil
	}

	configDir = defaultDockerConfigDir
	cfg.DockerConfig = configDir
	if err := setenv("DOCKER_CONFIG", configDir); err != nil {
		return "", fmt.Errorf("set DOCKER_CONFIG: %w", err)
	}

	return configDir, nil
}

func runtimeIdentitySupplementaryGroups(ctx context.Context, dockerHost string, socketGroup func(context.Context, string) mo.Option[uint32]) []uint32 {
	socketPath := defaultDockerSocketPath
	if value := strings.TrimSpace(dockerHost); value != "" {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "unix" {
			return nil
		}
		socketPath = cmp.Or(parsed.Host+parsed.Path, parsed.Opaque)
		if socketPath == "" {
			return nil
		}
		socketPath = filepath.Clean("/" + strings.TrimPrefix(socketPath, "/"))
	}

	socketGID, ok := socketGroup(ctx, socketPath).Get()
	if !ok {
		return nil
	}
	return []uint32{socketGID}
}

func prepareWritablePathsWithRoots(ctx context.Context, uid, gid int, mountpoints map[string]struct{}, projectsDir, dataDirectory, buildsDirectory string) error {
	if err := (acfs.Host{}).MkdirAll(ctx, dataDirectory, fs.DirPerm); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	if err := (acfs.Host{}).Chown(ctx, dataDirectory, uid, gid); err != nil {
		return fmt.Errorf("chown data directory: %w", err)
	}

	entries, err := acfs.Host{}.List(ctx, dataDirectory)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}

	for _, entry := range entries {
		entryPath := entry.Path
		if _, mounted := mountpoints[entryPath]; mounted {
			if lchownErr := (acfs.Host{}).Lchown(ctx, entryPath, uid, gid); lchownErr != nil {
				return fmt.Errorf("chown mounted %s: %w", entryPath, lchownErr)
			}
			continue
		}
		if projectsDir != "" && filepath.Clean(entryPath) == projectsDir {
			if lchownFnErr := lchownFn(ctx, entryPath, uid, gid); lchownFnErr != nil {
				return fmt.Errorf("chown %s: %w", entryPath, lchownFnErr)
			}
			continue
		}
		if chownRecursiveErr := chownRecursive(ctx, entryPath, uid, gid, mountpoints, projectsDir); chownRecursiveErr != nil {
			return fmt.Errorf("chown %s: %w", entryPath, chownRecursiveErr)
		}
	}

	if _, mounted := mountpoints[buildsDirectory]; mounted {
		if lchownErr2 := (acfs.Host{}).Lchown(ctx, buildsDirectory, uid, gid); lchownErr2 != nil {
			return fmt.Errorf("chown mounted builds directory: %w", lchownErr2)
		}
		return nil
	}

	if _, statErr := (acfs.Host{}).Stat(ctx, buildsDirectory, acfstypes.StatOptions{FollowFinal: true}); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat builds directory: %w", statErr)
	}

	if chownRecursiveErr2 := chownRecursive(ctx, buildsDirectory, uid, gid, mountpoints, projectsDir); chownRecursiveErr2 != nil {
		return fmt.Errorf("chown builds directory: %w", chownRecursiveErr2)
	}

	return nil
}

func ensureSQLiteFilesExist(ctx context.Context, databaseURL string) error {
	sqlitePath, ok, err := sqliteDatabasePath(databaseURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	sqliteAbs, err := filepath.Abs(sqlitePath)
	if err != nil {
		return fmt.Errorf("resolve sqlite file %s: %w", sqlitePath, err)
	}
	if mkdirAllErr := (acfs.Host{}).MkdirAll(ctx, filepath.Dir(sqliteAbs), fs.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("create sqlite directory %s: %w", filepath.Dir(sqlitePath), mkdirAllErr)
	}

	file, err := acfs.Host{}.OpenFile(ctx, sqliteAbs, acfs.OpenOptions{
		Mode:   fs.FilePerm,
		Read:   true,
		Write:  true,
		Create: true,
	})
	if err != nil {
		return fmt.Errorf("create sqlite file %s: %w", sqlitePath, err)
	}
	if closeErr := file.Close(); closeErr != nil {
		return fmt.Errorf("close sqlite file %s: %w", sqlitePath, closeErr)
	}

	return nil
}

func sqliteDatabasePath(databaseURL string) (string, bool, error) {
	value := cmp.Or(strings.TrimSpace(databaseURL), defaultDatabaseURL)
	if !strings.HasPrefix(value, "file:") {
		return "", false, nil
	}

	pathPart, err := kit.SQLitePathFromDSN(value)
	if err != nil {
		return "", false, fmt.Errorf("parse sqlite database url: %w", err)
	}
	if pathPart == "" || strings.HasPrefix(pathPart, ":memory:") {
		return "", false, nil
	}

	return filepath.Clean(pathPart), true, nil
}

func chownRecursive(ctx context.Context, path string, uid, gid int, mountpoints map[string]struct{}, projectsDir string) error {
	entry, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{})
	if err != nil {
		return err
	}
	if lchownFnErr := lchownFn(ctx, path, uid, gid); lchownFnErr != nil {
		return lchownFnErr
	}
	if !entry.IsDirectory || (projectsDir != "" && filepath.Clean(path) == projectsDir) {
		return nil
	}

	_, err = acfs.Root{Path: path}.Walk(ctx, "/", acfstypes.WalkOptions{}, func(child acfstypes.Entry) error {
		currentPath := filepath.Join(path, filepath.FromSlash(child.Path))
		if _, mounted := mountpoints[currentPath]; mounted {
			return filepath.SkipDir
		}
		if lchownFnErr := lchownFn(ctx, currentPath, uid, gid); lchownFnErr != nil {
			return lchownFnErr
		}
		return kit.Ternary(projectsDir != "" && currentPath == projectsDir, filepath.SkipDir, nil)
	})
	return err
}

func parseMountpoints(data string) map[string]struct{} {
	mountpoints := make(map[string]struct{})

	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		mountpoint := filepath.Clean(unescapeMountInfoPath(fields[4]))
		mountpoints[mountpoint] = struct{}{}
	}

	return mountpoints
}

func unescapeMountInfoPath(path string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(path)
}
