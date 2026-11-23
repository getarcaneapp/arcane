package selfcontainer

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentContainerInspectTarget(t *testing.T) {
	t.Run("prefers detected container id over hostname", func(t *testing.T) {
		target, err := CurrentContainerInspectTarget(
			func() (string, error) { return "0123456789ab", nil },
			func() (string, error) { return "rpi4", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "0123456789ab", target)
	})

	t.Run("falls back to hostname when container id unavailable", func(t *testing.T) {
		target, err := CurrentContainerInspectTarget(
			func() (string, error) { return "", errors.New("no container id found") },
			func() (string, error) { return "rpi4", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "rpi4", target)
	})

	t.Run("trims whitespace from detected container id", func(t *testing.T) {
		target, err := CurrentContainerInspectTarget(
			func() (string, error) { return "  0123456789ab  ", nil },
			func() (string, error) { return "rpi4", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "0123456789ab", target)
	})

	t.Run("returns hostname error when fallback fails", func(t *testing.T) {
		target, err := CurrentContainerInspectTarget(
			func() (string, error) { return "", errors.New("no container id found") },
			func() (string, error) { return "", errors.New("hostname unavailable") },
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), "hostname unavailable")
		require.Empty(t, target)
	})
}

func TestInspectCurrentArcaneContainer_FallsBackToLabelWhenHostnameIsSidecar(t *testing.T) {
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(`[{"Id":"arcane123","Names":["/arcane"],"State":"running","Labels":{"com.getarcaneapp.arcane":"true"}}]`))
		case strings.HasSuffix(r.URL.Path, "/containers/arcane123/json"):
			_, _ = w.Write([]byte("{\"Id\":\"arcane123\",\"Name\":\"/arcane\",\"State\":{\"Running\":true},\"HostConfig\":{\"NetworkMode\":\"container:s" +
				"idecar456\"},\"Config\":{\"Labels\":{\"com.getarcaneapp.arcane\":\"true\"}}}"))
		case strings.HasSuffix(r.URL.Path, "/json"):
			_, _ = w.Write([]byte(`{"Id":"sidecar456","Name":"/ts-arcane","State":{"Running":true},"Config":{"Labels":{}}}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	got, err := InspectCurrentArcaneContainer(t.Context(), dockerClient)
	require.NoError(t, err)
	assert.Equal(t, "arcane123", got.ID)
}

func TestInspectCurrentArcaneContainer_KeepsUnlabeledHitOverUnrelatedLabeledInstance(t *testing.T) {
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(`[{"Id":"other789","Names":["/arcane-other"],"State":"running","Labels":{"com.getarcaneapp.arcane":"true"}}]`))
		case strings.HasSuffix(r.URL.Path, "/containers/other789/json"):
			_, _ = w.Write([]byte("{\"Id\":\"other789\",\"Name\":\"/arcane-other\",\"State\":{\"Running\":true},\"HostConfig\":{\"NetworkMode\":\"bridge" +
				"\"},\"Config\":{\"Labels\":{\"com.getarcaneapp.arcane\":\"true\"}}}"))
		case strings.HasSuffix(r.URL.Path, "/json"):
			_, _ = w.Write([]byte(`{"Id":"custom123","Name":"/arcane-custom","State":{"Running":true},"Config":{"Labels":{}}}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	got, err := InspectCurrentArcaneContainer(t.Context(), dockerClient)
	require.NoError(t, err)
	assert.Equal(t, "custom123", got.ID)
}

func TestContainerDetection_TrueNASMountInfoAndSignals(t *testing.T) {
	const fullID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	trueNASMountInfo := "900 880 0:120 /containers/" + fullID + "/resolv.conf /etc/resolv.conf rw,relatime - zfs pool/ix-apps/docker rw\n"

	id, err := currentContainerID(
		func() (string, error) { return "", errors.New("no container ID found") },
		func() (string, error) { return trueNASMountInfo, nil },
	)
	require.NoError(t, err)
	assert.Equal(t, fullID, id)

	noEnv := func(string) string { return "" }
	noPath := func(string) bool { return false }
	noID := func() (string, error) { return "", errors.New("no container ID found") }

	assert.False(t, runningInContainer(noEnv, noPath, noID))
	assert.True(t, runningInContainer(func(k string) string {
		if k == "ARCANE_IN_CONTAINER" {
			return "true"
		}
		return ""
	}, noPath, noID))
	assert.True(t, runningInContainer(noEnv, func(p string) bool { return p == "/.dockerenv" }, noID))
	assert.True(t, runningInContainer(noEnv, noPath, func() (string, error) { return fullID, nil }))
}

func newTestDockerClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	return dockerClient
}
