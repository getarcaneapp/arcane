package swarm

import (
	"cmp"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	dockeropts "github.com/docker/cli/opts"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/samber/mo"
)

func buildServiceSpecInternal(
	service composegotypes.ServiceConfig,
	ns namespace,
	stackLabels map[string]string,
	networkNameByKey map[string]string,
	configMetaByKey map[string]resourceMeta,
	secretMetaByKey map[string]resourceMeta,
	projectVolumes composegotypes.Volumes,
) (swarm.ServiceSpec, error) {
	serviceName := ns.Scope(service.Name)
	serviceLabels := mergeLabelsInternal(nil, stackLabels)
	if service.Deploy != nil {
		serviceLabels = mergeLabelsInternal(service.Deploy.Labels, serviceLabels)
	}

	mounts, err := convertServiceMountsInternal(service.Volumes, ns, projectVolumes, stackLabels)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}
	secretRefs, err := convertServiceSecretRefsInternal(service.Secrets, secretMetaByKey)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}
	configRefs, err := convertServiceConfigRefsInternal(service.Configs, configMetaByKey)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}
	privileges, runtimeConfigRef, err := convertPrivilegesInternal(service.CredentialSpec, service.SecurityOpt, configMetaByKey)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}
	if runtimeConfigRef != nil {
		configRefs = append(configRefs, runtimeConfigRef)
		sort.Slice(configRefs, func(i, j int) bool {
			return configRefs[i].ConfigName < configRefs[j].ConfigName
		})
	}
	healthcheck, err := convertHealthcheckInternal(service.HealthCheck)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}
	networks, err := buildServiceNetworksInternal(service, networkNameByKey)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}
	capabilityAdd, capabilityDrop := dockeropts.EffectiveCapAddCapDrop(service.CapAdd, service.CapDrop)

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   serviceName,
			Labels: serviceLabels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:           service.Image,
				Command:         toStringSliceInternal(service.Entrypoint),
				Args:            toStringSliceInternal(service.Command),
				Env:             convertEnvInternal(service.Environment),
				Dir:             service.WorkingDir,
				User:            service.User,
				Groups:          service.GroupAdd,
				Hostname:        service.Hostname,
				Init:            service.Init,
				StopSignal:      service.StopSignal,
				StopGracePeriod: convertDurationPtrInternal(service.StopGracePeriod),
				ReadOnly:        service.ReadOnly,
				TTY:             service.Tty,
				OpenStdin:       service.StdinOpen,
				Labels:          mergeLabelsInternal(service.Labels, stackLabels),
				Mounts:          mounts,
				Secrets:         secretRefs,
				Configs:         configRefs,
				Hosts:           convertExtraHostsInternal(service.ExtraHosts),
				Healthcheck:     healthcheck,
				Privileges:      privileges,
				Isolation:       container.Isolation(service.Isolation),
				Sysctls:         map[string]string(service.Sysctls),
				CapabilityAdd:   capabilityAdd,
				CapabilityDrop:  capabilityDrop,
				Ulimits:         convertUlimitsInternal(service.Ulimits),
				OomScoreAdj:     service.OomScoreAdj,
			},
			Networks: networks,
			Runtime:  swarm.RuntimeType(service.Runtime),
		},
	}

	if service.Logging != nil {
		spec.TaskTemplate.LogDriver = &swarm.Driver{
			Name:    service.Logging.Driver,
			Options: service.Logging.Options,
		}
	}

	if len(service.DNS) > 0 || len(service.DNSSearch) > 0 || len(service.DNSOpts) > 0 {
		spec.TaskTemplate.ContainerSpec.DNSConfig = &swarm.DNSConfig{
			Nameservers: parseIPListInternal(service.DNS),
			Search:      service.DNSSearch,
			Options:     service.DNSOpts,
		}
	}

	if err := applyDeployConfigInternal(&spec, service.Deploy, service.Scale, service.Restart); err != nil {
		return swarm.ServiceSpec{}, err
	}
	if err := applyServicePortsInternal(&spec, service.Ports); err != nil {
		return swarm.ServiceSpec{}, err
	}

	return spec, nil
}

func convertExtraHostsInternal(extraHosts composegotypes.HostsList) []string {
	hosts := make([]string, 0, len(extraHosts))
	for hostName, addresses := range extraHosts {
		for _, address := range addresses {
			hosts = append(hosts, address+" "+hostName)
		}
	}
	slices.Sort(hosts)
	return hosts
}

func convertHealthcheckInternal(healthcheck *composegotypes.HealthCheckConfig) (*container.HealthConfig, error) {
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
		return &container.HealthConfig{Test: []string{"NONE"}}, nil
	}

	result := &container.HealthConfig{Test: healthcheck.Test}
	if healthcheck.Timeout != nil {
		result.Timeout = time.Duration(*healthcheck.Timeout)
	}
	if healthcheck.Interval != nil {
		result.Interval = time.Duration(*healthcheck.Interval)
	}
	if healthcheck.Retries != nil {
		if *healthcheck.Retries > uint64(math.MaxInt) {
			return nil, errors.Errorf("healthcheck retries %d exceeds the platform int range", *healthcheck.Retries)
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

func convertPrivilegesInternal(
	credentialSpec *composegotypes.CredentialSpecConfig,
	securityOptions []string,
	configMetaByKey map[string]resourceMeta,
) (*swarm.Privileges, *swarm.ConfigReference, error) {
	convertedCredentialSpec, runtimeConfigRef, err := convertCredentialSpecInternal(credentialSpec, configMetaByKey)
	if err != nil {
		return nil, nil, err
	}

	privileges := &swarm.Privileges{CredentialSpec: convertedCredentialSpec}
	hasPrivileges := convertedCredentialSpec != nil
	for _, securityOption := range securityOptions {
		applied, err := applySecurityOptionInternal(privileges, securityOption)
		if err != nil {
			return nil, nil, err
		}
		if applied {
			hasPrivileges = true
		}
	}

	if !hasPrivileges {
		return nil, runtimeConfigRef, nil
	}
	return privileges, runtimeConfigRef, nil
}

func convertCredentialSpecInternal(
	credentialSpec *composegotypes.CredentialSpecConfig,
	configMetaByKey map[string]resourceMeta,
) (*swarm.CredentialSpec, *swarm.ConfigReference, error) {
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

	converted := &swarm.CredentialSpec{
		File:     credentialSpec.File,
		Registry: credentialSpec.Registry,
	}
	if credentialSpec.Config == "" {
		return converted, nil, nil
	}

	meta, ok := configMetaByKey[credentialSpec.Config]
	if !ok {
		return nil, nil, errors.Errorf("undefined config %q for credential spec", credentialSpec.Config)
	}
	converted.Config = meta.ID
	return converted, &swarm.ConfigReference{
		ConfigID:   meta.ID,
		ConfigName: meta.Name,
		Runtime:    &swarm.ConfigReferenceRuntimeTarget{},
	}, nil
}

func applySecurityOptionInternal(privileges *swarm.Privileges, securityOption string) (bool, error) {
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
				return false, errors.WrapIff(err, "invalid no-new-privileges value %q", value)
			}
			enabled = parsed
		}
		privileges.NoNewPrivileges = enabled
	case "seccomp":
		switch strings.ToLower(value) {
		case "", "default":
			privileges.Seccomp = &swarm.SeccompOpts{Mode: swarm.SeccompModeDefault}
		case "unconfined":
			privileges.Seccomp = &swarm.SeccompOpts{Mode: swarm.SeccompModeUnconfined}
		default:
			return false, errors.Errorf("custom seccomp profile %q is not supported for swarm stacks", value)
		}
	case "apparmor":
		switch strings.ToLower(value) {
		case "", "default":
			privileges.AppArmor = &swarm.AppArmorOpts{Mode: swarm.AppArmorModeDefault}
		case "unconfined", "disabled":
			privileges.AppArmor = &swarm.AppArmorOpts{Mode: swarm.AppArmorModeDisabled}
		default:
			return false, errors.Errorf("custom AppArmor profile %q is not supported for swarm stacks", value)
		}
	case "label":
		if err := applySELinuxOptionInternal(privileges, value, securityOption); err != nil {
			return false, err
		}
	default:
		return false, errors.Errorf("unsupported swarm security option %q", securityOption)
	}
	return true, nil
}

func applySELinuxOptionInternal(privileges *swarm.Privileges, value, securityOption string) error {
	selinux := privileges.SELinuxContext
	if selinux == nil {
		selinux = &swarm.SELinuxContext{}
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
		return errors.Errorf("invalid SELinux security option %q", securityOption)
	}
	if strings.ToLower(labelKey) != "disable" && !hasLabelValue {
		return errors.Errorf("invalid SELinux security option %q", securityOption)
	}
	return nil
}

func convertUlimitsInternal(ulimits map[string]*composegotypes.UlimitsConfig) []*container.Ulimit {
	result := make([]*container.Ulimit, 0, len(ulimits))
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
		result = append(result, &container.Ulimit{Name: name, Soft: soft, Hard: hard})
	}
	slices.SortFunc(result, func(a, b *container.Ulimit) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return result
}

func applyDeployConfigInternal(spec *swarm.ServiceSpec, deploy *composegotypes.DeployConfig, scale *int, restart string) error {
	if deploy == nil {
		if err := applyServiceModeInternal(spec, "", scale); err != nil {
			return err
		}
		restartPolicy, err := convertRestartPolicyInternal(restart, nil)
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
	if err := applyServiceModeInternal(spec, deploy.Mode, replicas); err != nil {
		return err
	}

	if deploy.EndpointMode != "" {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		spec.EndpointSpec.Mode = swarm.ResolutionMode(strings.ToLower(deploy.EndpointMode))
	}

	spec.UpdateConfig = convertUpdateConfigInternal(deploy.UpdateConfig)
	spec.RollbackConfig = convertUpdateConfigInternal(deploy.RollbackConfig)

	restartPolicy, err := convertRestartPolicyInternal(restart, deploy.RestartPolicy)
	if err != nil {
		return err
	}
	spec.TaskTemplate.RestartPolicy = restartPolicy

	if deploy.Resources.Limits != nil || deploy.Resources.Reservations != nil {
		spec.TaskTemplate.Resources = &swarm.ResourceRequirements{}
	}
	if deploy.Resources.Limits != nil {
		spec.TaskTemplate.Resources.Limits = &swarm.Limit{
			NanoCPUs:    int64(deploy.Resources.Limits.NanoCPUs * 1e9),
			MemoryBytes: int64(deploy.Resources.Limits.MemoryBytes),
			Pids:        deploy.Resources.Limits.Pids,
		}
	}
	if deploy.Resources.Reservations != nil {
		spec.TaskTemplate.Resources.Reservations = &swarm.Resources{
			NanoCPUs:    int64(deploy.Resources.Reservations.NanoCPUs * 1e9),
			MemoryBytes: int64(deploy.Resources.Reservations.MemoryBytes),
		}
	}

	if len(deploy.Placement.Constraints) > 0 || len(deploy.Placement.Preferences) > 0 || deploy.Placement.MaxReplicas > 0 {
		spec.TaskTemplate.Placement = &swarm.Placement{
			Constraints: deploy.Placement.Constraints,
			MaxReplicas: deploy.Placement.MaxReplicas,
		}
		if len(deploy.Placement.Preferences) > 0 {
			preferences := make([]swarm.PlacementPreference, 0, len(deploy.Placement.Preferences))
			for _, preference := range deploy.Placement.Preferences {
				if spread := strings.TrimSpace(preference.Spread); spread != "" {
					preferences = append(preferences, swarm.PlacementPreference{
						Spread: &swarm.SpreadOver{SpreadDescriptor: spread},
					})
				}
			}
			spec.TaskTemplate.Placement.Preferences = preferences
		}
	}

	return nil
}

func convertUpdateConfigInternal(source *composegotypes.UpdateConfig) *swarm.UpdateConfig {
	if source == nil {
		return nil
	}
	parallelism := uint64(1)
	if source.Parallelism != nil {
		parallelism = *source.Parallelism
	}
	return &swarm.UpdateConfig{
		Parallelism:     parallelism,
		Delay:           time.Duration(source.Delay),
		FailureAction:   swarm.FailureAction(source.FailureAction),
		Monitor:         time.Duration(source.Monitor),
		MaxFailureRatio: source.MaxFailureRatio,
		Order:           swarm.UpdateOrder(source.Order),
	}
}

func convertRestartPolicyInternal(restart string, deployPolicy *composegotypes.RestartPolicy) (*swarm.RestartPolicy, error) {
	if deployPolicy != nil {
		return &swarm.RestartPolicy{
			Condition:   swarm.RestartPolicyCondition(deployPolicy.Condition),
			Delay:       convertDurationPtrInternal(deployPolicy.Delay),
			MaxAttempts: deployPolicy.MaxAttempts,
			Window:      convertDurationPtrInternal(deployPolicy.Window),
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
	case container.RestartPolicyDisabled, "":
		return nil, nil
	case container.RestartPolicyAlways, container.RestartPolicyUnlessStopped:
		return &swarm.RestartPolicy{Condition: swarm.RestartPolicyConditionAny}, nil
	case container.RestartPolicyOnFailure:
		maxAttempts := uint64(policy.MaximumRetryCount)
		return &swarm.RestartPolicy{Condition: swarm.RestartPolicyConditionOnFailure, MaxAttempts: &maxAttempts}, nil
	default:
		return nil, errors.Errorf("invalid restart policy %q", policy.Name)
	}
}

func applyServiceModeInternal(spec *swarm.ServiceSpec, mode string, replicas *int) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "global":
		if replicas != nil {
			return errors.New("replicas can only be used with replicated or replicated-job mode")
		}
		spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	case "global-job":
		if replicas != nil {
			return errors.New("replicas can only be used with replicated or replicated-job mode")
		}
		spec.Mode = swarm.ServiceMode{GlobalJob: &swarm.GlobalJob{}}
	case "replicated-job":
		replicaCount := toUint64PointerFromOptionalIntInternal(replicas)
		spec.Mode = swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{
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
		spec.Mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicaCount}}
	default:
		return errors.Errorf("unknown service mode %q", mode)
	}
	return nil
}

func toUint64PointerFromOptionalIntInternal(value *int) *uint64 {
	if value == nil || *value < 0 {
		return nil
	}
	return new(uint64(*value))
}

func applyServicePortsInternal(spec *swarm.ServiceSpec, ports []composegotypes.ServicePortConfig) error {
	if len(ports) == 0 {
		return nil
	}

	endpoint := spec.EndpointSpec
	if endpoint == nil {
		endpoint = &swarm.EndpointSpec{}
	}

	converted := make([]swarm.PortConfig, 0, len(ports))
	for _, port := range ports {
		if strings.TrimSpace(port.HostIP) != "" {
			slog.Warn("swarm ignores a compose port host IP", "hostIP", port.HostIP, "targetPort", port.Target)
		}
		entry := swarm.PortConfig{
			Name:       port.Name,
			Protocol:   network.IPProtocol(strings.ToLower(port.Protocol)),
			TargetPort: port.Target,
		}
		if port.Mode != "" {
			entry.PublishMode = swarm.PortConfigPublishMode(strings.ToLower(port.Mode))
		}
		if port.Published != "" {
			published, err := strconv.ParseUint(port.Published, 10, 32)
			if err != nil {
				return errors.WrapIff(err, "invalid published port %q", port.Published)
			}
			entry.PublishedPort = uint32(published)
		}
		converted = append(converted, entry)
	}

	slices.SortFunc(converted, swarm.PortConfig.Compare)
	endpoint.Ports = converted
	spec.EndpointSpec = endpoint
	return nil
}

func buildServiceNetworksInternal(service composegotypes.ServiceConfig, networkNameByKey map[string]string) ([]swarm.NetworkAttachmentConfig, error) {
	var attachments []swarm.NetworkAttachmentConfig
	if len(service.Networks) == 0 {
		defaultNetwork, ok := networkNameByKey["default"]
		if !ok {
			return nil, errors.New("undefined network \"default\"")
		}
		return []swarm.NetworkAttachmentConfig{{
			Target:  defaultNetwork,
			Aliases: []string{service.Name},
		}}, nil
	}

	for name, cfg := range service.Networks {
		networkName, ok := networkNameByKey[name]
		if !ok {
			return nil, errors.Errorf("undefined network %q", name)
		}
		aliases := []string{service.Name}
		if cfg == nil {
			attachments = append(attachments, swarm.NetworkAttachmentConfig{Target: networkName, Aliases: aliases})
			continue
		}
		aliases = append(slices.Clone(cfg.Aliases), service.Name)
		slices.Sort(aliases)
		aliases = slices.Compact(aliases)
		attachments = append(attachments, swarm.NetworkAttachmentConfig{
			Target:     networkName,
			Aliases:    aliases,
			DriverOpts: cfg.DriverOpts,
		})
	}
	sort.Slice(attachments, func(i, j int) bool {
		return attachments[i].Target < attachments[j].Target
	})
	return attachments, nil
}

func convertIPAMInternal(cfg composegotypes.IPAMConfig) *network.IPAM {
	if cfg.Driver == "" && len(cfg.Config) == 0 && len(cfg.Options) == 0 {
		return nil
	}
	result := &network.IPAM{
		Driver:  cfg.Driver,
		Options: cfg.Options,
	}
	if len(cfg.Config) > 0 {
		pools := make([]network.IPAMConfig, 0, len(cfg.Config))
		for _, pool := range cfg.Config {
			if pool == nil {
				continue
			}
			subnet := parsePrefixInternal(pool.Subnet)
			ipRange := parsePrefixInternal(pool.IPRange)
			gateway := parseAddrInternal(pool.Gateway).OrEmpty()
			pools = append(pools, network.IPAMConfig{
				Subnet:     subnet,
				Gateway:    gateway,
				IPRange:    ipRange,
				AuxAddress: parseAuxAddressesInternal(pool.AuxiliaryAddresses),
			})
		}
		result.Config = pools
	}
	return result
}

func parseIPListInternal(values []string) []netip.Addr {
	if len(values) == 0 {
		return nil
	}
	out := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		addr, ok := parseAddrInternal(value).Get()
		if ok {
			out = append(out, addr)
		}
	}
	return out
}

func parseAddrInternal(value string) mo.Option[netip.Addr] {
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

func parsePrefixInternal(value string) netip.Prefix {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Prefix{}
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}
	}
	return prefix
}

func parseAuxAddressesInternal(values map[string]string) map[string]netip.Addr {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]netip.Addr, len(values))
	for key, value := range values {
		if addr, ok := parseAddrInternal(value).Get(); ok {
			out[key] = addr
		}
	}
	return out
}

func convertEnvInternal(env composegotypes.MappingWithEquals) []string {
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

func toStringSliceInternal(command composegotypes.ShellCommand) []string {
	if len(command) == 0 {
		return nil
	}
	return command
}

func convertDurationPtrInternal(duration *composegotypes.Duration) *time.Duration {
	if duration == nil {
		return nil
	}
	return new(time.Duration(*duration))
}
