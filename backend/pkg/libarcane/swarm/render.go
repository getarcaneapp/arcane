package swarm

import (
	"context"
	"maps"
	"slices"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/backend/v2/pkg/projects/types"
)

type StackRenderOptions struct {
	Name            string
	ComposeContent  string
	OverrideContent string
	EnvContent      string
	WorkingDir      string
	PathMapper      *projects.PathMapper
}

type StackRenderResult struct {
	Name            string
	RenderedCompose string
	Services        []string
	Networks        []string
	Volumes         []string
	Configs         []string
	Secrets         []string
}

// RenderStackConfig renders a Compose-defined stack without deploying it.
//
// It validates the stack name, loads and interpolates the Compose project using
// the provided environment content, marshals the normalized project back to
// YAML, and reports the discovered service, network, volume, config, and secret
// names that would participate in deployment.
//
// ctx controls cancellation for Compose loading.
// opts provides the stack name, compose content, and optional env content to render.
//
// Returns the rendered compose YAML and related resource names.
// Returns an error if the stack name is empty, the compose or env content is
// invalid, or the normalized Compose project cannot be marshaled.
func RenderStackConfig(ctx context.Context, opts StackRenderOptions) (*StackRenderResult, error) {
	stackName := strings.TrimSpace(opts.Name)
	if err := validateStackNameInternal(stackName); err != nil {
		return nil, invalidStackErrorInternal(err)
	}
	ns := namespace{name: stackName}

	project, err := projects.LoadComposeProjectFromContent(ctx, projecttypes.ComposeContentOptions{
		ProjectName:     stackName,
		ComposeContent:  opts.ComposeContent,
		OverrideContent: opts.OverrideContent,
		EnvContent:      opts.EnvContent,
		WorkingDir:      opts.WorkingDir,
		PathMapper:      opts.PathMapper,
	})
	if err != nil {
		return nil, invalidStackErrorInternal(err)
	}
	if project.Name == "" {
		project.Name = stackName
	}

	configMetaByKey, err := planConfigsInternal(project, ns)
	if err != nil {
		return nil, invalidStackErrorInternal(err)
	}
	secretMetaByKey, err := planSecretsInternal(project, ns)
	if err != nil {
		return nil, invalidStackErrorInternal(err)
	}

	rendered, err := project.MarshalYAML()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to render compose project")
	}

	serviceNames := make([]string, 0, len(project.Services))
	for _, key := range slices.Sorted(maps.Keys(project.Services)) {
		service := project.Services[key]
		name := service.Name
		if name == "" {
			name = key
		}
		serviceNames = append(serviceNames, ns.Scope(name))
	}

	return &StackRenderResult{
		Name:            stackName,
		RenderedCompose: string(rendered),
		Services:        serviceNames,
		Networks:        sortedStringValuesInternal(planNetworksInternal(project, ns)),
		Volumes:         sortedStringValuesInternal(planVolumesInternal(project, ns)),
		Configs:         sortedPlannedResourceNamesInternal(configMetaByKey),
		Secrets:         sortedPlannedResourceNamesInternal(secretMetaByKey),
	}, nil
}

func sortedStringValuesInternal(valuesByKey map[string]string) []string {
	values := slices.Collect(maps.Values(valuesByKey))
	slices.Sort(values)
	return values
}

func sortedPlannedResourceNamesInternal(plansByKey map[string]plannedFileResourceInternal) []string {
	names := make([]string, 0, len(plansByKey))
	for _, plan := range plansByKey {
		names = append(names, plan.Meta.Name)
	}
	slices.Sort(names)
	return names
}
