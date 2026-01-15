package swarm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	composegoloader "github.com/compose-spec/compose-go/v2/loader"
	composegotypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	dockerclient "github.com/docker/docker/client"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
)

type resourceMeta struct {
	ID   string
	Name string
}

// StackDeployOptions controls how a swarm stack is deployed.
type StackDeployOptions struct {
	Name             string
	ComposeContent   string
	EnvContent       string
	WithRegistryAuth bool
	Prune            bool
	ResolveImage     string
}

// DeployStack deploys a stack directly using the Docker Engine API
func DeployStack(ctx context.Context, dockerClient *dockerclient.Client, opts StackDeployOptions) error {
	stackName := strings.TrimSpace(opts.Name)
	if stackName == "" {
		return errors.New("stack name is required")
	}

	project, err := loadComposeProject(ctx, stackName, opts.ComposeContent, opts.EnvContent)
	if err != nil {
		return err
	}
	if project.Name == "" {
		project.Name = stackName
	}

	stackLabels := map[string]string{swarmtypes.StackNamespaceLabel: stackName}

	networkNameByKey, err := ensureSwarmNetworks(ctx, dockerClient, project, stackName, stackLabels)
	if err != nil {
		return err
	}

	configMetaByKey, err := ensureSwarmConfigs(ctx, dockerClient, project, stackName, stackLabels)
	if err != nil {
		return err
	}

	secretMetaByKey, err := ensureSwarmSecrets(ctx, dockerClient, project, stackName, stackLabels)
	if err != nil {
		return err
	}

	existingServices, err := listStackServices(ctx, dockerClient, stackName)
	if err != nil {
		return err
	}

	desiredServices := map[string]struct{}{}
	for key, service := range project.Services {
		if service.Name == "" {
			service.Name = key
		}
		spec, err := buildServiceSpec(service, stackName, stackLabels, networkNameByKey, configMetaByKey, secretMetaByKey)
		if err != nil {
			return err
		}
		desiredServices[spec.Annotations.Name] = struct{}{}

		if existing, ok := existingServices[spec.Annotations.Name]; ok {
			if err := updateSwarmService(ctx, dockerClient, existing, spec); err != nil {
				return err
			}
			continue
		}

		if err := createSwarmService(ctx, dockerClient, spec, opts.WithRegistryAuth); err != nil {
			return err
		}
	}

	if opts.Prune {
		for name, svc := range existingServices {
			if _, ok := desiredServices[name]; ok {
				continue
			}
			if err := dockerClient.ServiceRemove(ctx, svc.ID); err != nil {
				return fmt.Errorf("failed to remove swarm service %s: %w", name, err)
			}
		}
	}

	return nil
}

func loadComposeProject(ctx context.Context, projectName, composeContent, envContent string) (*composegotypes.Project, error) {
	composeContent = strings.TrimSpace(composeContent)
	if composeContent == "" {
		return nil, errors.New("compose content is required")
	}

	envMap, err := parseEnvContent(envContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse env content: %w", err)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "/tmp"
	}

	configDetails := composegotypes.ConfigDetails{
		Version:    api.ComposeVersion,
		WorkingDir: workingDir,
		ConfigFiles: []composegotypes.ConfigFile{
			{Content: []byte(composeContent)},
		},
		Environment: composegotypes.Mapping(envMap),
	}

	project, err := composegoloader.LoadWithContext(ctx, configDetails, func(opts *composegoloader.Options) {
		if strings.TrimSpace(projectName) != "" {
			opts.SetProjectName(strings.TrimSpace(projectName), true)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load compose project: %w", err)
	}

	project = project.WithoutUnnecessaryResources()
	return project, nil
}

func listStackServices(ctx context.Context, dockerClient *dockerclient.Client, stackName string) (map[string]swarm.Service, error) {
	filters := filters.NewArgs(filters.Arg("label", fmt.Sprintf("%s=%s", swarmtypes.StackNamespaceLabel, stackName)))
	services, err := dockerClient.ServiceList(ctx, swarm.ServiceListOptions{Filters: filters})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm services: %w", err)
	}

	byName := make(map[string]swarm.Service, len(services))
	for _, service := range services {
		byName[service.Spec.Annotations.Name] = service
	}
	return byName, nil
}

func ensureSwarmNetworks(ctx context.Context, dockerClient *dockerclient.Client, project *composegotypes.Project, stackName string, stackLabels map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(project.Networks))
	for key, cfg := range project.Networks {
		networkName := strings.TrimSpace(cfg.Name)
		if networkName == "" {
			networkName = key
		}

		if bool(cfg.External) {
			result[key] = networkName
			continue
		}

		stackedName := stackScopedName(stackName, networkName)
		result[key] = stackedName

		_, err := dockerClient.NetworkInspect(ctx, stackedName, network.InspectOptions{Scope: "swarm"})
		if err == nil {
			continue
		}
		if !cerrdefs.IsNotFound(err) {
			return nil, fmt.Errorf("failed to inspect network %s: %w", stackedName, err)
		}

		driver := strings.TrimSpace(cfg.Driver)
		if driver == "" {
			driver = "overlay"
		}

		labels := mergeLabels(cfg.Labels, stackLabels)
		createOpts := network.CreateOptions{
			Driver:     driver,
			Scope:      "swarm",
			EnableIPv4: cfg.EnableIPv4,
			EnableIPv6: cfg.EnableIPv6,
			Internal:   cfg.Internal,
			Attachable: cfg.Attachable,
			Options:    cfg.DriverOpts,
			Labels:     labels,
			IPAM:       convertIPAM(cfg.Ipam),
		}

		if _, err := dockerClient.NetworkCreate(ctx, stackedName, createOpts); err != nil {
			return nil, fmt.Errorf("failed to create network %s: %w", stackedName, err)
		}
	}

	return result, nil
}

func ensureSwarmConfigs(ctx context.Context, dockerClient *dockerclient.Client, project *composegotypes.Project, stackName string, stackLabels map[string]string) (map[string]resourceMeta, error) {
	result := make(map[string]resourceMeta, len(project.Configs))
	for key, cfg := range project.Configs {
		name := resolveResourceName(stackName, key, cfg.Name, cfg.External)
		if cfg.External {
			meta, err := inspectConfig(ctx, dockerClient, name)
			if err != nil {
				return nil, err
			}
			result[key] = meta
			continue
		}

		meta, err := ensureConfig(ctx, dockerClient, name, cfg, stackLabels, project.WorkingDir)
		if err != nil {
			return nil, err
		}
		result[key] = meta
	}
	return result, nil
}

func ensureSwarmSecrets(ctx context.Context, dockerClient *dockerclient.Client, project *composegotypes.Project, stackName string, stackLabels map[string]string) (map[string]resourceMeta, error) {
	result := make(map[string]resourceMeta, len(project.Secrets))
	for key, cfg := range project.Secrets {
		name := resolveResourceName(stackName, key, cfg.Name, cfg.External)
		if cfg.External {
			meta, err := inspectSecret(ctx, dockerClient, name)
			if err != nil {
				return nil, err
			}
			result[key] = meta
			continue
		}

		meta, err := ensureSecret(ctx, dockerClient, name, cfg, stackLabels, project.WorkingDir)
		if err != nil {
			return nil, err
		}
		result[key] = meta
	}
	return result, nil
}

func ensureConfig(ctx context.Context, dockerClient *dockerclient.Client, name string, cfg composegotypes.ConfigObjConfig, stackLabels map[string]string, workingDir string) (resourceMeta, error) {
	if meta, err := inspectConfig(ctx, dockerClient, name); err == nil {
		return meta, nil
	} else if !cerrdefs.IsNotFound(err) {
		return resourceMeta{}, fmt.Errorf("failed to inspect config %s: %w", name, err)
	}

	data, err := resolveFileObjectContent(composegotypes.FileObjectConfig(cfg), workingDir)
	if err != nil {
		return resourceMeta{}, fmt.Errorf("failed to load config %s: %w", name, err)
	}

	labels := mergeLabels(cfg.Labels, stackLabels)
	spec := swarm.ConfigSpec{
		Annotations: swarm.Annotations{
			Name:   name,
			Labels: labels,
		},
		Data: data,
	}

	resp, err := dockerClient.ConfigCreate(ctx, spec)
	if err != nil {
		return resourceMeta{}, fmt.Errorf("failed to create config %s: %w", name, err)
	}
	return resourceMeta{ID: resp.ID, Name: name}, nil
}

func ensureSecret(ctx context.Context, dockerClient *dockerclient.Client, name string, cfg composegotypes.SecretConfig, stackLabels map[string]string, workingDir string) (resourceMeta, error) {
	if meta, err := inspectSecret(ctx, dockerClient, name); err == nil {
		return meta, nil
	} else if !cerrdefs.IsNotFound(err) {
		return resourceMeta{}, fmt.Errorf("failed to inspect secret %s: %w", name, err)
	}

	data, err := resolveFileObjectContent(composegotypes.FileObjectConfig(cfg), workingDir)
	if err != nil {
		return resourceMeta{}, fmt.Errorf("failed to load secret %s: %w", name, err)
	}

	labels := mergeLabels(cfg.Labels, stackLabels)
	spec := swarm.SecretSpec{
		Annotations: swarm.Annotations{
			Name:   name,
			Labels: labels,
		},
		Data: data,
	}

	resp, err := dockerClient.SecretCreate(ctx, spec)
	if err != nil {
		return resourceMeta{}, fmt.Errorf("failed to create secret %s: %w", name, err)
	}
	return resourceMeta{ID: resp.ID, Name: name}, nil
}

func inspectConfig(ctx context.Context, dockerClient *dockerclient.Client, name string) (resourceMeta, error) {
	config, _, err := dockerClient.ConfigInspectWithRaw(ctx, name)
	if err != nil {
		return resourceMeta{}, err
	}
	return resourceMeta{ID: config.ID, Name: config.Spec.Name}, nil
}

func inspectSecret(ctx context.Context, dockerClient *dockerclient.Client, name string) (resourceMeta, error) {
	secret, _, err := dockerClient.SecretInspectWithRaw(ctx, name)
	if err != nil {
		return resourceMeta{}, err
	}
	return resourceMeta{ID: secret.ID, Name: secret.Spec.Name}, nil
}

func resolveResourceName(stackName, key, resourceName string, external composegotypes.External) string {
	name := strings.TrimSpace(resourceName)
	if name == "" {
		name = key
	}
	if bool(external) {
		return name
	}
	return stackScopedName(stackName, name)
}

func buildServiceSpec(
	service composegotypes.ServiceConfig,
	stackName string,
	stackLabels map[string]string,
	networkNameByKey map[string]string,
	configMetaByKey map[string]resourceMeta,
	secretMetaByKey map[string]resourceMeta,
) (swarm.ServiceSpec, error) {
	serviceName := stackScopedName(stackName, service.Name)
	serviceLabels := mergeLabels(nil, stackLabels)
	if service.Deploy != nil {
		serviceLabels = mergeLabels(service.Deploy.Labels, serviceLabels)
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   serviceName,
			Labels: serviceLabels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:           service.Image,
				Command:         toStringSlice(service.Entrypoint),
				Args:            toStringSlice(service.Command),
				Env:             convertEnv(service.Environment),
				Dir:             service.WorkingDir,
				User:            service.User,
				Groups:          service.GroupAdd,
				Hostname:        service.Hostname,
				Init:            service.Init,
				StopSignal:      service.StopSignal,
				StopGracePeriod: convertDurationPtr(service.StopGracePeriod),
				ReadOnly:        service.ReadOnly,
				TTY:             service.Tty,
				OpenStdin:       service.StdinOpen,
				Labels:          mergeLabels(service.Labels, stackLabels),
				Mounts:          convertServiceMounts(service.Volumes),
				Secrets:         convertServiceSecretRefs(service.Secrets, secretMetaByKey),
				Configs:         convertServiceConfigRefs(service.Configs, configMetaByKey),
			},
			Networks: buildServiceNetworks(service, networkNameByKey),
		},
	}

	if len(service.ExtraHosts) > 0 {
		spec.TaskTemplate.ContainerSpec.Hosts = service.ExtraHosts.AsList(":")
	}

	if len(service.DNS) > 0 || len(service.DNSSearch) > 0 || len(service.DNSOpts) > 0 {
		spec.TaskTemplate.ContainerSpec.DNSConfig = &swarm.DNSConfig{
			Nameservers: []string(service.DNS),
			Search:      []string(service.DNSSearch),
			Options:     service.DNSOpts,
		}
	}

	applyDeployConfig(&spec, service.Deploy, service.Scale)
	applyServicePorts(&spec, service.Ports)

	return spec, nil
}

func createSwarmService(ctx context.Context, dockerClient *dockerclient.Client, spec swarm.ServiceSpec, withRegistryAuth bool) error {
	options := swarm.ServiceCreateOptions{}
	if withRegistryAuth {
		// TODO: Populate EncodedRegistryAuth for private registries.
		options.QueryRegistry = true
	}
	if _, err := dockerClient.ServiceCreate(ctx, spec, options); err != nil {
		return fmt.Errorf("failed to create swarm service %s: %w", spec.Annotations.Name, err)
	}
	return nil
}

func updateSwarmService(ctx context.Context, dockerClient *dockerclient.Client, existing swarm.Service, spec swarm.ServiceSpec) error {
	options := swarm.ServiceUpdateOptions{}
	if _, err := dockerClient.ServiceUpdate(ctx, existing.ID, swarm.Version{Index: existing.Version.Index}, spec, options); err != nil {
		return fmt.Errorf("failed to update swarm service %s: %w", spec.Annotations.Name, err)
	}
	return nil
}

func applyDeployConfig(spec *swarm.ServiceSpec, deploy *composegotypes.DeployConfig, scale *int) {
	if deploy == nil {
		applyServiceMode(spec, "", scale)
		return
	}

	applyServiceMode(spec, deploy.Mode, scale)
	if deploy.Replicas != nil && spec.Mode.Replicated != nil {
		spec.Mode.Replicated.Replicas = toUint64Pointer(*deploy.Replicas)
	}

	if deploy.EndpointMode != "" {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		spec.EndpointSpec.Mode = swarm.ResolutionMode(deploy.EndpointMode)
	}

	if deploy.UpdateConfig != nil {
		spec.UpdateConfig = &swarm.UpdateConfig{
			Parallelism:     valueOrZero(deploy.UpdateConfig.Parallelism),
			Delay:           time.Duration(deploy.UpdateConfig.Delay),
			FailureAction:   deploy.UpdateConfig.FailureAction,
			Monitor:         time.Duration(deploy.UpdateConfig.Monitor),
			MaxFailureRatio: deploy.UpdateConfig.MaxFailureRatio,
			Order:           deploy.UpdateConfig.Order,
		}
	}
	if deploy.RollbackConfig != nil {
		spec.RollbackConfig = &swarm.UpdateConfig{
			Parallelism:     valueOrZero(deploy.RollbackConfig.Parallelism),
			Delay:           time.Duration(deploy.RollbackConfig.Delay),
			FailureAction:   deploy.RollbackConfig.FailureAction,
			Monitor:         time.Duration(deploy.RollbackConfig.Monitor),
			MaxFailureRatio: deploy.RollbackConfig.MaxFailureRatio,
			Order:           deploy.RollbackConfig.Order,
		}
	}

	if deploy.RestartPolicy != nil {
		spec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{
			Condition: swarm.RestartPolicyCondition(deploy.RestartPolicy.Condition),
			Delay:     convertDurationPtr(deploy.RestartPolicy.Delay),
			Window:    convertDurationPtr(deploy.RestartPolicy.Window),
		}
		if deploy.RestartPolicy.MaxAttempts != nil {
			spec.TaskTemplate.RestartPolicy.MaxAttempts = deploy.RestartPolicy.MaxAttempts
		}
	}

	if deploy.Resources.Limits != nil || deploy.Resources.Reservations != nil {
		spec.TaskTemplate.Resources = &swarm.ResourceRequirements{}
	}
	if deploy.Resources.Limits != nil {
		spec.TaskTemplate.Resources.Limits = &swarm.Limit{
			NanoCPUs:    int64(deploy.Resources.Limits.NanoCPUs),
			MemoryBytes: int64(deploy.Resources.Limits.MemoryBytes),
			Pids:        deploy.Resources.Limits.Pids,
		}
	}
	if deploy.Resources.Reservations != nil {
		spec.TaskTemplate.Resources.Reservations = &swarm.Resources{
			NanoCPUs:    int64(deploy.Resources.Reservations.NanoCPUs),
			MemoryBytes: int64(deploy.Resources.Reservations.MemoryBytes),
		}
	}

	if len(deploy.Placement.Constraints) > 0 || len(deploy.Placement.Preferences) > 0 || deploy.Placement.MaxReplicas > 0 {
		spec.TaskTemplate.Placement = &swarm.Placement{
			Constraints: deploy.Placement.Constraints,
			MaxReplicas: deploy.Placement.MaxReplicas,
		}
	}
}

func applyServiceMode(spec *swarm.ServiceSpec, mode string, scale *int) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "global":
		spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	default:
		replicas := uint64(1)
		if scale != nil {
			replicas = uint64(*scale)
		}
		spec.Mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}}
	}
}

func applyServicePorts(spec *swarm.ServiceSpec, ports []composegotypes.ServicePortConfig) {
	if len(ports) == 0 {
		return
	}

	endpoint := spec.EndpointSpec
	if endpoint == nil {
		endpoint = &swarm.EndpointSpec{}
	}

	converted := make([]swarm.PortConfig, 0, len(ports))
	for _, port := range ports {
		entry := swarm.PortConfig{
			Name:       port.Name,
			Protocol:   swarm.PortConfigProtocol(strings.ToLower(port.Protocol)),
			TargetPort: port.Target,
		}
		if port.Mode != "" {
			entry.PublishMode = swarm.PortConfigPublishMode(strings.ToLower(port.Mode))
		}
		if port.Published != "" {
			if published, err := strconv.ParseUint(port.Published, 10, 32); err == nil {
				entry.PublishedPort = uint32(published)
			}
		}
		converted = append(converted, entry)
	}

	endpoint.Ports = converted
	spec.EndpointSpec = endpoint
}

func buildServiceNetworks(service composegotypes.ServiceConfig, networkNameByKey map[string]string) []swarm.NetworkAttachmentConfig {
	var attachments []swarm.NetworkAttachmentConfig
	if len(service.Networks) == 0 {
		if defaultNetwork, ok := networkNameByKey["default"]; ok {
			attachments = append(attachments, swarm.NetworkAttachmentConfig{Target: defaultNetwork})
		}
		return attachments
	}

	for name, cfg := range service.Networks {
		networkName := networkNameByKey[name]
		if networkName == "" {
			networkName = name
		}
		if cfg == nil {
			attachments = append(attachments, swarm.NetworkAttachmentConfig{Target: networkName})
			continue
		}
		attachments = append(attachments, swarm.NetworkAttachmentConfig{
			Target:     networkName,
			Aliases:    cfg.Aliases,
			DriverOpts: cfg.DriverOpts,
		})
	}
	return attachments
}

func convertServiceMounts(volumes []composegotypes.ServiceVolumeConfig) []mount.Mount {
	if len(volumes) == 0 {
		return nil
	}
	result := make([]mount.Mount, 0, len(volumes))
	for _, vol := range volumes {
		mountType := mapVolumeType(vol.Type)
		entry := mount.Mount{
			Type:        mountType,
			Source:      vol.Source,
			Target:      vol.Target,
			ReadOnly:    vol.ReadOnly,
			Consistency: mount.Consistency(vol.Consistency),
		}
		if vol.Bind != nil {
			entry.BindOptions = &mount.BindOptions{
				Propagation:      mount.Propagation(vol.Bind.Propagation),
				CreateMountpoint: bool(vol.Bind.CreateHostPath),
			}
		}
		if vol.Volume != nil {
			entry.VolumeOptions = &mount.VolumeOptions{
				NoCopy:  vol.Volume.NoCopy,
				Labels:  vol.Volume.Labels,
				Subpath: vol.Volume.Subpath,
			}
		}
		if vol.Tmpfs != nil {
			entry.TmpfsOptions = &mount.TmpfsOptions{
				SizeBytes: int64(vol.Tmpfs.Size),
				Mode:      os.FileMode(vol.Tmpfs.Mode),
			}
		}
		if vol.Image != nil {
			entry.ImageOptions = &mount.ImageOptions{Subpath: vol.Image.SubPath}
		}
		result = append(result, entry)
	}
	return result
}

func convertServiceSecretRefs(secrets []composegotypes.ServiceSecretConfig, secretMetaByKey map[string]resourceMeta) []*swarm.SecretReference {
	if len(secrets) == 0 {
		return nil
	}
	result := make([]*swarm.SecretReference, 0, len(secrets))
	for _, secret := range secrets {
		meta, ok := secretMetaByKey[secret.Source]
		if !ok {
			continue
		}
		ref := &swarm.SecretReference{
			SecretID:   meta.ID,
			SecretName: meta.Name,
		}
		target := secret.Target
		if target == "" {
			target = meta.Name
		}
		ref.File = &swarm.SecretReferenceFileTarget{
			Name: target,
			UID:  secret.UID,
			GID:  secret.GID,
			Mode: fileModeOrDefault(secret.Mode),
		}
		result = append(result, ref)
	}
	return result
}

func convertServiceConfigRefs(configs []composegotypes.ServiceConfigObjConfig, configMetaByKey map[string]resourceMeta) []*swarm.ConfigReference {
	if len(configs) == 0 {
		return nil
	}
	result := make([]*swarm.ConfigReference, 0, len(configs))
	for _, cfg := range configs {
		meta, ok := configMetaByKey[cfg.Source]
		if !ok {
			continue
		}
		ref := &swarm.ConfigReference{
			ConfigID:   meta.ID,
			ConfigName: meta.Name,
		}
		target := cfg.Target
		if target == "" {
			target = meta.Name
		}
		ref.File = &swarm.ConfigReferenceFileTarget{
			Name: target,
			UID:  cfg.UID,
			GID:  cfg.GID,
			Mode: fileModeOrDefault(cfg.Mode),
		}
		result = append(result, ref)
	}
	return result
}

func resolveFileObjectContent(fileConfig composegotypes.FileObjectConfig, workingDir string) ([]byte, error) {
	if fileConfig.Content != "" {
		return []byte(fileConfig.Content), nil
	}
	if fileConfig.Environment != "" {
		value, ok := os.LookupEnv(fileConfig.Environment)
		if !ok {
			return nil, fmt.Errorf("environment variable %s not set", fileConfig.Environment)
		}
		return []byte(value), nil
	}
	if fileConfig.File != "" {
		path := fileConfig.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		return os.ReadFile(path)
	}
	return nil, errors.New("config or secret content is required")
}

func convertIPAM(cfg composegotypes.IPAMConfig) *network.IPAM {
	if cfg.Driver == "" && len(cfg.Config) == 0 {
		return nil
	}
	result := &network.IPAM{
		Driver: cfg.Driver,
	}
	if len(cfg.Config) > 0 {
		pools := make([]network.IPAMConfig, 0, len(cfg.Config))
		for _, pool := range cfg.Config {
			if pool == nil {
				continue
			}
			pools = append(pools, network.IPAMConfig{
				Subnet:     pool.Subnet,
				Gateway:    pool.Gateway,
				IPRange:    pool.IPRange,
				AuxAddress: pool.AuxiliaryAddresses,
			})
		}
		result.Config = pools
	}
	return result
}

func convertEnv(env composegotypes.MappingWithEquals) []string {
	if env == nil {
		return nil
	}
	result := make([]string, 0, len(env))
	for key, value := range env {
		if value == nil {
			result = append(result, key)
			continue
		}
		result = append(result, fmt.Sprintf("%s=%s", key, *value))
	}
	return result
}

func toStringSlice(command composegotypes.ShellCommand) []string {
	if len(command) == 0 {
		return nil
	}
	return []string(command)
}

func convertDurationPtr(duration *composegotypes.Duration) *time.Duration {
	if duration == nil {
		return nil
	}
	value := time.Duration(*duration)
	return &value
}

func toUint64Pointer(value int) *uint64 {
	if value < 0 {
		return nil
	}
	converted := uint64(value)
	return &converted
}

func fileModeOrDefault(mode *composegotypes.FileMode) os.FileMode {
	if mode == nil {
		return 0444
	}
	converted := fileModeToUint32(mode)
	if converted == nil {
		return 0444
	}
	return os.FileMode(*converted)
}

func fileModeToUint32(mode *composegotypes.FileMode) *uint32 {
	return convertFileMode(mode)
}

func valueOrZero(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func mergeLabels(primary map[string]string, secondary map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range primary {
		out[key] = value
	}
	for key, value := range secondary {
		out[key] = value
	}
	return out
}

func stackScopedName(stackName, resourceName string) string {
	resourceName = strings.TrimSpace(resourceName)
	if resourceName == "" {
		return stackName
	}
	return fmt.Sprintf("%s_%s", stackName, resourceName)
}

func mapVolumeType(value string) mount.Type {
	switch strings.ToLower(value) {
	case "bind":
		return mount.TypeBind
	case "tmpfs":
		return mount.TypeTmpfs
	case "npipe":
		return mount.TypeNamedPipe
	case "cluster":
		return mount.TypeCluster
	case "image":
		return mount.TypeImage
	case "volume", "":
		return mount.TypeVolume
	default:
		return mount.TypeVolume
	}
}

func convertFileMode(mode *composegotypes.FileMode) *uint32 {
	if mode == nil {
		return nil
	}
	if result, ok := toUint32FromInt64(int64(*mode)); ok {
		return &result
	}
	return nil
}

func toUint32FromInt64(value int64) (uint32, bool) {
	if value < 0 || value > int64(^uint32(0)) {
		return 0, false
	}
	return uint32(value), true
}

func parseEnvContent(envContent string) (map[string]string, error) {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}

	if strings.TrimSpace(envContent) == "" {
		return env, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(envContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		env[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return env, nil
}
