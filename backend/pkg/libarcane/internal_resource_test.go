package libarcane

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"emperror.dev/errors"
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
	// With network_mode: service:<sidecar> both the hostname and the cgroup
	// mountinfo fallback resolve the sidecar container (no Arcane label);
	// detection must fall back to the label lookup instead of trusting the
	// derived inspect (#3544, #3693).
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(`[{"Id":"arcane123","Names":["/arcane"],"State":"running","Labels":{"com.getarcaneapp.arcane":"true"}}]`))
		case strings.HasSuffix(r.URL.Path, "/containers/arcane123/json"):
			_, _ = w.Write([]byte(`{"Id":"arcane123","Name":"/arcane","State":{"Running":true},"HostConfig":{"NetworkMode":"container:sidecar456"},"Config":{"Labels":{"com.getarcaneapp.arcane":"true"}}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			_, _ = w.Write([]byte(`{"Id":"sidecar456","Name":"/ts-arcane","State":{"Running":true},"Config":{"Labels":{}}}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	got, err := InspectCurrentArcaneContainer(context.Background(), dockerClient)
	require.NoError(t, err)
	assert.Equal(t, "arcane123", got.ID)
}

func TestInspectCurrentArcaneContainer_KeepsUnlabeledHitOverUnrelatedLabeledInstance(t *testing.T) {
	// The hostname resolves to the actual (unlabeled custom-image) Arcane
	// container; a second, labeled Arcane instance also runs on the daemon but
	// does not share the hostname container's network namespace, so it must not
	// hijack detection.
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(`[{"Id":"other789","Names":["/arcane-other"],"State":"running","Labels":{"com.getarcaneapp.arcane":"true"}}]`))
		case strings.HasSuffix(r.URL.Path, "/containers/other789/json"):
			_, _ = w.Write([]byte(`{"Id":"other789","Name":"/arcane-other","State":{"Running":true},"HostConfig":{"NetworkMode":"bridge"},"Config":{"Labels":{"com.getarcaneapp.arcane":"true"}}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			_, _ = w.Write([]byte(`{"Id":"custom123","Name":"/arcane-custom","State":{"Running":true},"Config":{"Labels":{}}}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	got, err := InspectCurrentArcaneContainer(context.Background(), dockerClient)
	require.NoError(t, err)
	assert.Equal(t, "custom123", got.ID)
}
