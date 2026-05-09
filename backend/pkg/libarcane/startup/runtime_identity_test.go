package startup

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRuntimeIdentityRequest(t *testing.T) {
	t.Run("disabled when unset", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{})
		require.NoError(t, err)
		require.Empty(t, warning)
		require.False(t, req.Enabled)
	})

	t.Run("warning when partial config", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{PUID: "1001"})
		require.NoError(t, err)
		require.Contains(t, warning, "PUID and PGID must both be set")
		require.False(t, req.Enabled)
	})

	t.Run("error when invalid numeric value", func(t *testing.T) {
		_, _, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{PUID: "abc", PGID: "1001"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid PUID")
	})

	t.Run("error when value exceeds uint32", func(t *testing.T) {
		tooLarge := strconv.FormatUint(uint64(math.MaxUint32)+1, 10)

		_, _, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{PUID: tooLarge, PGID: "1001"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid PUID")
	})

	t.Run("enabled when both set", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{
			PUID:       "1001",
			PGID:       "2001",
			DockerHost: "unix:///tmp/docker.sock",
		})
		require.NoError(t, err)
		require.Empty(t, warning)
		require.True(t, req.Enabled)
		require.Equal(t, 1001, req.UID)
		require.Equal(t, 2001, req.GID)
		require.Equal(t, uint32(1001), req.CredentialUID)
		require.Equal(t, uint32(2001), req.CredentialGID)
		require.Equal(t, "unix:///tmp/docker.sock", req.DockerHost)
	})

	t.Run("error when value is negative", func(t *testing.T) {
		_, _, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{PUID: "-1", PGID: "1001"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid PUID")
	})
}

func TestRuntimeDockerConfigDir(t *testing.T) {
	t.Run("defaults to app data docker config when unset", func(t *testing.T) {
		dir := runtimeDockerConfigDirInternal(&RuntimeIdentityConfig{})

		require.Equal(t, defaultDockerConfigDir, dir)
	})

	t.Run("preserves explicit docker config", func(t *testing.T) {
		dir := runtimeDockerConfigDirInternal(&RuntimeIdentityConfig{DockerConfig: "/custom/docker-config"})

		require.Equal(t, "/custom/docker-config", dir)
	})

	t.Run("root default runtime leaves runtime identity disabled", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(&RuntimeIdentityConfig{})

		require.NoError(t, err)
		require.Empty(t, warning)
		require.False(t, req.Enabled)
	})
}

func TestConfigureRuntimeDockerConfigEnv(t *testing.T) {
	t.Run("sets default for non root runtime when unset", func(t *testing.T) {
		cfg := &RuntimeIdentityConfig{}
		env := map[string]string{}

		configDir, err := configureRuntimeDockerConfigEnvInternal(
			cfg,
			func(key string, value string) error {
				env[key] = value
				return nil
			},
			1001,
			1001,
		)

		require.NoError(t, err)
		require.Equal(t, defaultDockerConfigDir, configDir)
		require.Equal(t, defaultDockerConfigDir, env["DOCKER_CONFIG"])
		require.Equal(t, defaultDockerConfigDir, cfg.DockerConfig)
	})

	t.Run("preserves explicit docker config for non root runtime", func(t *testing.T) {
		cfg := &RuntimeIdentityConfig{DockerConfig: "/custom/docker-config"}
		env := map[string]string{}
		setCalled := false

		configDir, err := configureRuntimeDockerConfigEnvInternal(
			cfg,
			func(key string, value string) error {
				setCalled = true
				env[key] = value
				return nil
			},
			1001,
			1001,
		)

		require.NoError(t, err)
		require.Equal(t, "/custom/docker-config", configDir)
		require.Empty(t, env["DOCKER_CONFIG"])
		require.Equal(t, "/custom/docker-config", cfg.DockerConfig)
		require.False(t, setCalled)
	})

	t.Run("skips explicit root runtime", func(t *testing.T) {
		cfg := &RuntimeIdentityConfig{}
		env := map[string]string{}
		setCalled := false

		configDir, err := configureRuntimeDockerConfigEnvInternal(
			cfg,
			func(key string, value string) error {
				setCalled = true
				env[key] = value
				return nil
			},
			0,
			0,
		)

		require.NoError(t, err)
		require.Empty(t, configDir)
		require.Empty(t, env["DOCKER_CONFIG"])
		require.Empty(t, cfg.DockerConfig)
		require.False(t, setCalled)
	})

	t.Run("sets default for mixed root uid non root gid runtime", func(t *testing.T) {
		cfg := &RuntimeIdentityConfig{}
		env := map[string]string{}

		configDir, err := configureRuntimeDockerConfigEnvInternal(
			cfg,
			func(key string, value string) error {
				env[key] = value
				return nil
			},
			0,
			1001,
		)

		require.NoError(t, err)
		require.Equal(t, defaultDockerConfigDir, configDir)
		require.Equal(t, defaultDockerConfigDir, env["DOCKER_CONFIG"])
		require.Equal(t, defaultDockerConfigDir, cfg.DockerConfig)
	})
}

func TestEnsureRuntimeDockerConfigCreatesCustomDirectory(t *testing.T) {
	customDir := filepath.Join(t.TempDir(), "custom-docker-config")
	cfg := &RuntimeIdentityConfig{DockerConfig: customDir}

	require.NoError(t, ensureRuntimeDockerConfigInternal(
		cfg,
		func(string, string) error {
			t.Fatal("setenv should not be called for explicit DOCKER_CONFIG")
			return nil
		},
		1001,
		1001,
	))

	info, err := os.Stat(customDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, customDir, cfg.DockerConfig)
}

func TestRuntimeIdentitySupplementaryGroups(t *testing.T) {
	t.Run("maps default docker socket group when docker host unset", func(t *testing.T) {
		groups := runtimeIdentitySupplementaryGroupsInternal(
			"",
			func(socketPath string) (uint32, bool) {
				require.Equal(t, defaultDockerSocketPath, socketPath)
				return 997, true
			},
		)

		require.Equal(t, []uint32{997}, groups)
	})

	t.Run("maps custom unix docker host socket group", func(t *testing.T) {
		groups := runtimeIdentitySupplementaryGroupsInternal(
			"unix:///tmp/docker.sock",
			func(socketPath string) (uint32, bool) {
				require.Equal(t, "/tmp/docker.sock", socketPath)
				return 998, true
			},
		)

		require.Equal(t, []uint32{998}, groups)
	})

	t.Run("skips non unix docker host", func(t *testing.T) {
		called := false

		groups := runtimeIdentitySupplementaryGroupsInternal(
			"tcp://docker:2375",
			func(string) (uint32, bool) {
				called = true
				return 0, false
			},
		)

		require.Nil(t, groups)
		require.False(t, called)
	})

	t.Run("skips socket group when socket lookup fails", func(t *testing.T) {
		groups := runtimeIdentitySupplementaryGroupsInternal(
			"unix:///tmp/missing.sock",
			func(string) (uint32, bool) { return 0, false },
		)

		require.Nil(t, groups)
	})
}

func TestParseMountpoints(t *testing.T) {
	data := `36 25 0:32 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
97 92 0:44 / /app/data rw,relatime - ext4 /dev/sda1 rw
98 92 0:45 / /app/data/projects rw,relatime - ext4 /dev/sdb1 rw
99 92 0:46 / /builds rw,relatime - ext4 /dev/sdc1 rw
`

	parsed := parseMountpointsInternal(data)
	require.Contains(t, parsed, "/app/data")
	require.Contains(t, parsed, "/app/data/projects")
	require.Contains(t, parsed, "/builds")
}

func TestSQLiteDatabasePath(t *testing.T) {
	t.Run("uses default sqlite path when unset", func(t *testing.T) {
		path, ok, err := sqliteDatabasePathInternal("")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, "data/arcane.db", path)
	})

	t.Run("preserves absolute sqlite path", func(t *testing.T) {
		path, ok, err := sqliteDatabasePathInternal("file:/app/custom/arcane.db?_pragma=journal_mode(WAL)")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, "/app/custom/arcane.db", path)
	})

	t.Run("returns relative sqlite path", func(t *testing.T) {
		path, ok, err := sqliteDatabasePathInternal("file:data/arcane.db?_pragma=journal_mode(WAL)")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, "data/arcane.db", path)
	})

	t.Run("skips non sqlite database urls", func(t *testing.T) {
		path, ok, err := sqliteDatabasePathInternal("postgres://arcane:secret@db/arcane")
		require.NoError(t, err)
		require.False(t, ok)
		require.Empty(t, path)
	})
}

func TestEnsureSQLiteFilesExistInternal(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "arcane.db")

	require.NoError(t, ensureSQLiteFilesExistInternal("file:"+dbPath))

	require.FileExists(t, dbPath)
	require.NoFileExists(t, dbPath+"-wal")
	require.NoFileExists(t, dbPath+"-shm")
}

func TestEnsureSQLiteFilesExistInternalCreatesParentDir(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "subdir", "arcane.db")

	require.NoError(t, ensureSQLiteFilesExistInternal("file:"+dbPath))

	require.FileExists(t, dbPath)
}

func TestUnescapeMountInfoPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"space", `/mnt/my\040dir`, "/mnt/my dir"},
		{"tab", `/mnt/my\011dir`, "/mnt/my\tdir"},
		{"newline", `/mnt/my\012dir`, "/mnt/my\ndir"},
		{"backslash", `/mnt/my\134dir`, `/mnt/my\dir`},
		{"no escapes", `/mnt/simple`, "/mnt/simple"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expect, unescapeMountInfoPathInternal(tt.input))
		})
	}
}
