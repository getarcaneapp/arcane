package builds

import (
	"context"
	"io"

	dockerclient "github.com/docker/docker/client"
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
	GetClient() (*dockerclient.Client, error)
}

type RegistryAuthProvider interface {
	GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error)
}

type Builder interface {
	BuildImage(ctx context.Context, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string) (*imagetypes.BuildResult, error)
}

type LogCapture interface {
	io.Writer
	String() string
	Truncated() bool
}
