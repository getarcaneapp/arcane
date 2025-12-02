package container

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
)

// readCredentials resolves auth-config from the current environment to be
// applied to the container if the `--use-api-socket` flag is set.
//
//   - If a valid "DOCKER_AUTH_CONFIG" env-var is found, and it contains
//     credentials, it's value is used.
//   - If no "DOCKER_AUTH_CONFIG" env-var is found, or it does not contain
//     credentials, it attempts to read from the CLI's credentials store.
//
// It returns an error if either the "DOCKER_AUTH_CONFIG" is incorrectly
// formatted, or when failing to read from the credentials store.
//
// A nil value is returned if neither option contained any credentials.
func readCredentials(dockerCLI config.Provider) (creds map[string]types.AuthConfig, _ error) {
	if v, ok := os.LookupEnv("DOCKER_AUTH_CONFIG"); ok && v != "" {
		// The results are expected to have been unmarshaled the same as
		// when reading from a config-file, which includes decoding the
		// base64-encoded "username:password" into the "UserName" and
		// "Password" fields.
		ac := &configfile.ConfigFile{}
		if err := ac.LoadFromReader(strings.NewReader(v)); err != nil {
			return nil, fmt.Errorf("failed to read credentials from DOCKER_AUTH_CONFIG: %w", err)
		}
		if len(ac.AuthConfigs) > 0 {
			return ac.AuthConfigs, nil
		}
	}

	// Resolve this here for later, ensuring we error our before we create the container.
	creds, err := dockerCLI.ConfigFile().GetAllCredentials()
	if err != nil {
		return nil, fmt.Errorf("resolving credentials failed: %w", err)
	}
	return creds, nil
}
