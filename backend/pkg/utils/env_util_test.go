package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupEnvOrFile(t *testing.T) {
	const name = "ARCANE_TEST_SECRET"
	writeFile := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}

	tests := []struct {
		name  string
		setup func(t *testing.T)
		want  string
		found bool
	}{
		{
			name: "file wins over the variable and is trimmed",
			setup: func(t *testing.T) {
				t.Setenv(name+"_FILE", writeFile(t, "from-file\n"))
				t.Setenv(name, "from-env")
			},
			want:  "from-file",
			found: true,
		},
		{
			name: "double underscore wins over single",
			setup: func(t *testing.T) {
				t.Setenv(name+"__FILE", writeFile(t, "double"))
				t.Setenv(name+"_FILE", writeFile(t, "single"))
			},
			want:  "double",
			found: true,
		},
		{
			name: "unreadable file falls back to the variable",
			setup: func(t *testing.T) {
				t.Setenv(name+"_FILE", filepath.Join(t.TempDir(), "missing"))
				t.Setenv(name, "from-env")
			},
			want:  "from-env",
			found: true,
		},
		{
			name:  "variable only",
			setup: func(t *testing.T) { t.Setenv(name, "from-env") },
			want:  "from-env",
			found: true,
		},
		{
			name:  "unset",
			setup: func(t *testing.T) {},
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			got, ok, _ := LookupEnvOrFile(name)
			require.Equal(t, tt.found, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
