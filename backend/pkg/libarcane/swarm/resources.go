package swarm

import (
	"context"
	"maps"
	"slices"
	"strings"

	"emperror.dev/errors"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

type resourceMeta struct {
	ID   string
	Name string
}

type managedFileResourceSpecInternal struct {
	Name           string
	Labels         map[string]string
	Data           []byte
	Driver         string
	DriverOpts     map[string]string
	TemplateDriver string
}

type plannedFileResourceInternal struct {
	Meta       resourceMeta
	BaseName   string
	Hash       string
	Config     composegotypes.FileObjectConfig
	Labels     map[string]string
	Data       []byte
	IsExternal bool
}

type fileResourceAdapterInternal struct {
	ResourceType string
	Inspect      func(context.Context, string) (resourceMeta, error)
	Create       func(context.Context, managedFileResourceSpecInternal) (resourceMeta, error)
	List         func(context.Context, dockerclient.Filters) ([]staleManagedResourceInternal, error)
	Remove       func(context.Context, string) error
}

func planNetworksInternal(project *composegotypes.Project, ns namespace) map[string]string {
	result := make(map[string]string, len(project.Networks))
	for key, config := range project.Networks {
		result[key] = ns.Resolve(key, config.Name)
	}
	return result
}

func planVolumesInternal(project *composegotypes.Project, ns namespace) map[string]string {
	result := make(map[string]string, len(project.Volumes))
	for key, config := range project.Volumes {
		result[key] = ns.Resolve(key, config.Name)
	}
	return result
}

func planConfigsInternal(project *composegotypes.Project, ns namespace) (map[string]plannedFileResourceInternal, error) {
	configs := make(map[string]composegotypes.FileObjectConfig, len(project.Configs))
	for key, config := range project.Configs {
		configs[key] = composegotypes.FileObjectConfig(config)
	}
	return planManagedFileResourcesInternal(configs, ns, project.WorkingDir, project.Environment, "config")
}

func planSecretsInternal(project *composegotypes.Project, ns namespace) (map[string]plannedFileResourceInternal, error) {
	secrets := make(map[string]composegotypes.FileObjectConfig, len(project.Secrets))
	for key, secret := range project.Secrets {
		secrets[key] = composegotypes.FileObjectConfig(secret)
	}
	return planManagedFileResourcesInternal(secrets, ns, project.WorkingDir, project.Environment, "secret")
}

func planManagedFileResourcesInternal(
	resources map[string]composegotypes.FileObjectConfig,
	ns namespace,
	workingDir string,
	environment composegotypes.Mapping,
	resourceType string,
) (map[string]plannedFileResourceInternal, error) {
	result := make(map[string]plannedFileResourceInternal, len(resources))
	for _, key := range slices.Sorted(maps.Keys(resources)) {
		resource := resources[key]
		baseName := ns.Resolve(key, resource.Name)
		plan := plannedFileResourceInternal{
			Meta:       resourceMeta{Name: baseName},
			BaseName:   baseName,
			Config:     resource,
			Labels:     resource.Labels,
			IsExternal: bool(resource.External),
		}
		if resource.External {
			result[key] = plan
			continue
		}
		if resourceType == "config" && resource.Driver != "" {
			return nil, errors.Errorf("config driver %q is not supported by the Docker swarm API", resource.Driver)
		}

		var data []byte
		if resource.Driver == "" {
			resolvedData, err := resolveFileObjectContentInternal(resource, workingDir, environment)
			if err != nil {
				return nil, errors.WrapIff(err, "failed to load %s %s", resourceType, baseName)
			}
			data = resolvedData
		}
		plan.Data = data
		plan.Hash = hashManagedFileResourceInternal(resource, data)
		plan.Meta.Name = managedResourceNameInternal(baseName, plan.Hash)
		result[key] = plan
	}
	return result, nil
}

func ensureSwarmNetworksInternal(ctx context.Context, dockerClient *dockerclient.Client, project *composegotypes.Project, ns namespace, stackLabels map[string]string) (map[string]string, error) {
	result := planNetworksInternal(project, ns)
	for _, key := range slices.Sorted(maps.Keys(project.Networks)) {
		cfg := project.Networks[key]
		networkName := result[key]

		if cfg.External {
			if networkName == "bridge" || networkName == "host" || networkName == "none" {
				continue
			}
			inspected, err := dockerClient.NetworkInspect(ctx, networkName, dockerclient.NetworkInspectOptions{Scope: "swarm"})
			if err != nil {
				return nil, invalidStackErrorInternal(errors.WrapIff(err, "external network %s is unavailable", networkName))
			}
			if inspected.Network.Scope != "swarm" {
				return nil, invalidStackErrorInternal(errors.Errorf("external network %q has scope %q, expected swarm", networkName, inspected.Network.Scope))
			}
			continue
		}

		_, err := dockerClient.NetworkInspect(ctx, networkName, dockerclient.NetworkInspectOptions{Scope: "swarm"})
		if err == nil {
			continue
		}
		if !cerrdefs.IsNotFound(err) {
			return nil, errors.WrapIff(err, "failed to inspect network %s", networkName)
		}

		driver := strings.TrimSpace(cfg.Driver)
		if driver == "" {
			driver = "overlay"
		}

		labels := mergeLabelsInternal(cfg.Labels, stackLabels)
		createOpts := dockerclient.NetworkCreateOptions{
			Driver:     driver,
			Scope:      "swarm",
			EnableIPv4: cfg.EnableIPv4,
			EnableIPv6: cfg.EnableIPv6,
			Internal:   cfg.Internal,
			Attachable: cfg.Attachable,
			Options:    cfg.DriverOpts,
			Labels:     labels,
			IPAM:       convertIPAMInternal(cfg.Ipam),
		}

		if _, err := dockerClient.NetworkCreate(ctx, networkName, createOpts); err != nil {
			return nil, errors.WrapIff(err, "failed to create network %s", networkName)
		}
	}

	return result, nil
}

// ensureSwarmVolumesInternal pre-creates named volumes that carry a driver or
// driver_opts so that Swarm services pick up the correct volume configuration.
// Without this step Docker creates a plain local volume on first use and the
// driver options are silently ignored — the root cause of issue #2376.
func ensureSwarmVolumesInternal(ctx context.Context, dockerClient *dockerclient.Client, project *composegotypes.Project, ns namespace, stackLabels map[string]string) error {
	volumeNameByKey := planVolumesInternal(project, ns)
	for _, key := range slices.Sorted(maps.Keys(project.Volumes)) {
		cfg := project.Volumes[key]
		// External volumes must already exist; nothing to create.
		if cfg.External {
			continue
		}
		// Only act when driver or driver_opts are explicitly set.
		if cfg.Driver == "" && len(cfg.DriverOpts) == 0 {
			continue
		}

		name := volumeNameByKey[key]

		// If the volume already exists, leave it as-is to avoid disrupting
		// running services that may be attached to it.
		if _, err := dockerClient.VolumeInspect(ctx, name, dockerclient.VolumeInspectOptions{}); err == nil {
			continue
		} else if !cerrdefs.IsNotFound(err) {
			return errors.WrapIff(err, "failed to inspect volume %s", name)
		}

		driver := cfg.Driver
		if driver == "" {
			driver = "local"
		}
		labels := mergeLabelsInternal(cfg.Labels, stackLabels)
		if _, err := dockerClient.VolumeCreate(ctx, dockerclient.VolumeCreateOptions{
			Name:       name,
			Driver:     driver,
			DriverOpts: cfg.DriverOpts,
			Labels:     labels,
		}); err != nil {
			return errors.WrapIff(err, "failed to create volume %s", name)
		}
	}
	return nil
}

func configFileResourceAdapterInternal(dockerClient *dockerclient.Client) fileResourceAdapterInternal {
	return fileResourceAdapterInternal{
		ResourceType: "config",
		Inspect: func(ctx context.Context, name string) (resourceMeta, error) {
			configResult, err := dockerClient.ConfigInspect(ctx, name, dockerclient.ConfigInspectOptions{})
			if err != nil {
				return resourceMeta{}, err
			}
			config := configResult.Config
			return resourceMeta{ID: config.ID, Name: config.Spec.Name}, nil
		},
		Create: func(ctx context.Context, resource managedFileResourceSpecInternal) (resourceMeta, error) {
			spec := swarm.ConfigSpec{
				Annotations: swarm.Annotations{Name: resource.Name, Labels: resource.Labels},
				Data:        resource.Data,
			}
			if resource.TemplateDriver != "" {
				spec.Templating = &swarm.Driver{Name: resource.TemplateDriver}
			}
			response, err := dockerClient.ConfigCreate(ctx, dockerclient.ConfigCreateOptions{Spec: spec})
			if err != nil {
				return resourceMeta{}, errors.WrapIff(err, "failed to create config %s", resource.Name)
			}
			return resourceMeta{ID: response.ID, Name: resource.Name}, nil
		},
		List: func(ctx context.Context, filters dockerclient.Filters) ([]staleManagedResourceInternal, error) {
			configsResult, err := dockerClient.ConfigList(ctx, dockerclient.ConfigListOptions{Filters: filters})
			if err != nil {
				return nil, errors.WrapIf(err, "failed to list stack configs")
			}
			return collectStaleManagedResourcesInternal(configsResult.Items, func(cfg swarm.Config) staleManagedResourceInternal {
				return staleManagedResourceInternal{ID: cfg.ID, Name: cfg.Spec.Name}
			}), nil
		},
		Remove: func(ctx context.Context, id string) error {
			_, err := dockerClient.ConfigRemove(ctx, id, dockerclient.ConfigRemoveOptions{})
			return err
		},
	}
}

func secretFileResourceAdapterInternal(dockerClient *dockerclient.Client) fileResourceAdapterInternal {
	return fileResourceAdapterInternal{
		ResourceType: "secret",
		Inspect: func(ctx context.Context, name string) (resourceMeta, error) {
			secretResult, err := dockerClient.SecretInspect(ctx, name, dockerclient.SecretInspectOptions{})
			if err != nil {
				return resourceMeta{}, err
			}
			secret := secretResult.Secret
			return resourceMeta{ID: secret.ID, Name: secret.Spec.Name}, nil
		},
		Create: func(ctx context.Context, resource managedFileResourceSpecInternal) (resourceMeta, error) {
			spec := swarm.SecretSpec{
				Annotations: swarm.Annotations{Name: resource.Name, Labels: resource.Labels},
				Data:        resource.Data,
			}
			if resource.Driver != "" {
				spec.Driver = &swarm.Driver{Name: resource.Driver, Options: resource.DriverOpts}
			}
			if resource.TemplateDriver != "" {
				spec.Templating = &swarm.Driver{Name: resource.TemplateDriver}
			}
			response, err := dockerClient.SecretCreate(ctx, dockerclient.SecretCreateOptions{Spec: spec})
			if err != nil {
				return resourceMeta{}, errors.WrapIff(err, "failed to create secret %s", resource.Name)
			}
			return resourceMeta{ID: response.ID, Name: resource.Name}, nil
		},
		List: func(ctx context.Context, filters dockerclient.Filters) ([]staleManagedResourceInternal, error) {
			secretsResult, err := dockerClient.SecretList(ctx, dockerclient.SecretListOptions{Filters: filters})
			if err != nil {
				return nil, errors.WrapIf(err, "failed to list stack secrets")
			}
			return collectStaleManagedResourcesInternal(secretsResult.Items, func(secret swarm.Secret) staleManagedResourceInternal {
				return staleManagedResourceInternal{ID: secret.ID, Name: secret.Spec.Name}
			}), nil
		},
		Remove: func(ctx context.Context, id string) error {
			_, err := dockerClient.SecretRemove(ctx, id, dockerclient.SecretRemoveOptions{})
			return err
		},
	}
}

func ensurePlannedFileResourcesInternal(
	ctx context.Context,
	plans map[string]plannedFileResourceInternal,
	stackLabels map[string]string,
	adapter fileResourceAdapterInternal,
) (map[string]resourceMeta, error) {
	result := make(map[string]resourceMeta, len(plans))
	for _, key := range slices.Sorted(maps.Keys(plans)) {
		plan := plans[key]
		if plan.IsExternal {
			meta, err := adapter.Inspect(ctx, plan.Meta.Name)
			if err != nil {
				return nil, invalidStackErrorInternal(errors.WrapIff(
					err,
					"external %s %s is unavailable",
					adapter.ResourceType,
					plan.Meta.Name,
				))
			}
			result[key] = meta
			continue
		}

		meta, err := ensureManagedFileResourceInternal(ctx, plan, stackLabels, adapter)
		if err != nil {
			return nil, err
		}
		result[key] = meta
	}
	return result, nil
}

func ensureManagedFileResourceInternal(
	ctx context.Context,
	plan plannedFileResourceInternal,
	stackLabels map[string]string,
	adapter fileResourceAdapterInternal,
) (resourceMeta, error) {
	if meta, err := adapter.Inspect(ctx, plan.Meta.Name); err == nil {
		return meta, nil
	} else if !cerrdefs.IsNotFound(err) {
		return resourceMeta{}, errors.WrapIff(err, "failed to inspect %s %s", adapter.ResourceType, plan.Meta.Name)
	}

	resourceLabels := mergeLabelsInternal(plan.Labels, stackLabels)
	resourceLabels[resourceTypeLabel] = adapter.ResourceType
	resourceLabels[resourceNameLabel] = plan.BaseName
	resourceLabels[resourceHashLabel] = plan.Hash
	return adapter.Create(ctx, managedFileResourceSpecInternal{
		Name:           plan.Meta.Name,
		Labels:         resourceLabels,
		Data:           plan.Data,
		Driver:         plan.Config.Driver,
		DriverOpts:     plan.Config.DriverOpts,
		TemplateDriver: plan.Config.TemplateDriver,
	})
}
