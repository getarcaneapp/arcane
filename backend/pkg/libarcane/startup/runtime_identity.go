package startup

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
)

const (
	defaultDataDirectory    = "/app/data"
	defaultBuildsDirectory  = "/builds"
	defaultDatabaseURL      = "file:data/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2500)&_txlock=immediate"
	defaultDockerConfigDir  = "/app/data/.docker"
	defaultDockerSocketPath = "/var/run/docker.sock"
	mountInfoPath           = "/proc/self/mountinfo"
)

type runtimeIdentityRequest struct {
	Enabled       bool
	UID           int
	GID           int
	CredentialUID uint32
	CredentialGID uint32
	DockerHost    string
}

// RuntimeIdentityConfig contains the config-backed environment values used to
// switch the process runtime identity before the application initializes.
type RuntimeIdentityConfig struct {
	PUID         string
	PGID         string
	DockerHost   string
	DockerConfig string
	DatabaseURL  string
}

// ApplyRequestedRuntimeIdentity switches the current process to the configured
// runtime UID/GID before the rest of the app initializes.
func ApplyRequestedRuntimeIdentity(ctx context.Context, cfg *RuntimeIdentityConfig) error {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	req, warning, err := loadRuntimeIdentityRequestInternal(cfg)
	if warning != "" {
		fmt.Fprintf(os.Stderr, "Runtime identity warning: %s\n", warning)
	}
	if err != nil || !req.Enabled {
		return err
	}

	runtimeUID := req.UID
	runtimeGID := req.GID

	// Avoid re-execing forever when the requested runtime identity is already active,
	// including explicit root requests such as PUID=0/PGID=0.
	if os.Geteuid() == runtimeUID && os.Getegid() == runtimeGID {
		if err := ensureRuntimeDockerConfigInternal(cfg, os.Setenv, runtimeUID, runtimeGID); err != nil {
			return err
		}
		return ensureSQLiteFilesExistInternal(cfg.DatabaseURL)
	}

	if os.Geteuid() != 0 {
		fmt.Fprintf(os.Stderr, "Runtime identity warning: process is not root (euid=%d), cannot switch to PUID=%d PGID=%d; continuing as current user\n",
			os.Geteuid(), runtimeUID, runtimeGID)
		if err := ensureRuntimeDockerConfigInternal(cfg, os.Setenv, runtimeUID, runtimeGID); err != nil {
			return err
		}
		return ensureSQLiteFilesExistInternal(cfg.DatabaseURL)
	}

	mountpoints, err := loadMountpointsInternal(mountInfoPath)
	if err != nil {
		return fmt.Errorf("load mountpoints: %w", err)
	}

	if err := ensureRuntimeDockerConfigInternal(cfg, os.Setenv, runtimeUID, runtimeGID); err != nil {
		return err
	}

	if err := prepareWritablePathsInternal(runtimeUID, runtimeGID, mountpoints); err != nil {
		return err
	}

	return reexecWithRuntimeIdentityInternal(ctx, req)
}

func loadRuntimeIdentityRequestInternal(cfg *RuntimeIdentityConfig) (runtimeIdentityRequest, string, error) {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	puid := strings.TrimSpace(cfg.PUID)
	pgid := strings.TrimSpace(cfg.PGID)

	if puid == "" && pgid == "" {
		return runtimeIdentityRequest{}, "", nil
	}

	if puid == "" || pgid == "" {
		return runtimeIdentityRequest{}, "PUID and PGID must both be set to enable non-root mode; continuing with default runtime user", nil
	}

	uid, credentialUID, err := parseRuntimeIdentityValueInternal(puid, "PUID")
	if err != nil {
		return runtimeIdentityRequest{}, "", fmt.Errorf("invalid PUID %q: %w", puid, err)
	}

	gid, credentialGID, err := parseRuntimeIdentityValueInternal(pgid, "PGID")
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

func runtimeDockerConfigDirInternal(cfg *RuntimeIdentityConfig) string {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	configDir := strings.TrimSpace(cfg.DockerConfig)
	if configDir != "" {
		return configDir
	}

	return defaultDockerConfigDir
}

func ensureRuntimeDockerConfigInternal(cfg *RuntimeIdentityConfig, setenv func(string, string) error, uid int, gid int) error {
	configDir, err := configureRuntimeDockerConfigEnvInternal(cfg, setenv, uid, gid)
	if err != nil {
		return err
	}
	if configDir == "" {
		return nil
	}

	if err := os.MkdirAll(configDir, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("create docker config directory: %w", err)
	}

	if configDir == defaultDockerConfigDir && os.Geteuid() == 0 {
		if err := os.Chown(configDir, uid, gid); err != nil {
			return fmt.Errorf("chown docker config directory: %w", err)
		}
	}

	return nil
}

func configureRuntimeDockerConfigEnvInternal(cfg *RuntimeIdentityConfig, setenv func(string, string) error, uid int, gid int) (string, error) {
	if cfg == nil {
		cfg = &RuntimeIdentityConfig{}
	}

	if uid == 0 && gid == 0 {
		// Both UID and GID are root; Docker uses /root/.docker by default.
		return "", nil
	}

	configDir := runtimeDockerConfigDirInternal(cfg)
	if strings.TrimSpace(cfg.DockerConfig) == "" {
		cfg.DockerConfig = configDir
		if err := setenv("DOCKER_CONFIG", configDir); err != nil {
			return "", fmt.Errorf("set DOCKER_CONFIG: %w", err)
		}
	}

	return configDir, nil
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

func prepareWritablePathsInternal(uid int, gid int, mountpoints map[string]struct{}) error {
	if err := os.MkdirAll(defaultDataDirectory, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	if err := os.Chown(defaultDataDirectory, uid, gid); err != nil {
		return fmt.Errorf("chown data directory: %w", err)
	}

	entries, err := os.ReadDir(defaultDataDirectory)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(defaultDataDirectory, entry.Name())
		if _, mounted := mountpoints[entryPath]; mounted {
			continue
		}
		if err := chownRecursiveInternal(entryPath, uid, gid, mountpoints); err != nil {
			return fmt.Errorf("chown %s: %w", entryPath, err)
		}
	}

	if _, mounted := mountpoints[defaultBuildsDirectory]; mounted {
		return nil
	}

	if _, err := os.Stat(defaultBuildsDirectory); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat builds directory: %w", err)
	}

	if err := chownRecursiveInternal(defaultBuildsDirectory, uid, gid, mountpoints); err != nil {
		return fmt.Errorf("chown builds directory: %w", err)
	}

	return nil
}

func ensureSQLiteFilesExistInternal(databaseURL string) error {
	sqlitePath, ok, err := sqliteDatabasePathInternal(databaseURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// Ensure the parent directory exists before creating the file.
	// This covers the "already the right user" early-return path where
	// prepareWritablePathsInternal is not called.
	dir := filepath.Dir(sqlitePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, pkgutils.DirPerm); err != nil { //nolint:gosec // path is derived from the configured SQLite DSN, not user input
			return fmt.Errorf("create sqlite directory %s: %w", dir, err)
		}
	}

	file, err := os.OpenFile(sqlitePath, os.O_CREATE|os.O_RDWR, pkgutils.FilePerm) //nolint:gosec // path is derived from the configured SQLite DSN
	if err != nil {
		return fmt.Errorf("create sqlite file %s: %w", sqlitePath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close sqlite file %s: %w", sqlitePath, err)
	}

	return nil
}

func sqliteDatabasePathInternal(databaseURL string) (string, bool, error) {
	value := strings.TrimSpace(databaseURL)
	if value == "" {
		value = defaultDatabaseURL
	}
	if !strings.HasPrefix(value, "file:") {
		return "", false, nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", false, fmt.Errorf("parse sqlite database url: %w", err)
	}

	// For relative URLs like "file:data/arcane.db", url.Parse puts the path in
	// Opaque (without a leading slash). For absolute URLs like "file:/app/data/arcane.db",
	// Opaque is empty and Path contains the absolute path. Only strip the leading
	// slash from the opaque portion to preserve absolute paths.
	var pathPart string
	if parsed.Opaque != "" {
		pathPart = strings.TrimPrefix(parsed.Opaque, "/")
	} else {
		pathPart = parsed.Path
	}

	if pathPart == "" || strings.HasPrefix(pathPart, ":memory:") {
		return "", false, nil
	}

	return filepath.Clean(pathPart), true, nil
}

func chownRecursiveInternal(path string, uid int, gid int, mountpoints map[string]struct{}) error {
	return filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip any sub-tree that is a separate mountpoint.
		if currentPath != path {
			if _, mounted := mountpoints[filepath.Clean(currentPath)]; mounted {
				return filepath.SkipDir
			}
		}
		//nolint:gosec // currentPath comes from fixed container paths under /app/data or /builds
		return os.Lchown(currentPath, uid, gid)
	})
}

func loadMountpointsInternal(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	return parseMountpointsInternal(string(data)), nil
}

func parseMountpointsInternal(data string) map[string]struct{} {
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

		mountpoint := filepath.Clean(unescapeMountInfoPathInternal(fields[4]))
		mountpoints[mountpoint] = struct{}{}
	}

	return mountpoints
}

// unescapeMountInfoPathInternal decodes the kernel's octal escape sequences
// used in /proc/self/mountinfo. The kernel only uses \040 (space), \011 (tab),
// \012 (newline), and \134 (backslash) — no other escape forms appear.
func unescapeMountInfoPathInternal(path string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(path)
}
