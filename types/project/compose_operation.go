package project

import (
	"context"
	"io"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/registry"
	buildtypes "go.getarcane.app/builds/types"
)

// BuildOptions selects Compose services and overrides the configured build provider.
type BuildOptions struct {
	Services []string
	Provider string
	Push     *bool
	Load     *bool
}

// ComposeImageOperations connects Compose preparation to the application's image and build services.
type ComposeImageOperations struct {
	Exists        func(context.Context, string) (bool, error)
	LastTagged    func(context.Context, string) (time.Time, error)
	Pull          func(context.Context, string, io.Writer) error
	Build         func(context.Context, buildtypes.BuildRequest, io.Writer, string) error
	BuildProvider string
}

// ComposeDeployment supplies application actions in deployment order.
type ComposeDeployment struct {
	ProjectID         string
	ProjectPath       string
	Options           *DeployOptions
	DefaultPullPolicy string
	GitOpsManaged     bool
	WaitTimeout       time.Duration
	AuthConfigs       map[string]registry.AuthConfig
	Progress          io.Writer
	PreDeploy         func(context.Context) error
	Load              func(context.Context) (*composetypes.Project, error)
	ResolveImages     func(context.Context) (ComposeImageOperations, error)
	Recover           func(context.Context)
}

// ComposeServiceUpdate supplies an already authorized service update.
type ComposeServiceUpdate struct {
	Project               *composetypes.Project
	Services              []string
	Images                ComposeImageOperations
	Progress              io.Writer
	AuthConfigs           map[string]registry.AuthConfig
	WaitTimeout           time.Duration
	RestoreBeforeMutation func(context.Context)
	Recover               func(context.Context)
}

// ComposeCommands supplies the SDK operations used by the coordinator.
type ComposeCommands struct {
	Stop func(context.Context, *composetypes.Project, []string) error
	Up   func(context.Context, *composetypes.Project, []string, bool, bool, bool, map[string]registry.AuthConfig, time.Duration) error
}

// ComposeCoordinator executes Compose workflows through application collaborators.
type ComposeCoordinator interface {
	Deploy(ctx context.Context, request ComposeDeployment) (*composetypes.Project, error)
	UpdateServices(ctx context.Context, request ComposeServiceUpdate) error
	PullServices(ctx context.Context, model *composetypes.Project, services []string, operations ComposeImageOperations, progress io.Writer) error
	EnsureImagesPresent(ctx context.Context, model *composetypes.Project, progress io.Writer, operations ComposeImageOperations) error
	BuildServices(ctx context.Context, projectID string, model *composetypes.Project, options BuildOptions, progress io.Writer, operations ComposeImageOperations) error
}
