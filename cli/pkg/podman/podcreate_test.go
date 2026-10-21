package podman

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPodCreateOptionsRequest(t *testing.T) {
	tests := []struct {
		name    string
		opts    podCreateOptions
		want    types.PodCreateRequest
		wantErr string
	}{
		{
			name: "network modes, named networks, and limits",
			opts: podCreateOptions{
				name:           "web",
				infra:          true,
				share:          "+pid",
				restart:        "on-failure:3",
				networks:       []string{"pasta:--ipv4-only", "backend:alias=api,interface_name=eth1"},
				ip:             "10.89.0.10",
				macAddress:     "92:d0:c6:0a:29:33",
				publish:        []string{"8080:80/tcp"},
				dns:            []string{"none", "1.1.1.1"},
				deviceReadBps:  []string{"8:0:1mb"},
				volumes:        []string{"/srv/data:/data:ro,Z", "cache:/cache"},
				infraCommand:   "/catatonit -P",
				labels:         map[string]string{"tier": "front"},
				memory:         "512m",
				securityOpt:    []string{"label=disable"},
				volumesFrom:    []string{"seed"},
				blkioWeight:    300,
				cpus:           1.5,
				hostsFile:      "none",
				shmSizeSystemd: "32m",
			},
			want: types.PodCreateRequest{
				Name:             "web",
				Labels:           map[string]string{"tier": "front"},
				RestartPolicy:    "on-failure",
				RestartRetries:   new(uint(3)),
				InfraCommand:     []string{"/catatonit", "-P"},
				SharedNamespaces: []string{"ipc", "net", "uts", "pid"},
				NetworkMode:      "pasta",
				NetworkOptions:   []string{"--ipv4-only"},
				Networks: []types.PodNetwork{{
					Name:          "backend",
					Aliases:       []string{"api"},
					StaticIPs:     []string{"10.89.0.10"},
					StaticMAC:     "92:d0:c6:0a:29:33",
					InterfaceName: "eth1",
				}},
				PortMappings: []types.PodPortMapping{{
					ContainerPort: 80,
					HostPort:      8080,
					Protocol:      "tcp",
				}},
				DNSServers:         []string{"1.1.1.1"},
				NoManageResolvConf: true,
				HostsFile:          "none",
				CPUs:               1.5,
				Memory:             "512m",
				BlkioWeight:        300,
				BlkioWeightDevices: []types.PodDeviceLimit{},
				DeviceReadBps:      []types.PodDeviceLimit{{Device: "8:0", Value: "1mb"}},
				DeviceWriteBps:     []types.PodDeviceLimit{},
				Volumes: []types.PodVolume{
					{
						Source:      "/srv/data",
						Destination: "/data",
						Options:     []string{"ro", "Z"},
					},
					{Source: "cache", Destination: "/cache"},
				},
				VolumesFrom:    []string{"seed"},
				ShmSizeSystemd: "32m",
				SecurityOpt:    []string{"label=disable"},
			},
		},
		{
			name: "static IP without a named network",
			opts: podCreateOptions{
				name:  "web",
				infra: true,
				ip:    "10.0.0.2",
			},
			wantErr: "need exactly one --network NAME",
		},
		{
			name: "volume without destination",
			opts: podCreateOptions{
				name:    "web",
				infra:   true,
				volumes: []string{"data"},
			},
			wantErr: "invalid --volume",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.opts.request(&cobra.Command{})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
