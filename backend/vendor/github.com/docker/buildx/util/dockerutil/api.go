package dockerutil

import (
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/docker"
	dockerclient "github.com/docker/docker/client"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
)

// ClientAPI represents an active docker API object.
type ClientAPI struct {
	dockerclient.APIClient
}

func NewClientAPI(cli command.Cli, ep string) (*ClientAPI, error) {
	ca := &ClientAPI{}

	var dep docker.Endpoint
	dem, err := GetDockerEndpoint(cli, ep)
	if err != nil {
		return nil, err
	} else if dem != nil {
		dep, err = docker.WithTLSData(cli.ContextStore(), ep, *dem)
		if err != nil {
			return nil, err
		}
	} else {
		dep = docker.Endpoint{
			EndpointMeta: docker.EndpointMeta{
				Host: ep,
			},
		}
	}

	clientOpts, err := dep.ClientOpts()
	if err != nil {
		return nil, err
	}

	clientOpts = append(clientOpts, dockerclient.WithTraceOptions(otelhttp.WithPropagators(
		propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	)))

	ca.APIClient, err = dockerclient.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, err
	}

	return ca, nil
}
