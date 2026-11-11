package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadEnv(t *testing.T) {
	t.Setenv("ARCANE_TRIVY_NETWORK", "\"arcane-external\"")
	t.Setenv("ARCANE_BASE_SERVER_URL", "")

	env, err := LoadEnv()
	require.NoError(t, err)
	require.Equal(t, "arcane-external", env["trivyNetwork"])
	require.NotContains(t, env, "baseServerUrl")
	require.Equal(t, "arcane-external", env.ValueOrDefault("trivyNetwork"))
	require.Equal(t, "http://localhost", env.ValueOrDefault("baseServerUrl"))
}

func TestLoadEnvJoinsInvalidValues(t *testing.T) {
	t.Setenv("ARCANE_ENABLE_GRAVATAR", "yes")
	t.Setenv("ARCANE_AUTO_HEAL_INTERVAL", "not a cron")
	t.Setenv("ARCANE_TRIVY_NETWORK", "arcane-external")

	env, err := LoadEnv()
	require.Nil(t, env)
	require.ErrorContains(t, err, "ARCANE_ENABLE_GRAVATAR")
	require.ErrorContains(t, err, "ARCANE_AUTO_HEAL_INTERVAL")
	require.NotContains(t, err.Error(), "ARCANE_TRIVY_NETWORK")
}

func TestProjectsDirectoryContainerPaths(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr string
		want    []string
	}{
		{name: "empty", value: ""},
		{name: "empty list", value: "[]", want: []string{}},
		{
			name:  "absolute and mapped entries",
			value: `[{"path":"/srv/compose/","enabled":true},{"path":"/opt/stacks:/host/stacks","enabled":false}]`,
			want:  []string{"/srv/compose", "/opt/stacks"},
		},
		{name: "windows drive path", value: `[{"path":"C:/compose","enabled":true}]`, want: []string{"C:/compose"}},
		{name: "invalid json", value: `{"path":"/srv"}`, wantErr: "JSON array"},
		{name: "relative path", value: `[{"path":"compose","enabled":true}]`, wantErr: "absolute"},
		{name: "relative mapping", value: `[{"path":"compose:/host","enabled":true}]`, wantErr: "absolute"},
		{name: "blank path", value: `[{"path":" ","enabled":true}]`, wantErr: "absolute"},
		{name: "duplicate after cleaning", value: `[{"path":"/srv/a","enabled":true},{"path":"/srv/a/","enabled":false}]`, wantErr: "more than once"},
		{name: "nested entries", value: `[{"path":"/srv","enabled":true},{"path":"/srv/a","enabled":true}]`, wantErr: "overlap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := projectsDirectoryContainerPaths(tt.value)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadEnvRejectsInvalidProjectsDirectories(t *testing.T) {
	t.Setenv("ARCANE_PROJECTS_DIRECTORIES", `[{"path":"relative","enabled":true}]`)

	env, err := LoadEnv()
	require.Nil(t, env)
	require.ErrorContains(t, err, "ARCANE_PROJECTS_DIRECTORIES")
}
