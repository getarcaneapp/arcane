package docker

import (
	"strings"

	"emperror.dev/errors"
)

// ValidateVolumeWorkspaceHelperSupport rejects Docker volume configurations
// that cannot be mounted safely inside Arcane's reusable workspace helper.
func ValidateVolumeWorkspaceHelperSupport(volumeName string, options map[string]string) error {
	if options["type"] == "none" || strings.Contains(options["o"], "bind") {
		return errors.Errorf("volume %q uses a custom mount configuration and cannot be accessed through the workspace helper", volumeName)
	}
	return nil
}
