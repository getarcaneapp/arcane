package swarm

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"emperror.dev/errors"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/backend/v2/pkg/projects/types"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
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
	resolveMode, err := normalizeResolveImageModeInternal(opts.ResolveImage)
	if err != nil {
		return invalidStackErrorInternal(err)
	}

	project, err := projects.LoadComposeProjectFromContent(ctx, projecttypes.ComposeContentOptions{
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

	stackLabels := map[string]string{swarmtypes.StackNamespaceLabel: stackName}

	networkNameByKey, err := ensureSwarmNetworksInternal(ctx, dockerClient, project, ns, stackLabels)
	if err != nil {
		return err
	}

	if err := ensureSwarmVolumesInternal(ctx, dockerClient, project, ns, stackLabels); err != nil {
		return err
	}

	configPlans, err := planConfigsInternal(project, ns)
	if err != nil {
		return invalidStackErrorInternal(err)
	}
	configMetaByKey, err := ensurePlannedFileResourcesInternal(
		ctx,
		configPlans,
		stackLabels,
		configFileResourceAdapterInternal(dockerClient),
	)
	if err != nil {
		return err
	}

	secretPlans, err := planSecretsInternal(project, ns)
	if err != nil {
		return invalidStackErrorInternal(err)
	}
	secretMetaByKey, err := ensurePlannedFileResourcesInternal(
		ctx,
		secretPlans,
		stackLabels,
		secretFileResourceAdapterInternal(dockerClient),
	)
	if err != nil {
		return err
	}

	existingServices, err := listStackServicesInternal(ctx, dockerClient, stackName)
	if err != nil {
		return err
	}

	desiredServices, err := reconcileStackServicesInternal(
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
			if _, err := dockerClient.ServiceRemove(ctx, svc.ID, dockerclient.ServiceRemoveOptions{}); err != nil {
				return errors.WrapIff(err, "failed to remove swarm service %s", name)
			}
		}
	}

	if err := cleanupStackResourcesInternal(ctx, dockerClient, stackName, configMetaByKey, secretMetaByKey); err != nil {
		return err
	}

	return nil
}

func reconcileStackServicesInternal(
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
		if service.Name == "" {
			service.Name = key
		}
		spec, err := buildServiceSpecInternal(service, ns, stackLabels, networkNameByKey, configMetaByKey, secretMetaByKey, project.Volumes)
		if err != nil {
			return nil, invalidStackErrorInternal(errors.WrapIff(err, "service %s", service.Name))
		}
		desiredServices[spec.Name] = struct{}{}

		if existing, ok := existingServices[spec.Name]; ok {
			if err := updateSwarmServiceInternal(ctx, dockerClient, existing, spec, opts.WithRegistryAuth, opts.RegistryAuthForImage, resolveMode); err != nil {
				return nil, err
			}
			continue
		}

		if err := createSwarmServiceInternal(ctx, dockerClient, spec, opts.WithRegistryAuth, opts.RegistryAuthForImage, resolveMode); err != nil {
			return nil, err
		}
	}

	return desiredServices, nil
}

func listStackServicesInternal(ctx context.Context, dockerClient *dockerclient.Client, stackName string) (map[string]swarm.Service, error) {
	filter := make(dockerclient.Filters).Add("label", fmt.Sprintf("%s=%s", swarmtypes.StackNamespaceLabel, stackName))
	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{Filters: filter})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list swarm services")
	}
	services := servicesResult.Items

	byName := make(map[string]swarm.Service, len(services))
	for _, service := range services {
		byName[service.Spec.Name] = service
	}
	return byName, nil
}

func createSwarmServiceInternal(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	spec swarm.ServiceSpec,
	withRegistryAuth bool,
	registryAuthForImage func(context.Context, string) (string, error),
	resolveMode string,
) error {
	encodedRegistryAuth, err := resolveRegistryAuthForSpecInternal(ctx, spec, withRegistryAuth, registryAuthForImage)
	if err != nil {
		return err
	}
	queryRegistry := encodedRegistryAuth != "" || shouldQueryRegistryOnCreateInternal(resolveMode)
	opts := dockerclient.ServiceCreateOptions{
		Spec:          spec,
		QueryRegistry: queryRegistry,
	}
	if encodedRegistryAuth != "" {
		opts.EncodedRegistryAuth = encodedRegistryAuth
	}
	if _, err := dockerClient.ServiceCreate(ctx, opts); err != nil {
		return errors.WrapIff(err, "failed to create swarm service %s", spec.Name)
	}
	return nil
}

func updateSwarmServiceInternal(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	existing swarm.Service,
	spec swarm.ServiceSpec,
	withRegistryAuth bool,
	registryAuthForImage func(context.Context, string) (string, error),
	resolveMode string,
) error {
	encodedRegistryAuth, err := resolveRegistryAuthForSpecInternal(ctx, spec, withRegistryAuth, registryAuthForImage)
	if err != nil {
		return err
	}
	queryRegistry := encodedRegistryAuth != "" || shouldQueryRegistryOnUpdateInternal(resolveMode, existing, spec)
	if !queryRegistry {
		preserveExistingResolvedImageInternal(&spec, existing)
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

	if _, err := dockerClient.ServiceUpdate(ctx, existing.ID, opts); err != nil {
		if strings.Contains(err.Error(), "service does not have a previous spec") {
			opts.RegistryAuthFrom = ""
			if _, retryErr := dockerClient.ServiceUpdate(ctx, existing.ID, opts); retryErr != nil {
				return errors.WrapIff(retryErr, "failed to update swarm service %s", spec.Name)
			}
			return nil
		}
		return errors.WrapIff(err, "failed to update swarm service %s", spec.Name)
	}
	return nil
}

func normalizeResolveImageModeInternal(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return resolveImageAlways, nil
	}
	switch mode {
	case resolveImageAlways, resolveImageChanged, resolveImageNever:
		return mode, nil
	default:
		return "", errors.Errorf("invalid resolve image mode %q: expected always, changed, or never", value)
	}
}

func shouldQueryRegistryOnCreateInternal(resolveMode string) bool {
	return resolveMode == resolveImageAlways || resolveMode == resolveImageChanged
}

func shouldQueryRegistryOnUpdateInternal(resolveMode string, existing swarm.Service, desired swarm.ServiceSpec) bool {
	switch resolveMode {
	case resolveImageAlways:
		return true
	case resolveImageChanged:
		desiredImage := stripImageDigestInternal(resolveServiceImageInternal(desired))
		existingImage := stripImageDigestInternal(resolveServiceImageInternal(existing.Spec))
		return desiredImage != "" && desiredImage != existingImage
	default:
		return false
	}
}

func resolveServiceImageInternal(spec swarm.ServiceSpec) string {
	if spec.TaskTemplate.ContainerSpec == nil {
		return ""
	}
	return spec.TaskTemplate.ContainerSpec.Image
}

func preserveExistingResolvedImageInternal(spec *swarm.ServiceSpec, existing swarm.Service) {
	if spec.TaskTemplate.ContainerSpec == nil || existing.Spec.TaskTemplate.ContainerSpec == nil {
		return
	}
	if stripImageDigestInternal(resolveServiceImageInternal(*spec)) == stripImageDigestInternal(resolveServiceImageInternal(existing.Spec)) {
		spec.TaskTemplate.ContainerSpec.Image = existing.Spec.TaskTemplate.ContainerSpec.Image
	}
}

func stripImageDigestInternal(image string) string {
	image, _, _ = strings.Cut(strings.TrimSpace(image), "@")
	return image
}

func resolveRegistryAuthForSpecInternal(
	ctx context.Context,
	spec swarm.ServiceSpec,
	withRegistryAuth bool,
	registryAuthForImage func(context.Context, string) (string, error),
) (string, error) {
	if !withRegistryAuth || registryAuthForImage == nil {
		return "", nil
	}

	image := resolveServiceImageInternal(spec)
	if strings.TrimSpace(image) == "" {
		return "", nil
	}

	encodedRegistryAuth, err := registryAuthForImage(ctx, image)
	if err != nil {
		return "", errors.WrapIff(err, "failed to resolve registry auth for image %s", image)
	}

	return encodedRegistryAuth, nil
}
