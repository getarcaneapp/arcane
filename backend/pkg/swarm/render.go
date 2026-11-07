package swarm

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	StackRenderOptions struct {
		PathMapper      *projects.PathMapper
		Name            string
		ComposeContent  string
		OverrideContent string
		EnvContent      string
		WorkingDir      string
	}

	StackRenderResult struct {
		Name            string
		RenderedCompose string
		Services        []string
		Networks        []string
		Volumes         []string
		Configs         []string
		Secrets         []string
	}
)

func RenderStackConfig(ctx context.Context, opts StackRenderOptions) (*StackRenderResult, error) {
	stackName := strings.TrimSpace(opts.Name)
	if err := ValidateStackName(stackName); err != nil {
		return nil, invalidStackError(err)
	}
	ns := namespace{name: stackName}

	project, err := projects.LoadComposeProjectFromContent(ctx, types.ComposeContentOptions{
		ProjectName:     stackName,
		ComposeContent:  opts.ComposeContent,
		OverrideContent: opts.OverrideContent,
		EnvContent:      opts.EnvContent,
		WorkingDir:      opts.WorkingDir,
		PathMapper:      opts.PathMapper,
	})
	if err != nil {
		return nil, invalidStackError(err)
	}
	if project.Name == "" {
		project.Name = stackName
	}

	configMetaByKey, err := planConfigs(project, ns)
	if err != nil {
		return nil, invalidStackError(err)
	}
	secretMetaByKey, err := planSecrets(project, ns)
	if err != nil {
		return nil, invalidStackError(err)
	}

	rendered, err := project.MarshalYAML()
	if err != nil {
		return nil, fmt.Errorf("failed to render compose project: %w", err)
	}

	serviceNames := make([]string, 0, len(project.Services))
	for _, key := range slices.Sorted(maps.Keys(project.Services)) {
		service := project.Services[key]
		name := cmp.Or(service.Name, key)
		serviceNames = append(serviceNames, ns.Scope(name))
	}

	return &StackRenderResult{
		Name:            stackName,
		RenderedCompose: string(rendered),
		Services:        serviceNames,
		Networks:        sortedStringValues(planNetworks(project, ns)),
		Volumes:         sortedStringValues(planVolumes(project, ns)),
		Configs:         sortedPlannedResourceNames(configMetaByKey),
		Secrets:         sortedPlannedResourceNames(secretMetaByKey),
	}, nil
}

func sortedStringValues(valuesByKey map[string]string) []string {
	values := slices.Collect(maps.Values(valuesByKey))
	slices.Sort(values)
	return values
}

func sortedPlannedResourceNames(plansByKey map[string]plannedFileResource) []string {
	names := make([]string, 0, len(plansByKey))
	for _, plan := range plansByKey {
		names = append(names, plan.Meta.Name)
	}
	slices.Sort(names)
	return names
}
