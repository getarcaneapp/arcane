package pods

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

func TestCreateMapsRequestToPodSpec(t *testing.T) {
	tests := []struct {
		name string
		req  types.PodCreateRequest
		want map[string]any
	}{
		{
			name: "named network with ports",
			req: types.PodCreateRequest{
				Name:             " web ",
				Hostname:         "web-host",
				Labels:           map[string]string{"tier": "front"},
				Networks:         []types.PodNetwork{{Name: "frontend"}},
				SharedNamespaces: []string{"net", "ipc"},
				PortMappings: []types.PodPortMapping{
					{
						ContainerPort: 80,
						HostPort:      8080,
						Protocol:      "tcp",
					},
				},
			},
			want: map[string]any{
				"name":              "web",
				"hostname":          "web-host",
				"labels":            map[string]any{"tier": "front"},
				"shared_namespaces": []any{"net", "ipc"},
				"portmappings": []any{
					map[string]any{
						"container_port": float64(80),
						"host_port":      float64(8080),
						"protocol":       "tcp",
					},
				},
				"netns":           map[string]any{"nsmode": "bridge"},
				"Networks":        map[string]any{"frontend": map[string]any{}},
				"resource_limits": map[string]any{},
			},
		},
		{
			name: "host network without infra",
			req: types.PodCreateRequest{
				Name:        "host-pod",
				NetworkMode: "host",
				NoInfra:     true,
			},
			want: map[string]any{
				"name":            "host-pod",
				"no_infra":        true,
				"netns":           map[string]any{"nsmode": "host"},
				"resource_limits": map[string]any{},
			},
		},
		{
			name: "every option",
			req: types.PodCreateRequest{
				Name:               "full",
				ExitPolicy:         "stop",
				RestartPolicy:      "on-failure",
				RestartRetries:     new(uint(3)),
				InfraImage:         "registry.k8s.io/pause:3.9",
				InfraCommand:       []string{"/catatonit", "-P"},
				InfraName:          "full-infra",
				InfraConmonPidFile: "/run/full.pid",
				ShareParent:        new(false),
				Pid:                "host",
				Ipc:                "ns:/proc/1/ns/ipc",
				Uts:                "private",
				Userns:             "keep-id:uid=1000",
				UIDMap:             []string{"0:100000:65536"},
				NetworkMode:        "pasta",
				NetworkOptions:     []string{"--ipv4-only"},
				Networks: []types.PodNetwork{{
					Name:          "backend",
					Aliases:       []string{"api"},
					StaticIPs:     []string{"10.89.0.10"},
					StaticMAC:     "92:d0:c6:0a:29:33",
					InterfaceName: "eth1",
				}},
				DNSServers:         []string{"1.1.1.1"},
				DNSSearch:          []string{"example.com"},
				DNSOptions:         []string{"ndots:2"},
				NoManageResolvConf: true,
				AddHosts:           []string{"db:10.0.0.5"},
				NoHosts:            true,
				NoHostname:         true,
				HostsFile:          "image",
				CgroupParent:       "machine.slice",
				CPUs:               1.5,
				CPUShares:          512,
				CPUSetCPUs:         "0-3",
				CPUSetMems:         "0",
				Memory:             "512m",
				MemorySwap:         "-1",
				BlkioWeight:        300,
				BlkioWeightDevices: []types.PodDeviceLimit{{Device: "8:0", Value: "200"}},
				DeviceReadBps:      []types.PodDeviceLimit{{Device: "8:0", Value: "1mb"}},
				DeviceWriteBps:     []types.PodDeviceLimit{{Device: "8:16", Value: "2mb"}},
				Devices:            []string{"/dev/fuse"},
				GPUs:               []string{"all"},
				Volumes: []types.PodVolume{
					{
						Source:      "/srv/data",
						Destination: "/data",
						Options:     []string{"ro", "Z"},
					},
					{Source: "cache", Destination: "/cache"},
				},
				VolumesFrom:    []string{"seed:ro"},
				ShmSize:        "64m",
				ShmSizeSystemd: "32m",
				SecurityOpt:    []string{"label=disable"},
				Sysctl:         map[string]string{"net.ipv4.ip_unprivileged_port_start": "80"},
			},
			want: map[string]any{
				"name":                  "full",
				"exit_policy":           "stop",
				"restart_policy":        "on-failure",
				"restart_tries":         float64(3),
				"infra_image":           "registry.k8s.io/pause:3.9",
				"infra_command":         []any{"/catatonit", "-P"},
				"infra_name":            "full-infra",
				"infra_conmon_pid_file": "/run/full.pid",
				"share_parent":          false,
				"pidns":                 map[string]any{"nsmode": "host"},
				"ipcns":                 map[string]any{"nsmode": "path", "value": "/proc/1/ns/ipc"},
				"utsns":                 map[string]any{"nsmode": "private"},
				"userns":                map[string]any{"nsmode": "keep-id", "value": "uid=1000"},
				"idmappings": map[string]any{
					"HostUIDMapping": false,
					"HostGIDMapping": false,
					"UIDMap": []any{
						map[string]any{
							"container_id": float64(0),
							"host_id":      float64(100000),
							"size":         float64(65536),
						},
					},
					"GIDMap": []any{
						map[string]any{
							"container_id": float64(0),
							"host_id":      float64(100000),
							"size":         float64(65536),
						},
					},
				},
				"netns":           map[string]any{"nsmode": "pasta"},
				"network_options": map[string]any{"pasta": []any{"--ipv4-only"}},
				"Networks": map[string]any{"backend": map[string]any{
					"aliases":        []any{"api"},
					"static_ips":     []any{"10.89.0.10"},
					"static_mac":     "92:d0:c6:0a:29:33",
					"interface_name": "eth1",
				}},
				"dns_server":            []any{"1.1.1.1"},
				"dns_search":            []any{"example.com"},
				"dns_option":            []any{"ndots:2"},
				"no_manage_resolv_conf": true,
				"hostadd":               []any{"db:10.0.0.5"},
				"no_manage_hosts":       true,
				"no_manage_hostname":    true,
				"hostsFile":             "image",
				"cgroup_parent":         "machine.slice",
				"resource_limits": map[string]any{
					"memory": map[string]any{"limit": float64(512 << 20), "swap": float64(-1)},
					"cpu": map[string]any{
						"shares": float64(512),
						"quota":  float64(150000),
						"period": float64(100000),
						"cpus":   "0-3",
						"mems":   "0",
					},
					"blockIO": map[string]any{
						"weight": float64(300),
						"weightDevice": []any{
							map[string]any{
								"major":  float64(8),
								"minor":  float64(0),
								"weight": float64(200),
							},
						},
						"throttleReadBpsDevice": []any{
							map[string]any{
								"major": float64(8),
								"minor": float64(0),
								"rate":  float64(1 << 20),
							},
						},
						"throttleWriteBpsDevice": []any{
							map[string]any{
								"major": float64(8),
								"minor": float64(16),
								"rate":  float64(2 << 20),
							},
						},
					},
				},
				"pod_devices": []any{"/dev/fuse", "nvidia.com/gpu=all"},
				"mounts": []any{
					map[string]any{
						"destination": "/data",
						"type":        "bind",
						"source":      "/srv/data",
						"options":     []any{"ro", "Z"},
					},
				},
				"volumes":          []any{map[string]any{"Name": "cache", "Dest": "/cache"}},
				"volumes_from":     []any{"seed:ro"},
				"shm_size":         float64(64 << 20),
				"shm_size_systemd": float64(32 << 20),
				"security_opt":     []any{"label=disable"},
				"sysctl":           map[string]any{"net.ipv4.ip_unprivileged_port_start": "80"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v4.0.0/libpod/pods/create", r.URL.Path)
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Id":"pod-1"}`))
			}))
			t.Cleanup(server.Close)
			client, err := podman.NewClient(server.URL)
			require.NoError(t, err)

			svc := NewService(func(context.Context) (podman.Runtime, error) {
				return podman.Runtime{Client: client}, nil
			})
			id, err := svc.Create(t.Context(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, "pod-1", id)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCreateRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		req  types.PodCreateRequest
	}{
		{name: "memory", req: types.PodCreateRequest{Name: "bad", Memory: "lots"}},
		{name: "device", req: types.PodCreateRequest{Name: "bad", DeviceReadBps: []types.PodDeviceLimit{{Device: "/dev/sda", Value: "1mb"}}}},
		{name: "weight", req: types.PodCreateRequest{Name: "bad", BlkioWeightDevices: []types.PodDeviceLimit{{Device: "8:0", Value: "heavy"}}}},
		{name: "id map", req: types.PodCreateRequest{Name: "bad", GIDMap: []string{"0:100000"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(func(context.Context) (podman.Runtime, error) {
				t.Error("runtime should not be requested")
				return podman.Runtime{}, nil
			})
			_, err := svc.Create(t.Context(), tt.req)
			require.ErrorIs(t, err, common.ErrBadRequest)
		})
	}
}

func TestInspectMergesContainerDetails(t *testing.T) {
	tests := []struct {
		name       string
		listStatus int
		want       []types.PodInspectContainer
		wantErr    bool
	}{
		{
			name:       "merges list details by id",
			listStatus: http.StatusOK,
			want: []types.PodInspectContainer{
				{
					ID:      "infra1",
					Name:    "web-infra",
					State:   "running",
					Image:   "localhost/podman-pause:5.4.0",
					Status:  "Up 2 minutes",
					Created: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
					IsInfra: true,
					Ports: []types.PodPortMapping{
						{
							ContainerPort: 80,
							HostPort:      8080,
							Protocol:      "tcp",
							Range:         1,
						},
					},
				},
				{
					ID:       "app1",
					Name:     "web-app",
					State:    "exited",
					Image:    "docker.io/library/nginx:latest",
					Status:   "Exited (1) 1 minute ago",
					Created:  time.Date(2026, 10, 1, 12, 1, 0, 0, time.UTC),
					ExitCode: 1,
					Ports:    []types.PodPortMapping{},
				},
				{
					ID:    "gone1",
					Name:  "web-gone",
					State: "removing",
				},
			},
		},
		{
			name:       "container list failure",
			listStatus: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v4.0.0/libpod/pods/web/json":
					_, _ = w.Write([]byte(`{"Id":"pod123","Name":"web","Containers":[
						{"Id":"infra1","Name":"web-infra","State":"running"},
						{"Id":"app1","Name":"web-app","State":"exited"},
						{"Id":"gone1","Name":"web-gone","State":"removing"}
					]}`))
				case "/v4.0.0/libpod/containers/json":
					assert.JSONEq(t, `{"pod":["pod123"]}`, r.URL.Query().Get("filters"))
					w.WriteHeader(tt.listStatus)
					_, _ = w.Write([]byte(`[
						{"Id":"app1","Image":"docker.io/library/nginx:latest","Status":"Exited (1) 1 minute ago","Created":"2026-10-01T12:01:00Z","ExitCode":1},
						{"Id":"infra1","Image":"localhost/podman-pause:5.4.0","Status":"Up 2 minutes","Created":"2026-10-01T12:00:00Z","IsInfra":true,
							"Ports":[{"container_port":80,"host_port":8080,"range":1,"protocol":"tcp"}]}
					]`))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			client, err := podman.NewClient(server.URL)
			require.NoError(t, err)

			svc := NewService(func(context.Context) (podman.Runtime, error) {
				return podman.Runtime{Client: client}, nil
			})
			pod, err := svc.Inspect(t.Context(), "web")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, pod.Containers)
		})
	}
}
