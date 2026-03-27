package startup

import (
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRuntimeIdentityRequest(t *testing.T) {
	t.Run("disabled when unset", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(func(string) string { return "" })
		require.NoError(t, err)
		require.Empty(t, warning)
		require.False(t, req.Enabled)
	})

	t.Run("warning when partial config", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(func(key string) string {
			if key == "PUID" {
				return "1001"
			}
			return ""
		})
		require.NoError(t, err)
		require.Contains(t, warning, "PUID and PGID must both be set")
		require.False(t, req.Enabled)
	})

	t.Run("error when invalid numeric value", func(t *testing.T) {
		_, _, err := loadRuntimeIdentityRequestInternal(func(key string) string {
			if key == "PUID" {
				return "abc"
			}
			return "1001"
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid PUID")
	})

	t.Run("error when value exceeds uint32", func(t *testing.T) {
		tooLarge := strconv.FormatUint(uint64(math.MaxUint32)+1, 10)

		_, _, err := loadRuntimeIdentityRequestInternal(func(key string) string {
			if key == "PUID" {
				return tooLarge
			}
			return "1001"
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid PUID")
	})

	t.Run("enabled when both set", func(t *testing.T) {
		req, warning, err := loadRuntimeIdentityRequestInternal(func(key string) string {
			switch key {
			case "PUID":
				return "1001"
			case "PGID":
				return "2001"
			default:
				return ""
			}
		})
		require.NoError(t, err)
		require.Empty(t, warning)
		require.True(t, req.Enabled)
		require.Equal(t, uint32(1001), req.UID)
		require.Equal(t, uint32(2001), req.GID)
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
