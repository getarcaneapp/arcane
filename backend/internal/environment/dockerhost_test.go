package environment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/common"
)

func TestNormalizeDockerHost(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{name: "blank clears the binding", value: "   ", want: ""},
		{name: "tcp endpoint", value: " tcp://192.168.2.33:2375 ", want: "tcp://192.168.2.33:2375"},
		{name: "https endpoint", value: "https://docker.example:2376", want: "https://docker.example:2376"},
		{name: "unix socket", value: "unix:///var/run/docker.sock", want: "unix:///var/run/docker.sock"},
		{name: "rejects ssh", value: "ssh://user@host", wantErr: "uses ssh"},
		{name: "rejects unknown scheme", value: "ftp://host:21", wantErr: "unsupported Docker host scheme"},
		{name: "rejects credentials", value: "tcp://user:pass@host:2375", wantErr: "must not embed credentials"},
		{name: "rejects missing port", value: "tcp://192.168.2.33", wantErr: "must include a port"},
		{name: "rejects relative socket path", value: "unix://var/run/docker.sock", wantErr: "absolute socket path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeDockerHost(tt.value)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.ErrorIs(t, err, common.ErrBadRequest, "invalid targets must classify as a bad request")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateEnvironmentTargetInternal(t *testing.T) {
	t.Run("docker host environment needs no API URL", func(t *testing.T) {
		env := &Environment{Name: "dh", DockerHost: new(" tcp://192.168.2.33:2375 ")}
		require.NoError(t, validateEnvironmentTargetInternal(env))
		assert.Equal(t, "tcp://192.168.2.33:2375", env.DockerHostURL())
		assert.True(t, env.IsDockerHost())
	})

	t.Run("docker host environment rejects an API URL", func(t *testing.T) {
		env := &Environment{Name: "dh", ApiUrl: "http://agent:3553", DockerHost: new("tcp://192.168.2.33:2375")}
		require.Error(t, validateEnvironmentTargetInternal(env))
	})

	t.Run("docker host environment rejects an agent token", func(t *testing.T) {
		env := &Environment{Name: "dh", AccessToken: new("arc_token"), DockerHost: new("tcp://192.168.2.33:2375")}
		require.Error(t, validateEnvironmentTargetInternal(env))
	})

	t.Run("agent environment needs no explicit API URL", func(t *testing.T) {
		// Create generates an agent endpoint when the caller omits one.
		require.NoError(t, validateEnvironmentTargetInternal(&Environment{Name: "agent"}))
		require.NoError(t, validateEnvironmentTargetInternal(&Environment{Name: "agent", ApiUrl: "http://agent:3553"}))
	})
}

func TestApplyDockerHostUpdateInternal(t *testing.T) {
	agentEnv := &Environment{ApiUrl: "http://agent:3553", AccessToken: new("arc_token"), ApiKeyID: new("key-1")}

	t.Run("switching to a docker host unbinds the agent", func(t *testing.T) {
		updates := map[string]any{"docker_host": " tcp://192.168.2.33:2375 "}
		require.NoError(t, applyDockerHostUpdateInternal(updates, agentEnv))
		assert.Equal(t, "tcp://192.168.2.33:2375", updates["docker_host"])
		assert.Empty(t, updates["api_url"])
		assert.Empty(t, updates["access_token"])
		assert.Nil(t, updates["api_key_id"])
	})

	t.Run("a docker host update cannot smuggle an API URL or token", func(t *testing.T) {
		require.Error(t, applyDockerHostUpdateInternal(map[string]any{
			"docker_host": "tcp://192.168.2.33:2375",
			"api_url":     "http://agent:3553",
		}, agentEnv))
		require.Error(t, applyDockerHostUpdateInternal(map[string]any{
			"docker_host":  "tcp://192.168.2.33:2375",
			"access_token": "arc_token",
		}, agentEnv))
	})

	t.Run("a docker host environment rejects agent bindings", func(t *testing.T) {
		dockerHostEnv := &Environment{DockerHost: new("tcp://192.168.2.33:2375")}
		require.Error(t, applyDockerHostUpdateInternal(map[string]any{"api_url": "http://agent:3553"}, dockerHostEnv))
		require.Error(t, applyDockerHostUpdateInternal(map[string]any{"access_token": "arc_token"}, dockerHostEnv))
		// Blanking the API URL is a no-op for a docker-host environment: the daemon is its target.
		require.NoError(t, applyDockerHostUpdateInternal(map[string]any{"api_url": ""}, dockerHostEnv))
	})

	t.Run("clearing the docker host returns to the agent flow", func(t *testing.T) {
		dockerHostEnv := &Environment{DockerHost: new("tcp://192.168.2.33:2375")}
		updates := map[string]any{"docker_host": "", "api_url": "http://agent:3553"}
		require.NoError(t, applyDockerHostUpdateInternal(updates, dockerHostEnv))
		assert.Empty(t, updates["docker_host"])

		require.Error(t, applyDockerHostUpdateInternal(map[string]any{"docker_host": ""}, dockerHostEnv),
			"clearing the docker host needs an API URL to fall back to")
	})

	t.Run("agent-only updates are untouched", func(t *testing.T) {
		updates := map[string]any{"access_token": "rotated"}
		require.NoError(t, applyDockerHostUpdateInternal(updates, agentEnv))
		assert.Equal(t, "rotated", updates["access_token"])
		require.Error(t, applyDockerHostUpdateInternal(map[string]any{"api_url": ""}, agentEnv),
			"an agent environment must keep an API URL")
	})
}
