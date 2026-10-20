package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
)

type PodAction string

const (
	PodActionStart   PodAction = "start"
	PodActionStop    PodAction = "stop"
	PodActionRestart PodAction = "restart"
	PodActionPause   PodAction = "pause"
	PodActionUnpause PodAction = "unpause"
	PodActionKill    PodAction = "kill"
)

type PodActionOptions struct {
	Timeout *int
	Signal  string
}

type PodPortMapping struct {
	ContainerPort uint16 `json:"container_port"`
	HostPort      uint16 `json:"host_port,omitempty"`
	HostIP        string `json:"host_ip,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Range         uint16 `json:"range,omitempty"`
}

type PodNamespace struct {
	NSMode string `json:"nsmode,omitempty"`
	Value  string `json:"value,omitempty"`
}

type PodNetworkOptions struct {
	StaticIPs     []string `json:"static_ips,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	StaticMAC     string   `json:"static_mac,omitempty"`
	InterfaceName string   `json:"interface_name,omitempty"`
}

type PodMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Source      string   `json:"source"`
	Options     []string `json:"options,omitempty"`
}

type PodNamedVolume struct {
	Name    string   `json:"Name"`
	Dest    string   `json:"Dest"`
	Options []string `json:"Options,omitempty"`
}

type PodIDMap struct {
	ContainerID int `json:"container_id"`
	HostID      int `json:"host_id"`
	Size        int `json:"size"`
}

type PodIDMappings struct {
	HostUIDMapping bool       `json:"HostUIDMapping"`
	HostGIDMapping bool       `json:"HostGIDMapping"`
	UIDMap         []PodIDMap `json:"UIDMap,omitempty"`
	GIDMap         []PodIDMap `json:"GIDMap,omitempty"`
}

type PodMemoryLimits struct {
	Limit *int64 `json:"limit,omitempty"`
	Swap  *int64 `json:"swap,omitempty"`
}

type PodCPULimits struct {
	Shares *uint64 `json:"shares,omitempty"`
	Quota  *int64  `json:"quota,omitempty"`
	Period *uint64 `json:"period,omitempty"`
	Cpus   string  `json:"cpus,omitempty"`
	Mems   string  `json:"mems,omitempty"`
}

type PodWeightDevice struct {
	Major  int64   `json:"major"`
	Minor  int64   `json:"minor"`
	Weight *uint16 `json:"weight,omitempty"`
}

type PodThrottleDevice struct {
	Major int64  `json:"major"`
	Minor int64  `json:"minor"`
	Rate  uint64 `json:"rate"`
}

type PodBlockIO struct {
	Weight                 *uint16             `json:"weight,omitempty"`
	WeightDevice           []PodWeightDevice   `json:"weightDevice,omitempty"`
	ThrottleReadBpsDevice  []PodThrottleDevice `json:"throttleReadBpsDevice,omitempty"`
	ThrottleWriteBpsDevice []PodThrottleDevice `json:"throttleWriteBpsDevice,omitempty"`
}

type PodResources struct {
	Memory  *PodMemoryLimits `json:"memory,omitempty"`
	CPU     *PodCPULimits    `json:"cpu,omitempty"`
	BlockIO *PodBlockIO      `json:"blockIO,omitempty"`
}

type PodSpec struct {
	Name               string                       `json:"name"`
	Hostname           string                       `json:"hostname,omitempty"`
	ExitPolicy         string                       `json:"exit_policy,omitempty"`
	Labels             map[string]string            `json:"labels,omitempty"`
	NoInfra            bool                         `json:"no_infra,omitempty"`
	InfraConmonPidFile string                       `json:"infra_conmon_pid_file,omitempty"`
	InfraCommand       []string                     `json:"infra_command,omitempty"`
	InfraImage         string                       `json:"infra_image,omitempty"`
	InfraName          string                       `json:"infra_name,omitempty"`
	Ipc                *PodNamespace                `json:"ipcns,omitempty"`
	SharedNamespaces   []string                     `json:"shared_namespaces,omitempty"`
	RestartPolicy      string                       `json:"restart_policy,omitempty"`
	RestartRetries     *uint                        `json:"restart_tries,omitempty"`
	ShareParent        *bool                        `json:"share_parent,omitempty"`
	Pid                *PodNamespace                `json:"pidns,omitempty"`
	Userns             *PodNamespace                `json:"userns,omitempty"`
	Uts                *PodNamespace                `json:"utsns,omitempty"`
	Devices            []string                     `json:"pod_devices,omitempty"`
	Sysctl             map[string]string            `json:"sysctl,omitempty"`
	NetNS              *PodNamespace                `json:"netns,omitempty"`
	PortMappings       []PodPortMapping             `json:"portmappings,omitempty"`
	Networks           map[string]PodNetworkOptions `json:"Networks,omitempty"`
	NoManageResolvConf bool                         `json:"no_manage_resolv_conf,omitempty"`
	DNSServer          []string                     `json:"dns_server,omitempty"`
	DNSSearch          []string                     `json:"dns_search,omitempty"`
	DNSOption          []string                     `json:"dns_option,omitempty"`
	NoManageHostname   bool                         `json:"no_manage_hostname,omitempty"`
	NoManageHosts      bool                         `json:"no_manage_hosts,omitempty"`
	HostAdd            []string                     `json:"hostadd,omitempty"`
	HostsFile          string                       `json:"hostsFile,omitempty"`
	NetworkOptions     map[string][]string          `json:"network_options,omitempty"`
	CgroupParent       string                       `json:"cgroup_parent,omitempty"`
	ResourceLimits     *PodResources                `json:"resource_limits,omitempty"`
	Mounts             []PodMount                   `json:"mounts,omitempty"`
	Volumes            []PodNamedVolume             `json:"volumes,omitempty"`
	VolumesFrom        []string                     `json:"volumes_from,omitempty"`
	ShmSize            *int64                       `json:"shm_size,omitempty"`
	ShmSizeSystemd     *int64                       `json:"shm_size_systemd,omitempty"`
	SecurityOpt        []string                     `json:"security_opt,omitempty"`
	IDMappings         *PodIDMappings               `json:"idmappings,omitempty"`
}

func (c *Client) PodList(ctx context.Context, filters map[string][]string) ([]podmantypes.Pod, error) {
	query, err := filtersQuery(url.Values{}, filters)
	if err != nil {
		return nil, err
	}

	return c.doJSON[[]podmantypes.Pod](ctx, request{
		method: http.MethodGet,
		path:   "/pods/json",
		query:  query,
	})
}

func (c *Client) PodInspect(ctx context.Context, nameOrID string) (podmantypes.PodInspect, error) {
	return c.doJSON[podmantypes.PodInspect](ctx, request{method: http.MethodGet, path: "/pods/" + url.PathEscape(nameOrID) + "/json"})
}

func (c *Client) PodCreate(ctx context.Context, spec PodSpec) (string, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode pod spec: %w", err)
	}

	out, err := c.doJSON[podmantypes.PodCreateResponse](ctx, request{
		method:      http.MethodPost,
		path:        "/pods/create",
		body:        bytes.NewReader(encoded),
		contentType: "application/json",
	})
	return out.ID, err
}

func (c *Client) PodAction(ctx context.Context, nameOrID string, action PodAction, opts PodActionOptions) error {
	query := url.Values{}
	if opts.Timeout != nil && action == PodActionStop {
		query.Set("t", strconv.Itoa(*opts.Timeout))
	}
	if opts.Signal != "" && action == PodActionKill {
		query.Set("signal", opts.Signal)
	}

	_, err := c.doText(ctx, request{
		method: http.MethodPost,
		path:   "/pods/" + url.PathEscape(nameOrID) + "/" + string(action),
		query:  query,
	})
	return err
}

func (c *Client) PodRemove(ctx context.Context, nameOrID string, force bool) error {
	_, err := c.doText(ctx, request{
		method: http.MethodDelete,
		path:   "/pods/" + url.PathEscape(nameOrID),
		query:  url.Values{"force": {strconv.FormatBool(force)}},
	})
	return err
}

func (c *Client) PodPrune(ctx context.Context) ([]podmantypes.PodPruneReport, error) {
	return c.doJSON[[]podmantypes.PodPruneReport](ctx, request{method: http.MethodPost, path: "/pods/prune"})
}

func (c *Client) PodTop(ctx context.Context, nameOrID, psArgs string) (podmantypes.PodTop, error) {
	query := url.Values{"stream": {"false"}}
	if psArgs != "" {
		query.Set("ps_args", psArgs)
	}

	return c.doJSON[podmantypes.PodTop](ctx, request{
		method: http.MethodGet,
		path:   "/pods/" + url.PathEscape(nameOrID) + "/top",
		query:  query,
	})
}

func (c *Client) PodStats(ctx context.Context, namesOrIDs []string) ([]podmantypes.PodStats, error) {
	query := url.Values{
		"stream":     {"false"},
		"namesOrIDs": namesOrIDs,
	}
	if len(namesOrIDs) == 0 {
		query.Set("all", "true")
	}

	return c.doJSON[[]podmantypes.PodStats](ctx, request{
		method: http.MethodGet,
		path:   "/pods/stats",
		query:  query,
	})
}
