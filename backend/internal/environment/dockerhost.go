package environment

import (
	"fmt"
	"net/url"
	"strings"

	"getarcane.dev/arcane/v3/internal/common"
)

// invalidTargetError reports invalid environment connection settings as an HTTP 400 instead of a 500.
type invalidTargetError struct{ msg string }

// dockerHostSchemes are the Docker API endpoint schemes the manager can talk to directly.
// ssh:// is deliberately absent: moby's ssh transport shells out to a local `ssh` binary the
// manager image does not ship, so accepting it would only defer the failure to request time.
var dockerHostSchemes = map[string]struct{}{
	"tcp":   {},
	"http":  {},
	"https": {},
	"unix":  {},
	"npipe": {},
}

func (e *invalidTargetError) Error() string { return e.msg }

// Unwrap maps the error onto the shared bad-request sentinel so handlers answer 400.
func (e *invalidTargetError) Unwrap() error { return common.ErrBadRequest }

func invalidTargetErrorf(format string, args ...any) error {
	return &invalidTargetError{msg: fmt.Sprintf(format, args...)}
}

// NormalizeDockerHost validates a Docker API endpoint for a docker-host environment.
// An empty value clears the docker-host binding and returns "".
func NormalizeDockerHost(value string) (string, error) {
	host := strings.TrimSpace(value)
	if host == "" {
		return "", nil
	}

	parsed, err := url.Parse(host)
	if err != nil {
		return "", invalidTargetErrorf("invalid Docker host %q: %v", host, err)
	}
	if _, ok := dockerHostSchemes[parsed.Scheme]; !ok {
		if parsed.Scheme == "ssh" {
			return "", invalidTargetErrorf("Docker host %q uses ssh, which the manager cannot tunnel by itself; forward the port and use tcp:// instead", host)
		}
		return "", invalidTargetErrorf("unsupported Docker host scheme %q (use tcp://, http://, https://, unix:// or npipe://)", parsed.Scheme)
	}
	if parsed.User != nil {
		return "", invalidTargetErrorf("Docker host %q must not embed credentials; the manager does not forward them", host)
	}
	if parsed.Scheme == "unix" || parsed.Scheme == "npipe" {
		if parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
			return "", invalidTargetErrorf("Docker host %q must use an absolute socket path, for example unix:///var/run/docker.sock", host)
		}
		return host, nil
	}
	if parsed.Host == "" {
		return "", invalidTargetErrorf("Docker host %q must include a host and port", host)
	}
	if parsed.Port() == "" {
		return "", invalidTargetErrorf("Docker host %q must include a port, for example tcp://192.168.2.33:2375", host)
	}
	return host, nil
}

// validateEnvironmentTargetInternal rejects ambiguous or malformed connection settings on create.
// An environment points at either an agent API URL or a Docker API endpoint, never both; an agent
// environment needs no API URL here because create generates one when the caller omits it.
func validateEnvironmentTargetInternal(env *Environment) error {
	host := env.DockerHostURL()
	if host == "" {
		return nil
	}

	normalized, err := NormalizeDockerHost(host)
	if err != nil {
		return err
	}
	env.DockerHost = &normalized

	if strings.TrimSpace(env.ApiUrl) != "" {
		return invalidTargetErrorf("a Docker host environment cannot also have an API URL")
	}
	if env.AccessToken != nil && strings.TrimSpace(*env.AccessToken) != "" {
		return invalidTargetErrorf("a Docker host environment cannot use an agent access token")
	}
	return nil
}

// applyDockerHostUpdateInternal normalizes a docker_host entry in an update map and keeps the agent
// binding consistent with it. Switching an environment to a docker host drops the agent URL, token
// and key binding, since the manager talks to the daemon directly and no agent is involved; an
// environment that keeps its docker host must not gain an agent URL or token.
func applyDockerHostUpdateInternal(updates map[string]any, current *Environment) error {
	raw, hasDockerHost := updates["docker_host"]
	currentHost := current.DockerHostURL()

	if !hasDockerHost {
		if currentHost == "" {
			// The environment stays agent-backed: the update must not blank the API URL, which
			// would leave it without any target at all.
			if apiURL, isString := updates["api_url"].(string); isString && strings.TrimSpace(apiURL) == "" {
				return invalidTargetErrorf("either an API URL or a Docker host is required")
			}
			return nil
		}
		if apiURL, isString := updates["api_url"].(string); isString && strings.TrimSpace(apiURL) != "" {
			return invalidTargetErrorf("a Docker host environment cannot also have an API URL")
		}
		if _, ok := updates["api_url"]; !ok && strings.TrimSpace(current.ApiUrl) != "" {
			return invalidTargetErrorf("a Docker host environment cannot also have an API URL")
		}
		if token, isString := updates["access_token"].(string); isString && strings.TrimSpace(token) != "" {
			return invalidTargetErrorf("a Docker host environment cannot use an agent access token")
		}
		if _, ok := updates["access_token"]; !ok && current.AccessToken != nil && strings.TrimSpace(*current.AccessToken) != "" {
			return invalidTargetErrorf("a Docker host environment cannot use an agent access token")
		}
		return nil
	}

	host, _ := raw.(string)
	normalized, err := NormalizeDockerHost(host)
	if err != nil {
		return err
	}
	updates["docker_host"] = normalized

	if normalized == "" {
		// Clearing the binding needs an agent target to fall back to.
		apiURL, _ := updates["api_url"].(string)
		if strings.TrimSpace(apiURL) == "" && strings.TrimSpace(current.ApiUrl) == "" {
			return invalidTargetErrorf("clearing the Docker host requires an API URL")
		}
		return nil
	}

	// The manager talks to this daemon directly, so no agent may stay bound to the environment.
	if apiURL, isString := updates["api_url"].(string); isString && strings.TrimSpace(apiURL) != "" {
		return invalidTargetErrorf("a Docker host environment cannot also have an API URL")
	}
	if token, isString := updates["access_token"].(string); isString && strings.TrimSpace(token) != "" {
		return invalidTargetErrorf("a Docker host environment cannot use an agent access token")
	}
	updates["api_url"] = ""
	updates["access_token"] = ""
	updates["api_key_id"] = nil
	return nil
}
