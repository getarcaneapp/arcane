package swarm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	dockeropts "github.com/docker/cli/opts"
	mobyContainer "github.com/moby/moby/api/types/container"
	mobyNetwork "github.com/moby/moby/api/types/network"
	mobySwarm "github.com/moby/moby/api/types/swarm"
	"github.com/samber/mo"

	kit "kit.getarcane.app"
)

func buildServiceSpec(
	service composeTypes.ServiceConfig,
	ns namespace,
	stackLabels map[string]string,
	networkNameByKey map[string]string,
	configMetaByKey map[string]resourceMeta,
	secretMetaByKey map[string]resourceMeta,
	projectVolumes composeTypes.Volumes,
) (mobySwarm.ServiceSpec, error) {
	serviceName := ns.Scope(service.Name)
	serviceLabels := mergeLabels(nil, stackLabels)
	if service.Deploy != nil {
		serviceLabels = mergeLabels(service.Deploy.Labels, serviceLabels)
	}

	mounts, err := convertServiceMounts(service.Volumes, ns, projectVolumes, stackLabels)
	if err != nil {
		return mobySwarm.ServiceSpec{}, err
	}
	secretRefs, err := convertServiceSecretRefs(service.Secrets, secretMetaByKey)
	if err != nil {
		return mobySwarm.ServiceSpec{}, err
	}
	configRefs, err := convertServiceConfigRefs(service.Configs, configMetaByKey)
	if err != nil {
		return mobySwarm.ServiceSpec{}, err
	}
	privileges, runtimeConfigRef, err := convertPrivileges(service.CredentialSpec, service.SecurityOpt, configMetaByKey)
	if err != nil {
		return mobySwarm.ServiceSpec{}, err
	}
	if runtimeConfigRef != nil {
		configRefs = append(configRefs, runtimeConfigRef)
		slices.SortFunc(configRefs, func(a, b *mobySwarm.ConfigReference) int {
			return strings.Compare(a.ConfigName, b.ConfigName)
		})
	}
	healthcheck, err := convertHealthcheck(service.HealthCheck)
	if err != nil {
		return mobySwarm.ServiceSpec{}, err
	}
	networks, err := buildServiceNetworks(service, networkNameByKey)
	if err != nil {
		return mobySwarm.ServiceSpec{}, err
	}
	capabilityAdd, capabilityDrop := dockeropts.EffectiveCapAddCapDrop(service.CapAdd, service.CapDrop)

	var logDriver *mobySwarm.Driver
	if service.Logging != nil {
		logDriver = &mobySwarm.Driver{
			Name:    service.Logging.Driver,
			Options: service.Logging.Options,
		}
	}
	var dnsConfig *mobySwarm.DNSConfig
	if len(service.DNS) > 0 || len(service.DNSSearch) > 0 || len(service.DNSOpts) > 0 {
		dnsConfig = &mobySwarm.DNSConfig{
			Nameservers: parseIPList(service.DNS),
			Search:      service.DNSSearch,
			Options:     service.DNSOpts,
		}
	}

	spec := mobySwarm.ServiceSpec{
		Name:   serviceName,
		Labels: serviceLabels,
		TaskTemplate: mobySwarm.TaskSpec{
			ContainerSpec: &mobySwarm.ContainerSpec{
				Image:           service.Image,
				Command:         kit.Ternary(len(service.Entrypoint) == 0, nil, service.Entrypoint),
				Args:            kit.Ternary(len(service.Command) == 0, nil, service.Command),
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
				Mounts:          mounts,
				Secrets:         secretRefs,
				Configs:         configRefs,
				Hosts:           convertExtraHosts(service.ExtraHosts),
				DNSConfig:       dnsConfig,
				Healthcheck:     healthcheck,
				Privileges:      privileges,
				Isolation:       mobyContainer.Isolation(service.Isolation),
				Sysctls:         map[string]string(service.Sysctls),
				CapabilityAdd:   capabilityAdd,
				CapabilityDrop:  capabilityDrop,
				Ulimits:         convertUlimits(service.Ulimits),
				OomScoreAdj:     service.OomScoreAdj,
			},
			LogDriver: logDriver,
			Networks:  networks,
			Runtime:   mobySwarm.RuntimeType(service.Runtime),
		},
	}

	if applyDeployConfigErr := applyDeployConfig(&spec, service.Deploy, service.Scale, service.Restart); applyDeployConfigErr != nil {
		return mobySwarm.ServiceSpec{}, applyDeployConfigErr
	}
	if applyServicePortsErr := applyServicePorts(&spec, service.Ports); applyServicePortsErr != nil {
		return mobySwarm.ServiceSpec{}, applyServicePortsErr
	}

	return spec, nil
}

func convertExtraHosts(extraHosts composeTypes.HostsList) []string {
	hosts := make([]string, 0, len(extraHosts))
	for hostName, addresses := range extraHosts {
		for _, address := range addresses {
			hosts = append(hosts, address+" "+hostName)
		}
	}
	slices.Sort(hosts)
	return hosts
}

func convertHealthcheck(healthcheck *composeTypes.HealthCheckConfig) (*mobyContainer.HealthConfig, error) {
	if healthcheck == nil {
		return nil, nil
	}
	if healthcheck.Disable {
		if len(healthcheck.Test) > 0 ||
			healthcheck.Timeout != nil ||
			healthcheck.Interval != nil ||
			healthcheck.Retries != nil ||
			healthcheck.StartPeriod != nil ||
			healthcheck.StartInterval != nil {
			return nil, errors.New("healthcheck disable cannot be combined with other healthcheck fields")
		}
		return &mobyContainer.HealthConfig{Test: []string{"NONE"}}, nil
	}

	result := &mobyContainer.HealthConfig{Test: healthcheck.Test}
	if healthcheck.Timeout != nil {
		result.Timeout = time.Duration(*healthcheck.Timeout)
	}
	if healthcheck.Interval != nil {
		result.Interval = time.Duration(*healthcheck.Interval)
	}
	if healthcheck.Retries != nil {
		if *healthcheck.Retries > uint64(math.MaxInt) {
			return nil, fmt.Errorf("healthcheck retries %d exceeds the platform int range", *healthcheck.Retries)
		}
		result.Retries = int(*healthcheck.Retries)
	}
	if healthcheck.StartPeriod != nil {
		result.StartPeriod = time.Duration(*healthcheck.StartPeriod)
	}
	if healthcheck.StartInterval != nil {
		result.StartInterval = time.Duration(*healthcheck.StartInterval)
	}
	return result, nil
}

func convertPrivileges(
	credentialSpec *composeTypes.CredentialSpecConfig,
	securityOptions []string,
	configMetaByKey map[string]resourceMeta,
) (*mobySwarm.Privileges, *mobySwarm.ConfigReference, error) {
	convertedCredentialSpec, runtimeConfigRef, err := convertCredentialSpec(credentialSpec, configMetaByKey)
	if err != nil {
		return nil, nil, err
	}

	privileges := &mobySwarm.Privileges{CredentialSpec: convertedCredentialSpec}
	hasPrivileges := convertedCredentialSpec != nil
	for _, securityOption := range securityOptions {
		applied, applySecurityOptionErr := applySecurityOption(privileges, securityOption)
		if applySecurityOptionErr != nil {
			return nil, nil, applySecurityOptionErr
		}
		if applied {
			hasPrivileges = true
		}
	}

	return kit.Ternary(!hasPrivileges, nil, privileges), runtimeConfigRef, nil
}

func convertCredentialSpec(
	credentialSpec *composeTypes.CredentialSpecConfig,
	configMetaByKey map[string]resourceMeta,
) (*mobySwarm.CredentialSpec, *mobySwarm.ConfigReference, error) {
	if credentialSpec == nil {
		return nil, nil, nil
	}

	configured := 0
	if credentialSpec.Config != "" {
		configured++
	}
	if credentialSpec.File != "" {
		configured++
	}
	if credentialSpec.Registry != "" {
		configured++
	}
	if configured > 1 {
		return nil, nil, errors.New("credential spec can only set one of config, file, or registry")
	}
	if configured == 0 {
		return nil, nil, nil
	}

	converted := &mobySwarm.CredentialSpec{
		File:     credentialSpec.File,
		Registry: credentialSpec.Registry,
	}
	if credentialSpec.Config == "" {
		return converted, nil, nil
	}

	meta, ok := configMetaByKey[credentialSpec.Config]
	if !ok {
		return nil, nil, fmt.Errorf("undefined config %q for credential spec", credentialSpec.Config)
	}
	converted.Config = meta.ID
	return converted, &mobySwarm.ConfigReference{
		ConfigID:   meta.ID,
		ConfigName: meta.Name,
		Runtime:    &mobySwarm.ConfigReferenceRuntimeTarget{},
	}, nil
}

func applySecurityOption(privileges *mobySwarm.Privileges, securityOption string) (bool, error) {
	option := strings.TrimSpace(securityOption)
	if option == "" {
		return false, nil
	}
	key, value, found := strings.Cut(option, "=")
	if !found {
		key, value, found = strings.Cut(option, ":")
	}
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)

	switch key {
	case "no-new-privileges":
		enabled := true
		if found && value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, fmt.Errorf("invalid no-new-privileges value %q: %w", value, err)
			}
			enabled = parsed
		}
		privileges.NoNewPrivileges = enabled
	case "seccomp":
		switch strings.ToLower(value) {
		case "", "default":
			privileges.Seccomp = &mobySwarm.SeccompOpts{Mode: mobySwarm.SeccompModeDefault}
		case "unconfined":
			privileges.Seccomp = &mobySwarm.SeccompOpts{Mode: mobySwarm.SeccompModeUnconfined}
		default:
			return false, fmt.Errorf("custom seccomp profile %q is not supported for swarm stacks", value)
		}
	case "apparmor":
		switch strings.ToLower(value) {
		case "", "default":
			privileges.AppArmor = &mobySwarm.AppArmorOpts{Mode: mobySwarm.AppArmorModeDefault}
		case "unconfined", "disabled":
			privileges.AppArmor = &mobySwarm.AppArmorOpts{Mode: mobySwarm.AppArmorModeDisabled}
		default:
			return false, fmt.Errorf("custom AppArmor profile %q is not supported for swarm stacks", value)
		}
	case "label":
		if err := applySELinuxOption(privileges, value, securityOption); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported swarm security option %q", securityOption)
	}
	return true, nil
}

func applySELinuxOption(privileges *mobySwarm.Privileges, value, securityOption string) error {
	selinux := privileges.SELinuxContext
	if selinux == nil {
		selinux = &mobySwarm.SELinuxContext{}
		privileges.SELinuxContext = selinux
	}
	labelKey, labelValue, hasLabelValue := strings.Cut(value, ":")
	switch strings.ToLower(labelKey) {
	case "disable":
		selinux.Disable = true
	case "user":
		selinux.User = labelValue
	case "role":
		selinux.Role = labelValue
	case "type":
		selinux.Type = labelValue
	case "level":
		selinux.Level = labelValue
	default:
		return fmt.Errorf("invalid SELinux security option %q", securityOption)
	}
	if strings.ToLower(labelKey) != "disable" && !hasLabelValue {
		return fmt.Errorf("invalid SELinux security option %q", securityOption)
	}
	return nil
}

func convertUlimits(ulimits map[string]*composeTypes.UlimitsConfig) []*mobyContainer.Ulimit {
	result := make([]*mobyContainer.Ulimit, 0, len(ulimits))
	for _, name := range slices.Sorted(maps.Keys(ulimits)) {
		limit := ulimits[name]
		if limit == nil {
			continue
		}
		soft, hard := int64(limit.Soft), int64(limit.Hard)
		if limit.Single != 0 {
			soft = int64(limit.Single)
			hard = int64(limit.Single)
		}
		result = append(result, &mobyContainer.Ulimit{Name: name, Soft: soft, Hard: hard})
	}
	slices.SortFunc(result, func(a, b *mobyContainer.Ulimit) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return result
}

func applyDeployConfig(spec *mobySwarm.ServiceSpec, deploy *composeTypes.DeployConfig, scale *int, restart string) error {
	if deploy == nil {
		if err := applyServiceMode(spec, "", scale); err != nil {
			return err
		}
		restartPolicy, err := convertRestartPolicy(restart, nil)
		if err != nil {
			return err
		}
		spec.TaskTemplate.RestartPolicy = restartPolicy
		return nil
	}

	replicas := scale
	if deploy.Replicas != nil {
		replicas = deploy.Replicas
	}
	if err := applyServiceMode(spec, deploy.Mode, replicas); err != nil {
		return err
	}

	if deploy.EndpointMode != "" {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &mobySwarm.EndpointSpec{}
		}
		spec.EndpointSpec.Mode = mobySwarm.ResolutionMode(strings.ToLower(deploy.EndpointMode))
	}

	spec.UpdateConfig = convertUpdateConfig(deploy.UpdateConfig)
	spec.RollbackConfig = convertUpdateConfig(deploy.RollbackConfig)

	restartPolicy, err := convertRestartPolicy(restart, deploy.RestartPolicy)
	if err != nil {
		return err
	}
	spec.TaskTemplate.RestartPolicy = restartPolicy

	if deploy.Resources.Limits != nil || deploy.Resources.Reservations != nil {
		spec.TaskTemplate.Resources = &mobySwarm.ResourceRequirements{}
	}
	if deploy.Resources.Limits != nil {
		spec.TaskTemplate.Resources.Limits = &mobySwarm.Limit{
			NanoCPUs:    int64(deploy.Resources.Limits.NanoCPUs * 1e9),
			MemoryBytes: int64(deploy.Resources.Limits.MemoryBytes),
			Pids:        deploy.Resources.Limits.Pids,
		}
	}
	if deploy.Resources.Reservations != nil {
		spec.TaskTemplate.Resources.Reservations = &mobySwarm.Resources{
			NanoCPUs:    int64(deploy.Resources.Reservations.NanoCPUs * 1e9),
			MemoryBytes: int64(deploy.Resources.Reservations.MemoryBytes),
		}
	}

	if len(deploy.Placement.Constraints) > 0 || len(deploy.Placement.Preferences) > 0 || deploy.Placement.MaxReplicas > 0 {
		spec.TaskTemplate.Placement = &mobySwarm.Placement{
			Constraints: deploy.Placement.Constraints,
			MaxReplicas: deploy.Placement.MaxReplicas,
		}
		if len(deploy.Placement.Preferences) > 0 {
			preferences := make([]mobySwarm.PlacementPreference, 0, len(deploy.Placement.Preferences))
			for _, preference := range deploy.Placement.Preferences {
				if spread := strings.TrimSpace(preference.Spread); spread != "" {
					preferences = append(preferences, mobySwarm.PlacementPreference{
						Spread: &mobySwarm.SpreadOver{SpreadDescriptor: spread},
					})
				}
			}
			spec.TaskTemplate.Placement.Preferences = preferences
		}
	}

	return nil
}

func convertUpdateConfig(source *composeTypes.UpdateConfig) *mobySwarm.UpdateConfig {
	if source == nil {
		return nil
	}
	parallelism := uint64(1)
	if source.Parallelism != nil {
		parallelism = *source.Parallelism
	}
	return &mobySwarm.UpdateConfig{
		Parallelism:     parallelism,
		Delay:           time.Duration(source.Delay),
		FailureAction:   mobySwarm.FailureAction(source.FailureAction),
		Monitor:         time.Duration(source.Monitor),
		MaxFailureRatio: source.MaxFailureRatio,
		Order:           mobySwarm.UpdateOrder(source.Order),
	}
}

func convertRestartPolicy(restart string, deployPolicy *composeTypes.RestartPolicy) (*mobySwarm.RestartPolicy, error) {
	if deployPolicy != nil {
		return &mobySwarm.RestartPolicy{
			Condition:   mobySwarm.RestartPolicyCondition(deployPolicy.Condition),
			Delay:       convertDurationPtr(deployPolicy.Delay),
			MaxAttempts: deployPolicy.MaxAttempts,
			Window:      convertDurationPtr(deployPolicy.Window),
		}, nil
	}
	if strings.TrimSpace(restart) == "" {
		return nil, nil
	}

	policy, err := dockeropts.ParseRestartPolicy(restart)
	if err != nil {
		return nil, err
	}
	if policy.MaximumRetryCount < 0 {
		return nil, errors.New("invalid restart policy: maximum retry count cannot be negative")
	}
	switch policy.Name {
	case mobyContainer.RestartPolicyDisabled, "":
		return nil, nil
	case mobyContainer.RestartPolicyAlways, mobyContainer.RestartPolicyUnlessStopped:
		return &mobySwarm.RestartPolicy{Condition: mobySwarm.RestartPolicyConditionAny}, nil
	case mobyContainer.RestartPolicyOnFailure:
		maxAttempts := uint64(policy.MaximumRetryCount)
		return &mobySwarm.RestartPolicy{Condition: mobySwarm.RestartPolicyConditionOnFailure, MaxAttempts: &maxAttempts}, nil
	default:
		return nil, fmt.Errorf("invalid restart policy %q", policy.Name)
	}
}

func applyServiceMode(spec *mobySwarm.ServiceSpec, mode string, replicas *int) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "global":
		if replicas != nil {
			return errors.New("replicas can only be used with replicated or replicated-job mode")
		}
		spec.Mode = mobySwarm.ServiceMode{Global: &mobySwarm.GlobalService{}}
	case "global-job":
		if replicas != nil {
			return errors.New("replicas can only be used with replicated or replicated-job mode")
		}
		spec.Mode = mobySwarm.ServiceMode{GlobalJob: &mobySwarm.GlobalJob{}}
	case "replicated-job":
		var replicaCount *uint64
		if replicas != nil && *replicas >= 0 {
			replicaCount = new(uint64(*replicas))
		}
		spec.Mode = mobySwarm.ServiceMode{ReplicatedJob: &mobySwarm.ReplicatedJob{
			MaxConcurrent:    replicaCount,
			TotalCompletions: replicaCount,
		}}
	case "replicated", "":
		replicaCount := uint64(1)
		if replicasInput := replicas; replicasInput != nil {
			if *replicasInput <= 0 {
				replicaCount = 0
			} else {
				replicaCount = uint64(*replicasInput)
			}
		}
		spec.Mode = mobySwarm.ServiceMode{Replicated: &mobySwarm.ReplicatedService{Replicas: &replicaCount}}
	default:
		return fmt.Errorf("unknown service mode %q", mode)
	}
	return nil
}

func applyServicePorts(spec *mobySwarm.ServiceSpec, ports []composeTypes.ServicePortConfig) error {
	if len(ports) == 0 {
		return nil
	}

	endpoint := spec.EndpointSpec
	if endpoint == nil {
		endpoint = &mobySwarm.EndpointSpec{}
	}

	converted := make([]mobySwarm.PortConfig, 0, len(ports))
	for _, port := range ports {
		if strings.TrimSpace(port.HostIP) != "" {
			slog.WarnContext(context.Background(), "swarm ignores a compose port host IP", //nolint:forbidigo // Compose-to-swarm conversion is a pure transformation without a request context.
				"hostIp", port.HostIP, "targetPort", port.Target)
		}
		entry := mobySwarm.PortConfig{
			Name:       port.Name,
			Protocol:   mobyNetwork.IPProtocol(strings.ToLower(port.Protocol)),
			TargetPort: port.Target,
		}
		if port.Mode != "" {
			entry.PublishMode = mobySwarm.PortConfigPublishMode(strings.ToLower(port.Mode))
		}
		if port.Published != "" {
			published, err := strconv.ParseUint(port.Published, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid published port %q: %w", port.Published, err)
			}
			entry.PublishedPort = uint32(published)
		}
		converted = append(converted, entry)
	}

	slices.SortFunc(converted, mobySwarm.PortConfig.Compare)
	endpoint.Ports = converted
	spec.EndpointSpec = endpoint
	return nil
}

func buildServiceNetworks(service composeTypes.ServiceConfig, networkNameByKey map[string]string) ([]mobySwarm.NetworkAttachmentConfig, error) {
	var attachments []mobySwarm.NetworkAttachmentConfig
	if len(service.Networks) == 0 {
		defaultNetwork, ok := networkNameByKey["default"]
		if !ok {
			return nil, errors.New("undefined network \"default\"")
		}
		return []mobySwarm.NetworkAttachmentConfig{{
			Target:  defaultNetwork,
			Aliases: []string{service.Name},
		}}, nil
	}

	for name, cfg := range service.Networks {
		networkName, ok := networkNameByKey[name]
		if !ok {
			return nil, fmt.Errorf("undefined network %q", name)
		}
		aliases := []string{service.Name}
		if cfg == nil {
			attachments = append(attachments, mobySwarm.NetworkAttachmentConfig{Target: networkName, Aliases: aliases})
			continue
		}
		aliases = append(slices.Clone(cfg.Aliases), service.Name)
		slices.Sort(aliases)
		aliases = slices.Compact(aliases)
		attachments = append(attachments, mobySwarm.NetworkAttachmentConfig{
			Target:     networkName,
			Aliases:    aliases,
			DriverOpts: cfg.DriverOpts,
		})
	}
	slices.SortFunc(attachments, func(a, b mobySwarm.NetworkAttachmentConfig) int {
		return strings.Compare(a.Target, b.Target)
	})
	return attachments, nil
}

func convertIPAM(cfg composeTypes.IPAMConfig) *mobyNetwork.IPAM {
	if cfg.Driver == "" && len(cfg.Config) == 0 && len(cfg.Options) == 0 {
		return nil
	}
	result := &mobyNetwork.IPAM{
		Driver:  cfg.Driver,
		Options: cfg.Options,
	}
	if len(cfg.Config) > 0 {
		pools := make([]mobyNetwork.IPAMConfig, 0, len(cfg.Config))
		for _, pool := range cfg.Config {
			if pool == nil {
				continue
			}
			subnet := parsePrefix(pool.Subnet)
			ipRange := parsePrefix(pool.IPRange)
			gateway := parseAddr(pool.Gateway).OrEmpty()
			pools = append(pools, mobyNetwork.IPAMConfig{
				Subnet:     subnet,
				Gateway:    gateway,
				IPRange:    ipRange,
				AuxAddress: parseAuxAddresses(pool.AuxiliaryAddresses),
			})
		}
		result.Config = pools
	}
	return result
}

func parseIPList(values []string) []netip.Addr {
	if len(values) == 0 {
		return nil
	}
	out := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		addr, ok := parseAddr(value).Get()
		if ok {
			out = append(out, addr)
		}
	}
	return out
}

func parseAddr(value string) mo.Option[netip.Addr] {
	value = strings.TrimSpace(value)
	if value == "" {
		return mo.None[netip.Addr]()
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return mo.None[netip.Addr]()
	}
	return mo.Some(addr)
}

func parsePrefix(value string) netip.Prefix {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Prefix{}
	}
	prefix, err := netip.ParsePrefix(value)
	return kit.Ternary(err != nil, netip.Prefix{}, prefix)
}

func parseAuxAddresses(values map[string]string) map[string]netip.Addr {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]netip.Addr, len(values))
	for key, value := range values {
		if addr, ok := parseAddr(value).Get(); ok {
			out[key] = addr
		}
	}
	return out
}

func convertEnv(env composeTypes.MappingWithEquals) []string {
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
	slices.Sort(result)
	return result
}

func convertDurationPtr(duration *composeTypes.Duration) *time.Duration {
	if duration == nil {
		return nil
	}
	return new(time.Duration(*duration))
}
