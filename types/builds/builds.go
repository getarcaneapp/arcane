package builds

import (
	"context"
	"io"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	dockerregistry "github.com/moby/moby/api/types/registry"
	dockerclient "github.com/moby/moby/client"
)

type BuildSettings struct {
	DepotProjectId   string
	DepotToken       string
	BuildProvider    string
	BuildTimeoutSecs int
}

type SettingsProvider interface {
	BuildSettings() BuildSettings
}

type DockerClientProvider interface {
	GetClient() (*dockerclient.Client, error)
}

type RegistryAuthProvider interface {
	GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error)
	GetRegistryAuthForHost(ctx context.Context, registryHost string) (string, error)
	GetAllRegistryAuthConfigs(ctx context.Context) (map[string]dockerregistry.AuthConfig, error)
}

type Builder interface {
	BuildImage(ctx context.Context, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string) (*imagetypes.BuildResult, error)
}

type LogCapture interface {
	io.Writer
	String() string
	Truncated() bool
}
