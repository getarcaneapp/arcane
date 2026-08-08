package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateVolumeWorkspaceHelperSupport(t *testing.T) {
	require.NoError(t, ValidateVolumeWorkspaceHelperSupport("data", nil))
	require.NoError(t, ValidateVolumeWorkspaceHelperSupport("data", map[string]string{"type": "nfs"}))
	require.Error(t, ValidateVolumeWorkspaceHelperSupport("data", map[string]string{"type": "none"}))
	require.Error(t, ValidateVolumeWorkspaceHelperSupport("data", map[string]string{"o": "bind,rw"}))
}
