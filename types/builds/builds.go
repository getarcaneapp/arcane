package builds

import (
	"context"
	"io"

	dockersdkclient "github.com/docker/go-sdk/client"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
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
	GetSDKClient(ctx context.Context) (dockersdkclient.SDKClient, error)
}

type RegistryAuthProvider interface {
	GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error)
	GetRegistryAuthForHost(ctx context.Context, registryHost string) (string, error)
	GetAllRegistryAuthConfigs(ctx context.Context) (map[string]containerregistry.RegistryAuthConfig, error)
}

type Builder interface {
	BuildImage(ctx context.Context, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string) (*imagetypes.BuildResult, error)
}

type LogCapture interface {
	io.Writer
	String() string
	Truncated() bool
}
