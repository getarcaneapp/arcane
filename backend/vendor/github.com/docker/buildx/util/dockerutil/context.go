package dockerutil

import (
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/docker"
	"github.com/pkg/errors"
)

// GetDockerEndpoint returns docker endpoint meta for given context
func GetDockerEndpoint(dockerCli command.Cli, name string) (*docker.EndpointMeta, error) {
	list, err := dockerCli.ContextStore().List()
	if err != nil {
		return nil, err
	}
	for _, l := range list {
		if l.Name == name {
			epm, err := docker.EndpointFromContext(l)
			if err != nil {
				return nil, err
			}
			return &epm, nil
		}
	}
	return nil, nil
}

// GetCurrentEndpoint returns the current default endpoint value
func GetCurrentEndpoint(dockerCli command.Cli) (string, error) {
	name := dockerCli.CurrentContext()
	if name != "default" {
		return name, nil
	}
	dem, err := GetDockerEndpoint(dockerCli, name)
	if err != nil {
		return "", errors.Errorf("docker endpoint for %q not found", name)
	} else if dem != nil {
		return dem.Host, nil
	}
	return "", nil
}
