package podman

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	internaltypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

var podNetworkModes = []string{"bridge", "host", "none", "private", "slirp4netns", "pasta", "ns"}

type podCreateOptions struct {
	name               string
	hostname           string
	labels             map[string]string
	labelFiles         []string
	exitPolicy         string
	restart            string
	infra              bool
	infraImage         string
	infraCommand       string
	infraName          string
	infraConmonPidFile string
	share              string
	shareParent        bool
	pid                string
	ipc                string
	uts                string
	userns             string
	uidMap             []string
	gidMap             []string
	networks           []string
	networkAliases     []string
	ip                 string
	ip6                string
	macAddress         string
	publish            []string
	dns                []string
	dnsSearch          []string
	dnsOptions         []string
	addHosts           []string
	noHosts            bool
	noHostname         bool
	hostsFile          string
	cgroupParent       string
	cpus               float64
	cpuShares          uint64
	cpusetCPUs         string
	cpusetMems         string
	memory             string
	memorySwap         string
	blkioWeight        uint16
	blkioWeightDevices []string
	deviceReadBps      []string
	deviceWriteBps     []string
	devices            []string
	gpus               []string
	volumes            []string
	volumesFrom        []string
	shmSize            string
	shmSizeSystemd     string
	securityOpt        []string
	sysctl             map[string]string
}

func newPodCreateCmd() *cobra.Command {
	var opts podCreateOptions
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a pod",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := opts.request(cmd)
			if err != nil {
				return err
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodCreateResponse]{
				Method:         http.MethodPost,
				Path:           internaltypes.PodmanPods(c.EnvID()),
				Body:           req,
				FailureMessage: "failed to create pod",
				Render: func(data types.PodCreateResponse) error {
					output.Success("Pod %s created successfully", opts.name)
					output.KeyValue("ID", data.ID)
					return nil
				},
			})
		},
	}
	flags := cmd.Flags()
	flags.StringVarP(&opts.name, "name", "n", "", "Pod name (required)")
	flags.StringVar(&opts.hostname, "hostname", "", "Pod hostname")
	flags.StringToStringVarP(&opts.labels, "label", "l", nil, "Label (KEY=VALUE)")
	flags.StringArrayVar(&opts.labelFiles, "label-file", nil, "Read labels from a local file of KEY=VALUE lines")
	flags.StringVar(&opts.exitPolicy, "exit-policy", "", "Behaviour when the last container exits (continue or stop)")
	flags.StringVar(&opts.restart, "restart", "", "Restart policy for containers (no, always, on-failure[:N], unless-stopped)")
	flags.BoolVar(&opts.infra, "infra", true, "Create an infra container to share namespaces with")
	flags.StringVar(&opts.infraImage, "infra-image", "", "Image for the infra container")
	flags.StringVar(&opts.infraCommand, "infra-command", "", "Entrypoint of the infra container")
	flags.StringVar(&opts.infraName, "infra-name", "", "Name of the infra container")
	flags.StringVar(&opts.infraConmonPidFile, "infra-conmon-pidfile", "", "Path that receives the infra container's conmon PID")
	flags.StringVar(&opts.share, "share", "", "Comma-separated namespaces to share (default ipc,net,uts; prefix + to add to the default)")
	flags.BoolVar(&opts.shareParent, "share-parent", true, "Use the pod's cgroup as the cgroup parent of its containers")
	flags.StringVar(&opts.pid, "pid", "", "PID namespace (host, private, ns:PATH, container:ID)")
	flags.StringVar(&opts.ipc, "ipc", "", "IPC namespace (host, private, shareable, none, ns:PATH, container:ID)")
	flags.StringVar(&opts.uts, "uts", "", "UTS namespace (host, private, ns:PATH, container:ID)")
	flags.StringVar(&opts.userns, "userns", "", "User namespace (auto, keep-id[:OPTS], host, nomap, ns:PATH)")
	flags.StringSliceVar(&opts.uidMap, "uidmap", nil, "UID mapping CONTAINER:HOST:SIZE")
	flags.StringSliceVar(&opts.gidMap, "gidmap", nil, "GID mapping CONTAINER:HOST:SIZE")
	flags.StringArrayVar(&opts.networks, "network", nil, "Network mode (bridge, host, none, private, slirp4netns[:OPTS], pasta[:OPTS], ns:PATH) or network NAME[:alias=A,ip=IP,mac=MAC,interface_name=IF]")
	flags.StringSliceVar(&opts.networkAliases, "network-alias", nil, "Network-scoped alias on the --network")
	flags.StringVar(&opts.ip, "ip", "", "Static IPv4 address on the --network")
	flags.StringVar(&opts.ip6, "ip6", "", "Static IPv6 address on the --network")
	flags.StringVar(&opts.macAddress, "mac-address", "", "Static MAC address on the --network")
	flags.StringArrayVarP(&opts.publish, "publish", "p", nil, "Publish a port ([hostIp:][hostPort:]containerPort[/protocol])")
	flags.StringSliceVar(&opts.dns, "dns", nil, "DNS server, or none to keep the image's resolv.conf")
	flags.StringSliceVar(&opts.dnsSearch, "dns-search", nil, "DNS search domain")
	flags.StringSliceVar(&opts.dnsOptions, "dns-option", nil, "DNS resolver option")
	flags.StringSliceVar(&opts.addHosts, "add-host", nil, "Extra /etc/hosts entry (HOST:IP)")
	flags.BoolVar(&opts.noHosts, "no-hosts", false, "Use the image's /etc/hosts")
	flags.BoolVar(&opts.noHostname, "no-hostname", false, "Use the image's /etc/hostname")
	flags.StringVar(&opts.hostsFile, "hosts-file", "", "Base file for /etc/hosts, or image or none")
	flags.StringVar(&opts.cgroupParent, "cgroup-parent", "", "Parent cgroup of the pod")
	flags.Float64Var(&opts.cpus, "cpus", 0, "Number of CPUs")
	flags.Uint64VarP(&opts.cpuShares, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	flags.StringVar(&opts.cpusetCPUs, "cpuset-cpus", "", "CPUs allowed to run the pod (0-3, 0,1)")
	flags.StringVar(&opts.cpusetMems, "cpuset-mems", "", "Memory nodes allowed to run the pod (0-3, 0,1)")
	flags.StringVarP(&opts.memory, "memory", "m", "", "Memory limit (512m, 2g)")
	flags.StringVar(&opts.memorySwap, "memory-swap", "", "Memory plus swap limit, or -1 for unlimited swap")
	flags.Uint16Var(&opts.blkioWeight, "blkio-weight", 0, "Block IO weight (10-1000)")
	flags.StringArrayVar(&opts.blkioWeightDevices, "blkio-weight-device", nil, "Per-device block IO weight (MAJOR:MINOR:WEIGHT)")
	flags.StringArrayVar(&opts.deviceReadBps, "device-read-bps", nil, "Per-device read rate limit (MAJOR:MINOR:RATE, e.g. 8:0:1mb)")
	flags.StringArrayVar(&opts.deviceWriteBps, "device-write-bps", nil, "Per-device write rate limit (MAJOR:MINOR:RATE, e.g. 8:0:1mb)")
	flags.StringArrayVar(&opts.devices, "device", nil, "Host device (HOST[:CONTAINER[:PERMISSIONS]])")
	flags.StringSliceVar(&opts.gpus, "gpus", nil, "NVIDIA GPUs to add through CDI, or all")
	flags.StringArrayVarP(&opts.volumes, "volume", "v", nil, "Bind mount or named volume (SOURCE:DESTINATION[:OPTIONS])")
	flags.StringArrayVar(&opts.volumesFrom, "volumes-from", nil, "Mount volumes from a container (CONTAINER[:OPTIONS])")
	flags.StringVar(&opts.shmSize, "shm-size", "", "Size of /dev/shm (64m)")
	flags.StringVar(&opts.shmSizeSystemd, "shm-size-systemd", "", "Size of systemd tmpfs mounts (64m)")
	flags.StringArrayVar(&opts.securityOpt, "security-opt", nil, "Security option (label=disable, seccomp=unconfined)")
	flags.StringToStringVar(&opts.sysctl, "sysctl", nil, "Namespaced kernel parameter (KEY=VALUE)")
	_ = cmd.MarkFlagRequired("name")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func (o podCreateOptions) request(cmd *cobra.Command) (types.PodCreateRequest, error) {
	labels, err := o.allLabels()
	if err != nil {
		return types.PodCreateRequest{}, err
	}

	ports := make([]types.PodPortMapping, 0, len(o.publish))
	for _, spec := range o.publish {
		mappings, parseErr := nat.ParsePortSpec(spec)
		if parseErr != nil {
			return types.PodCreateRequest{}, fmt.Errorf("invalid --publish %q: %w", spec, parseErr)
		}
		for _, mapping := range mappings {
			hostPort, portErr := strconv.ParseUint(cmp.Or(mapping.Binding.HostPort, "0"), 10, 16)
			if portErr != nil {
				return types.PodCreateRequest{}, fmt.Errorf("invalid --publish %q: %w", spec, portErr)
			}
			ports = append(ports, types.PodPortMapping{
				ContainerPort: uint16(mapping.Port.Int()),
				HostPort:      uint16(hostPort),
				HostIP:        mapping.Binding.HostIP,
				Protocol:      mapping.Port.Proto(),
			})
		}
	}

	networkMode, networkOptions, networks, err := o.networkSettings()
	if err != nil {
		return types.PodCreateRequest{}, err
	}

	deviceLimits := func(flag string, values []string) ([]types.PodDeviceLimit, error) {
		out := make([]types.PodDeviceLimit, 0, len(values))
		for _, value := range values {
			index := strings.LastIndex(value, ":")
			if index <= 0 {
				return nil, fmt.Errorf("invalid --%s %q: use MAJOR:MINOR:VALUE", flag, value)
			}
			out = append(out, types.PodDeviceLimit{Device: value[:index], Value: value[index+1:]})
		}
		return out, nil
	}
	weightDevices, err := deviceLimits("blkio-weight-device", o.blkioWeightDevices)
	if err != nil {
		return types.PodCreateRequest{}, err
	}
	readBps, err := deviceLimits("device-read-bps", o.deviceReadBps)
	if err != nil {
		return types.PodCreateRequest{}, err
	}
	writeBps, err := deviceLimits("device-write-bps", o.deviceWriteBps)
	if err != nil {
		return types.PodCreateRequest{}, err
	}

	volumes := make([]types.PodVolume, 0, len(o.volumes))
	for _, value := range o.volumes {
		parts := strings.SplitN(value, ":", 3)
		if len(parts) < 2 {
			return types.PodCreateRequest{}, fmt.Errorf("invalid --volume %q: use SOURCE:DESTINATION[:OPTIONS]", value)
		}
		volumes = append(volumes, types.PodVolume{
			Source:      parts[0],
			Destination: parts[1],
			Options:     kit.Ternary(len(parts) == 3, strings.Split(parts[len(parts)-1], ","), nil),
		})
	}

	policy, retries, hasRetries := strings.Cut(o.restart, ":")
	restartRetries, err := strconv.ParseUint(cmp.Or(retries, "0"), 10, 32)
	if err != nil {
		return types.PodCreateRequest{}, fmt.Errorf("invalid --restart %q: %w", o.restart, err)
	}

	share := strings.TrimSpace(o.share)
	noShare := cmd.Flags().Changed("share") && (share == "" || share == "none")
	return types.PodCreateRequest{
		Name:               o.name,
		Hostname:           o.hostname,
		Labels:             kit.Ternary(len(labels) > 0, labels, nil),
		ExitPolicy:         o.exitPolicy,
		RestartPolicy:      kit.Ternary(policy == "never", "no", policy),
		RestartRetries:     kit.Ternary(hasRetries, new(uint(restartRetries)), nil),
		NoInfra:            !o.infra || (noShare && !cmd.Flags().Changed("infra")),
		InfraImage:         o.infraImage,
		InfraCommand:       strings.Fields(o.infraCommand),
		InfraName:          o.infraName,
		InfraConmonPidFile: o.infraConmonPidFile,
		SharedNamespaces: func() []string {
			if share == "" || noShare {
				return nil
			}
			if added, ok := strings.CutPrefix(share, "+"); ok {
				return append([]string{"ipc", "net", "uts"}, strings.Split(added, ",")...)
			}
			return strings.Split(share, ",")
		}(),
		ShareParent:    kit.Ternary(cmd.Flags().Changed("share-parent"), new(o.shareParent), nil),
		Pid:            o.pid,
		Ipc:            o.ipc,
		Uts:            o.uts,
		Userns:         o.userns,
		UIDMap:         o.uidMap,
		GIDMap:         o.gidMap,
		NetworkMode:    networkMode,
		NetworkOptions: networkOptions,
		Networks:       networks,
		PortMappings:   ports,
		DNSServers: slices.DeleteFunc(slices.Clone(o.dns), func(server string) bool {
			return server == "none"
		}),
		DNSSearch:          o.dnsSearch,
		DNSOptions:         o.dnsOptions,
		NoManageResolvConf: slices.Contains(o.dns, "none"),
		AddHosts:           o.addHosts,
		NoHosts:            o.noHosts,
		NoHostname:         o.noHostname,
		HostsFile:          o.hostsFile,
		CgroupParent:       o.cgroupParent,
		CPUs:               o.cpus,
		CPUShares:          o.cpuShares,
		CPUSetCPUs:         o.cpusetCPUs,
		CPUSetMems:         o.cpusetMems,
		Memory:             o.memory,
		MemorySwap:         o.memorySwap,
		BlkioWeight:        o.blkioWeight,
		BlkioWeightDevices: weightDevices,
		DeviceReadBps:      readBps,
		DeviceWriteBps:     writeBps,
		Devices:            o.devices,
		GPUs:               o.gpus,
		Volumes:            volumes,
		VolumesFrom:        o.volumesFrom,
		ShmSize:            o.shmSize,
		ShmSizeSystemd:     o.shmSizeSystemd,
		SecurityOpt:        o.securityOpt,
		Sysctl:             o.sysctl,
	}, nil
}

func (o podCreateOptions) allLabels() (map[string]string, error) {
	labels := map[string]string{}
	for _, path := range o.labelFiles {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read --label-file: %w", err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, _ := strings.Cut(line, "=")
			labels[key] = value
		}
		if readErr := cmp.Or(scanner.Err(), file.Close()); readErr != nil {
			return nil, fmt.Errorf("read --label-file %s: %w", path, readErr)
		}
	}
	maps.Copy(labels, o.labels)
	return labels, nil
}

func (o podCreateOptions) networkSettings() (string, []string, []types.PodNetwork, error) {
	var mode string
	var options []string
	var networks []types.PodNetwork
	for _, value := range o.networks {
		name, rest, _ := strings.Cut(value, ":")
		if slices.Contains(podNetworkModes, name) {
			mode = kit.Ternary(name == "ns", value, name)
			options = kit.Ternary(name == "ns" || rest == "", options, strings.Split(rest, ","))
			continue
		}
		network := types.PodNetwork{Name: name}
		for option := range strings.SplitSeq(rest, ",") {
			key, optionValue, _ := strings.Cut(option, "=")
			switch key {
			case "":
			case "alias":
				network.Aliases = append(network.Aliases, optionValue)
			case "ip", "ip6":
				network.StaticIPs = append(network.StaticIPs, optionValue)
			case "mac":
				network.StaticMAC = optionValue
			case "interface_name":
				network.InterfaceName = optionValue
			default:
				return "", nil, nil, fmt.Errorf("invalid --network option %q", option)
			}
		}
		networks = append(networks, network)
	}
	if len(o.networkAliases) == 0 && o.ip == "" && o.ip6 == "" && o.macAddress == "" {
		return mode, options, networks, nil
	}
	if len(networks) != 1 {
		return "", nil, nil, errors.New("--ip, --ip6, --mac-address, and --network-alias need exactly one --network NAME")
	}
	networks[0] = types.PodNetwork{
		Name:    networks[0].Name,
		Aliases: append(networks[0].Aliases, o.networkAliases...),
		StaticIPs: slices.DeleteFunc(append(networks[0].StaticIPs, o.ip, o.ip6), func(ip string) bool {
			return ip == ""
		}),
		StaticMAC:     cmp.Or(o.macAddress, networks[0].StaticMAC),
		InterfaceName: networks[0].InterfaceName,
	}
	return mode, options, networks, nil
}
