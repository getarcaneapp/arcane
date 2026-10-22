package swarm

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

// StackDeployOptions controls how a swarm stack is deployed.
type StackDeployOptions struct {
	RegistryAuthForImage func(context.Context, string) (string, error)
	PathMapper           *projects.PathMapper
	Name                 string
	ComposeContent       string
	OverrideContent      string
	EnvContent           string
	ResolveImage         string
	WorkingDir           string
	WithRegistryAuth     bool
	Prune                bool
}

// DeployStack deploys a Compose-defined stack to Docker Swarm using the Engine API.
//
// It validates the stack name, loads the Compose project with environment
// interpolation, ensures required swarm-scoped networks, configs, and secrets
// exist, creates or updates services to match the desired state, optionally
// prunes services no longer declared by the stack, and removes stale managed
// configs and secrets after reconciliation.
//
// ctx controls cancellation for Compose loading and Docker API calls.
// dockerClient must target a swarm manager capable of creating and updating stack resources.
// opts provides the stack name, compose content, optional env content, registry-auth behavior, pruning, and image-resolution mode.
//
// Returns nil when the stack has been reconciled successfully.
// Returns an error if the stack name is empty, the compose or env content is
// invalid, a referenced resource cannot be inspected or created, or any Docker
// API call required to reconcile the stack fails.
func DeployStack(ctx context.Context, dockerClient *dockerclient.Client, opts StackDeployOptions) error {
	stackName := strings.TrimSpace(opts.Name)
	if err := validateStackNameInternal(stackName); err != nil {
		return invalidStackErrorInternal(err)
	}
	ns := namespace{name: stackName}
	resolveMode, err := normalizeResolveImageMode(opts.ResolveImage)
	if err != nil {
		return invalidStackErrorInternal(err)
	}

	project, err := projects.LoadComposeProjectFromContent(ctx, types.ComposeContentOptions{
		ProjectName:     stackName,
		ComposeContent:  opts.ComposeContent,
		OverrideContent: opts.OverrideContent,
		EnvContent:      opts.EnvContent,
		WorkingDir:      opts.WorkingDir,
		PathMapper:      opts.PathMapper,
	})
	if err != nil {
		return invalidStackErrorInternal(err)
	}
	if project.Name == "" {
		project.Name = stackName
	}

	stackLabels := map[string]string{types.SwarmStackNamespaceLabel: stackName}

	networkNameByKey, err := ensureSwarmNetworks(ctx, dockerClient, project, ns, stackLabels)
	if err != nil {
		return err
	}

	if ensureSwarmVolumesErr := ensureSwarmVolumes(ctx, dockerClient, project, ns, stackLabels); ensureSwarmVolumesErr != nil {
		return ensureSwarmVolumesErr
	}

	configPlans, err := planConfigs(project, ns)
	if err != nil {
		return invalidStackErrorInternal(err)
	}
	configMetaByKey, err := ensurePlannedFileResources(
		ctx,
		configPlans,
		stackLabels,
		configFileResourceAdapter(dockerClient),
	)
	if err != nil {
		return err
	}

	secretPlans, err := planSecrets(project, ns)
	if err != nil {
		return invalidStackErrorInternal(err)
	}
	secretMetaByKey, err := ensurePlannedFileResources(
		ctx,
		secretPlans,
		stackLabels,
		secretFileResourceAdapter(dockerClient),
	)
	if err != nil {
		return err
	}

	existingServices, err := listStackServices(ctx, dockerClient, stackName)
	if err != nil {
		return err
	}

	desiredServices, err := reconcileStackServices(
		ctx,
		dockerClient,
		project,
		ns,
		stackLabels,
		networkNameByKey,
		configMetaByKey,
		secretMetaByKey,
		existingServices,
		opts,
		resolveMode,
	)
	if err != nil {
		return err
	}

	if opts.Prune {
		for _, name := range slices.Sorted(maps.Keys(existingServices)) {
			svc := existingServices[name]
			if _, ok := desiredServices[name]; ok {
				continue
			}
			if _, serviceRemoveErr := dockerClient.ServiceRemove(ctx, svc.ID, dockerclient.ServiceRemoveOptions{}); serviceRemoveErr != nil {
				return fmt.Errorf("failed to remove swarm service %s: %w", name, serviceRemoveErr)
			}
		}
	}

	if cleanupStackResourcesErr := cleanupStackResources(ctx, dockerClient, stackName, configMetaByKey, secretMetaByKey); cleanupStackResourcesErr != nil {
		return cleanupStackResourcesErr
	}

	return nil
}

func reconcileStackServices(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	project *composegotypes.Project,
	ns namespace,
	stackLabels map[string]string,
	networkNameByKey map[string]string,
	configMetaByKey map[string]resourceMeta,
	secretMetaByKey map[string]resourceMeta,
	existingServices map[string]swarm.Service,
	opts StackDeployOptions,
	resolveMode string,
) (map[string]struct{}, error) {
	desiredServices := map[string]struct{}{}

	for _, key := range slices.Sorted(maps.Keys(project.Services)) {
		service := project.Services[key]
		service.Name = cmp.Or(service.Name, key)
		spec, err := buildServiceSpecInternal(service, ns, stackLabels, networkNameByKey, configMetaByKey, secretMetaByKey, project.Volumes)
		if err != nil {
			return nil, invalidStackErrorInternal(fmt.Errorf("service %s: %w", service.Name, err))
		}
		desiredServices[spec.Name] = struct{}{}

		if existing, ok := existingServices[spec.Name]; ok {
			if updateSwarmServiceErr := updateSwarmService(ctx, dockerClient, existing, spec, opts.WithRegistryAuth, opts.RegistryAuthForImage, resolveMode); updateSwarmServiceErr != nil {
				return nil, updateSwarmServiceErr
			}
			continue
		}

		if createSwarmServiceErr := createSwarmService(ctx, dockerClient, spec, opts.WithRegistryAuth, opts.RegistryAuthForImage, resolveMode); createSwarmServiceErr != nil {
			return nil, createSwarmServiceErr
		}
	}

	return desiredServices, nil
}

func listStackServices(ctx context.Context, dockerClient *dockerclient.Client, stackName string) (map[string]swarm.Service, error) {
	filter := make(dockerclient.Filters).Add("label", fmt.Sprintf("%s=%s", types.SwarmStackNamespaceLabel, stackName))
	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{Filters: filter})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm services: %w", err)
	}
	services := servicesResult.Items

	byName := make(map[string]swarm.Service, len(services))
	for _, service := range services {
		byName[service.Spec.Name] = service
	}
	return byName, nil
}

func createSwarmService(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	spec swarm.ServiceSpec,
	withRegistryAuth bool,
	registryAuthForImage func(context.Context, string) (string, error),
	resolveMode string,
) error {
	encodedRegistryAuth, err := resolveRegistryAuthForSpec(ctx, spec, withRegistryAuth, registryAuthForImage)
	if err != nil {
		return err
	}
	queryRegistry := encodedRegistryAuth != "" || shouldQueryRegistryOnCreate(resolveMode)
	opts := dockerclient.ServiceCreateOptions{
		Spec:          spec,
		QueryRegistry: queryRegistry,
	}
	opts.EncodedRegistryAuth = cmp.Or(encodedRegistryAuth, opts.EncodedRegistryAuth)
	if _, serviceCreateErr := dockerClient.ServiceCreate(ctx, opts); serviceCreateErr != nil {
		return fmt.Errorf("failed to create swarm service %s: %w", spec.Name, serviceCreateErr)
	}
	return nil
}

func updateSwarmService(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	existing swarm.Service,
	spec swarm.ServiceSpec,
	withRegistryAuth bool,
	registryAuthForImage func(context.Context, string) (string, error),
	resolveMode string,
) error {
	encodedRegistryAuth, err := resolveRegistryAuthForSpec(ctx, spec, withRegistryAuth, registryAuthForImage)
	if err != nil {
		return err
	}
	queryRegistry := encodedRegistryAuth != "" || shouldQueryRegistryOnUpdate(resolveMode, existing, spec)
	if !queryRegistry {
		preserveExistingResolvedImage(&spec, existing)
	}
	// Do not force task rescheduling on no-op updates.
	spec.TaskTemplate.ForceUpdate = existing.Spec.TaskTemplate.ForceUpdate

	opts := dockerclient.ServiceUpdateOptions{
		Version:       existing.Version,
		Spec:          spec,
		QueryRegistry: queryRegistry,
	}
	if encodedRegistryAuth != "" {
		opts.EncodedRegistryAuth = encodedRegistryAuth
		opts.RegistryAuthFrom = swarm.RegistryAuthFromSpec
	} else {
		opts.RegistryAuthFrom = swarm.RegistryAuthFromPreviousSpec
	}

	if _, serviceUpdateErr := dockerClient.ServiceUpdate(ctx, existing.ID, opts); serviceUpdateErr != nil {
		if strings.Contains(serviceUpdateErr.Error(), "service does not have a previous spec") {
			opts.RegistryAuthFrom = ""
			if _, retryErr := dockerClient.ServiceUpdate(ctx, existing.ID, opts); retryErr != nil {
				return fmt.Errorf("failed to update swarm service %s: %w", spec.Name, retryErr)
			}
			return nil
		}
		return fmt.Errorf("failed to update swarm service %s: %w", spec.Name, serviceUpdateErr)
	}
	return nil
}

func normalizeResolveImageMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return resolveImageAlways, nil
	}
	switch mode {
	case resolveImageAlways, resolveImageChanged, resolveImageNever:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid resolve image mode %q: expected always, changed, or never", value)
	}
}

func shouldQueryRegistryOnCreate(resolveMode string) bool {
	return resolveMode == resolveImageAlways || resolveMode == resolveImageChanged
}

func shouldQueryRegistryOnUpdate(resolveMode string, existing swarm.Service, desired swarm.ServiceSpec) bool {
	switch resolveMode {
	case resolveImageAlways:
		return true
	case resolveImageChanged:
		desiredImage := stripImageDigest(resolveServiceImage(desired))
		existingImage := stripImageDigest(resolveServiceImage(existing.Spec))
		return desiredImage != "" && desiredImage != existingImage
	default:
		return false
	}
}

func resolveServiceImage(spec swarm.ServiceSpec) string {
	if spec.TaskTemplate.ContainerSpec == nil {
		return ""
	}
	return spec.TaskTemplate.ContainerSpec.Image
}

func preserveExistingResolvedImage(spec *swarm.ServiceSpec, existing swarm.Service) {
	if spec.TaskTemplate.ContainerSpec == nil || existing.Spec.TaskTemplate.ContainerSpec == nil {
		return
	}
	if stripImageDigest(resolveServiceImage(*spec)) == stripImageDigest(resolveServiceImage(existing.Spec)) {
		spec.TaskTemplate.ContainerSpec.Image = existing.Spec.TaskTemplate.ContainerSpec.Image
	}
}

func stripImageDigest(image string) string {
	image, _, _ = strings.Cut(strings.TrimSpace(image), "@")
	return image
}

func resolveRegistryAuthForSpec(
	ctx context.Context,
	spec swarm.ServiceSpec,
	withRegistryAuth bool,
	registryAuthForImage func(context.Context, string) (string, error),
) (string, error) {
	if !withRegistryAuth || registryAuthForImage == nil {
		return "", nil
	}

	image := resolveServiceImage(spec)
	if strings.TrimSpace(image) == "" {
		return "", nil
	}

	encodedRegistryAuth, err := registryAuthForImage(ctx, image)
	if err != nil {
		return "", fmt.Errorf("failed to resolve registry auth for image %s: %w", image, err)
	}

	return encodedRegistryAuth, nil
}
