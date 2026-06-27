package projects

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetCurrentContainerInspectTargetInternal(t *testing.T) {
	t.Run("prefers detected container id over hostname", func(t *testing.T) {
		target, err := getCurrentContainerInspectTargetInternal(
			func() (string, error) { return "0123456789ab", nil },
			func() (string, error) { return "rpi4", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "0123456789ab", target)
	})

	t.Run("falls back to hostname when container id unavailable", func(t *testing.T) {
		target, err := getCurrentContainerInspectTargetInternal(
			func() (string, error) { return "", errors.New("no container id found") },
			func() (string, error) { return "rpi4", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "rpi4", target)
	})

	t.Run("trims whitespace from detected container id", func(t *testing.T) {
		target, err := getCurrentContainerInspectTargetInternal(
			func() (string, error) { return "  0123456789ab  ", nil },
			func() (string, error) { return "rpi4", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "0123456789ab", target)
	})

	t.Run("returns hostname error when fallback fails", func(t *testing.T) {
		target, err := getCurrentContainerInspectTargetInternal(
			func() (string, error) { return "", errors.New("no container id found") },
			func() (string, error) { return "", errors.New("hostname unavailable") },
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), "hostname unavailable")
		require.Equal(t, "", target)
	})
}
