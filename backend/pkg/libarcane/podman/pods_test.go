package podman

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientPodCreate(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v4.0.0/libpod/pods/create", r.URL.Path)
		data, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.NoError(t, json.Unmarshal(data, &body))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"pod123"}`))
	})

	id, err := client.PodCreate(t.Context(), PodSpec{
		Name:     "web",
		Hostname: "web-host",
		PortMappings: []PodPortMapping{
			{ContainerPort: 80, HostPort: 8080},
		},
		Networks:  map[string]PodNetworkOptions{"frontend": {}},
		InfraName: "web-infra",
	})
	require.NoError(t, err)
	assert.Equal(t, "pod123", id)
	assert.Equal(t, "web", body["name"])
	assert.Equal(t, "web-host", body["hostname"])
	assert.Equal(t, "web-infra", body["infra_name"])
	assert.Contains(t, body, "Networks")
	assert.Equal(t, []any{map[string]any{"container_port": float64(80), "host_port": float64(8080)}}, body["portmappings"])
}

func TestClientPodAction(t *testing.T) {
	tests := []struct {
		name   string
		action PodAction
		opts   PodActionOptions
		query  string
	}{
		{name: "start", action: PodActionStart},
		{name: "stop with timeout", action: PodActionStop, opts: PodActionOptions{Timeout: new(5)}, query: "t=5"},
		{name: "kill with signal", action: PodActionKill, opts: PodActionOptions{Signal: "SIGTERM"}, query: "signal=SIGTERM"},
		{name: "restart ignores signal", action: PodActionRestart, opts: PodActionOptions{Signal: "SIGTERM"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v4.0.0/libpod/pods/web/"+string(tt.action), r.URL.Path)
				assert.Equal(t, tt.query, r.URL.RawQuery)
				_, _ = w.Write([]byte(`{"Id":"pod123","Errs":[]}`))
			})

			require.NoError(t, client.PodAction(t.Context(), "web", tt.action, tt.opts))
		})
	}
}

func TestClientPodStats(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		query string
	}{
		{name: "all pods", query: "all=true&stream=false"},
		{name: "named pods", names: []string{"web", "db"}, query: "namesOrIDs=web&namesOrIDs=db&stream=false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v4.0.0/libpod/pods/stats", r.URL.Path)
				assert.Equal(t, tt.query, r.URL.RawQuery)
				_, _ = w.Write([]byte(`[{"Pod":"pod123","CID":"abc","Name":"web-infra","CPU":"0.10%","MemUsage":"1MB / 2GB","NetIO":"1kB / 2kB","PIDS":"1"}]`))
			})

			stats, err := client.PodStats(t.Context(), tt.names)
			require.NoError(t, err)
			require.Len(t, stats, 1)
			assert.Equal(t, "pod123", stats[0].Pod)
			assert.Equal(t, "1kB / 2kB", stats[0].NetIO)
		})
	}
}

func TestClientPodListAndInspect(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.0.0/libpod/pods/json":
			assert.JSONEq(t, `{"label":["app=web"]}`, r.URL.Query().Get("filters"))
			_, _ = w.Write([]byte(`[{"Id":"pod123","Name":"web","Status":"Running","InfraId":"infra1","Containers":[{"Id":"abc","Names":"web-app","Status":"running"}]}]`))
		case "/v4.0.0/libpod/pods/web/json":
			_, _ = w.Write([]byte(`{
				"Id":"pod123","Name":"web","State":"Running","InfraContainerID":"infra1","NumContainers":2,
				"SharedNamespaces":["net","ipc"],"Containers":[{"Id":"abc","Name":"web-app","State":"running"}],
				"CreateCommand":["podman","pod","create"],"ExitPolicy":"continue","RestartPolicy":"always",
				"CreateCgroup":true,"CgroupParent":"machine.slice","CgroupPath":"machine.slice/pod.slice",
				"cpu_period":100000,"cpu_quota":50000,"cpu_shares":512,"cpuset_cpus":"0-1","cpuset_mems":"0",
				"memory_limit":536870912,"memory_swap":1073741824,"blkio_weight":300,
				"blkio_weight_device":[{"Path":"/dev/sda","Weight":200}],
				"device_read_bps":[{"Path":"/dev/sda","Rate":1048576}],
				"device_write_bps":[{"Path":"/dev/sdb","Rate":2097152}],
				"mounts":[{"Type":"bind","Source":"/srv","Destination":"/data","Options":["rbind"],"RW":true,"Propagation":"rprivate","SubPath":"sub"}],
				"devices":[{"PathOnHost":"/dev/fuse","PathInContainer":"/dev/fuse","CgroupPermissions":"rwm"}],
				"volumes_from":["seed:ro"],"security_opt":["label=disable"],
				"InfraConfig":{
					"PortBindings":{"80/tcp":[{"HostIp":"","HostPort":"8080"}]},"HostNetwork":false,
					"StaticIP":"10.88.0.5","StaticMAC":"aa:bb:cc:dd:ee:ff","NoManageResolvConf":true,
					"DNSServer":["1.1.1.1"],"DNSSearch":["lan"],"DNSOption":["ndots:2"],"NoManageHostname":true,
					"NoManageHosts":true,"HostAdd":["db:10.0.0.2"],"HostsFile":"/etc/hosts.base","Networks":["frontend"],
					"NetworkOptions":{"pasta":["-T","8080"]},"pid_ns":"host","userns":"keep-id","uts_ns":"private"
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	})

	pods, err := client.PodList(t.Context(), map[string][]string{"label": {"app=web"}})
	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "infra1", pods[0].InfraID)
	require.Len(t, pods[0].Containers, 1)
	assert.Equal(t, "web-app", pods[0].Containers[0].Names)

	pod, err := client.PodInspect(t.Context(), "web")
	require.NoError(t, err)
	assert.Equal(t, "infra1", pod.InfraContainerID)
	assert.Equal(t, uint(2), pod.NumContainers)
	assert.Equal(t, []string{"net", "ipc"}, pod.SharedNamespaces)
	assert.Equal(t, []string{"podman", "pod", "create"}, pod.CreateCommand)
	assert.Equal(t, "continue", pod.ExitPolicy)
	assert.Equal(t, "always", pod.RestartPolicy)
	assert.True(t, pod.CreateCgroup)
	assert.Equal(t, "machine.slice", pod.CgroupParent)
	assert.Equal(t, "machine.slice/pod.slice", pod.CgroupPath)
	assert.Equal(t, uint64(100000), pod.CPUPeriod)
	assert.Equal(t, int64(50000), pod.CPUQuota)
	assert.Equal(t, uint64(512), pod.CPUShares)
	assert.Equal(t, "0-1", pod.CPUSetCPUs)
	assert.Equal(t, "0", pod.CPUSetMems)
	assert.Equal(t, uint64(536870912), pod.MemoryLimit)
	assert.Equal(t, uint64(1073741824), pod.MemorySwap)
	assert.Equal(t, uint64(300), pod.BlkioWeight)
	assert.Equal(t, []types.PodWeightDevice{{Path: "/dev/sda", Weight: 200}}, pod.BlkioWeightDevices)
	assert.Equal(t, []types.PodThrottleDevice{{Path: "/dev/sda", Rate: 1048576}}, pod.DeviceReadBps)
	assert.Equal(t, []types.PodThrottleDevice{{Path: "/dev/sdb", Rate: 2097152}}, pod.DeviceWriteBps)
	assert.Equal(t, []types.PodMount{
		{
			Type:        "bind",
			Source:      "/srv",
			Destination: "/data",
			Options:     []string{"rbind"},
			RW:          true,
			Propagation: "rprivate",
			SubPath:     "sub",
		},
	}, pod.Mounts)
	assert.Equal(t, []types.PodDevice{
		{
			PathOnHost:        "/dev/fuse",
			PathInContainer:   "/dev/fuse",
			CgroupPermissions: "rwm",
		},
	}, pod.Devices)
	assert.Equal(t, []string{"seed:ro"}, pod.VolumesFrom)
	assert.Equal(t, []string{"label=disable"}, pod.SecurityOpts)
	assert.Equal(t, &types.PodInfraConfig{
		PortBindings:       map[string][]types.PodPortBinding{"80/tcp": {{HostPort: "8080"}}},
		StaticIP:           "10.88.0.5",
		StaticMAC:          "aa:bb:cc:dd:ee:ff",
		NoManageResolvConf: true,
		DNSServer:          []string{"1.1.1.1"},
		DNSSearch:          []string{"lan"},
		DNSOption:          []string{"ndots:2"},
		NoManageHostname:   true,
		NoManageHosts:      true,
		HostAdd:            []string{"db:10.0.0.2"},
		HostsFile:          "/etc/hosts.base",
		Networks:           []string{"frontend"},
		NetworkOptions:     map[string][]string{"pasta": {"-T", "8080"}},
		PidNS:              "host",
		UserNS:             "keep-id",
		UtsNS:              "private",
	}, pod.InfraConfig)
}
