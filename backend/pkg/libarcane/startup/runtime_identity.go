package startup

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
)

const (
	defaultDataDirectory    = "/app/data"
	defaultBuildsDirectory  = "/builds"
	defaultDatabaseURL      = "file:data/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2500)&_txlock=immediate"
	defaultDockerSocketPath = "/var/run/docker.sock"
	mountInfoPath           = "/proc/self/mountinfo"
)

type runtimeIdentityRequest struct {
	Enabled bool
	UID     uint32
	GID     uint32
}

// ApplyRequestedRuntimeIdentity switches the current process to the configured
// runtime UID/GID before the rest of the app initializes.
func ApplyRequestedRuntimeIdentity(ctx context.Context) error {
	req, warning, err := loadRuntimeIdentityRequestInternal(os.Getenv)
	if warning != "" {
		fmt.Fprintf(os.Stderr, "Runtime identity warning: %s\n", warning)
	}
	if err != nil || !req.Enabled {
		return err
	}

	runtimeUID := int(req.UID)
	runtimeGID := int(req.GID)

	// Avoid re-execing forever when the requested runtime identity is already active,
	// including explicit root requests such as PUID=0/PGID=0.
	if os.Geteuid() == runtimeUID && os.Getegid() == runtimeGID {
		return ensureSQLiteFilesExistInternal(os.Getenv("DATABASE_URL"))
	}

	if os.Geteuid() != 0 {
		return ensureSQLiteFilesExistInternal(os.Getenv("DATABASE_URL"))
	}

	mountpoints, err := loadMountpointsInternal(mountInfoPath)
	if err != nil {
		return fmt.Errorf("load mountpoints: %w", err)
	}

	if err := prepareWritablePathsInternal(req, mountpoints); err != nil {
		return err
	}

	return reexecWithRuntimeIdentityInternal(ctx, req)
}

func loadRuntimeIdentityRequestInternal(getenv func(string) string) (runtimeIdentityRequest, string, error) {
	puid := strings.TrimSpace(getenv("PUID"))
	pgid := strings.TrimSpace(getenv("PGID"))

	if puid == "" && pgid == "" {
		return runtimeIdentityRequest{}, "", nil
	}

	if puid == "" || pgid == "" {
		return runtimeIdentityRequest{}, "PUID and PGID must both be set to enable non-root mode; continuing with default runtime user", nil
	}

	uid, err := parseRuntimeIdentityValueInternal(puid, "PUID")
	if err != nil {
		return runtimeIdentityRequest{}, "", fmt.Errorf("invalid PUID %q: %w", puid, err)
	}

	gid, err := parseRuntimeIdentityValueInternal(pgid, "PGID")
	if err != nil {
		return runtimeIdentityRequest{}, "", fmt.Errorf("invalid PGID %q: %w", pgid, err)
	}

	return runtimeIdentityRequest{
		Enabled: true,
		UID:     uid,
		GID:     gid,
	}, "", nil
}

func parseRuntimeIdentityValueInternal(raw string, key string) (uint32, error) {
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return uint32(value), nil
}

func runtimeIdentitySupplementaryGroupsInternal(getenv func(string) string, resolveSocketGroup func(string) (uint32, bool)) []uint32 {
	socketPath, ok := dockerSocketPathInternal(getenv("DOCKER_HOST"))
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

func prepareWritablePathsInternal(req runtimeIdentityRequest, mountpoints map[string]struct{}) error {
	uid := int(req.UID)
	gid := int(req.GID)

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
		if err := chownRecursiveInternal(entryPath, uid, gid); err != nil {
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

	if err := chownRecursiveInternal(defaultBuildsDirectory, uid, gid); err != nil {
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

func chownRecursiveInternal(path string, uid int, gid int) error {
	return filepath.Walk(path, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
