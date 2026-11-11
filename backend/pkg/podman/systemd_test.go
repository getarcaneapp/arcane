package podman

import (
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

func TestNewSystemd(t *testing.T) {
	uidBus := "/run/user/" + strconv.Itoa(os.Getuid()) + "/bus"
	tests := []struct {
		env      map[string]string
		want     *dbusSystemd
		name     string
		host     string
		buses    []string
		statted  []string
		rootless bool
	}{
		{
			name: "rootful default system bus",
			want: &dbusSystemd{
				address: defaultSystemBusAddress,
				scope:   types.PodmanSystemdScopeSystem,
			},
		},
		{
			name: "rootful custom system bus",
			env:  map[string]string{"DBUS_SYSTEM_BUS_ADDRESS": "unix:path=/host/bus"},
			want: &dbusSystemd{
				address: "unix:path=/host/bus",
				scope:   types.PodmanSystemdScopeSystem,
			},
		},
		{
			name:     "rootless session bus",
			rootless: true,
			env: map[string]string{
				"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus",
				"DBUS_SYSTEM_BUS_ADDRESS":  "unix:path=/host/bus",
			},
			want: &dbusSystemd{
				address: "unix:path=/run/user/1000/bus",
				scope:   types.PodmanSystemdScopeUser,
			},
		},
		{
			name: "local unix socket",
			host: "unix:///run/podman/podman.sock",
			want: &dbusSystemd{
				address: defaultSystemBusAddress,
				scope:   types.PodmanSystemdScopeSystem,
			},
		},
		{
			name: "remote engine without bus",
			host: "tcp://podman.example:2375",
			want: &dbusSystemd{
				scope:  types.PodmanSystemdScopeSystem,
				reason: "the Podman engine at tcp://podman.example:2375 is remote; set DBUS_SYSTEM_BUS_ADDRESS to that host's bus to manage its systemd units",
			},
		},
		{
			name:     "remote engine with explicit bus",
			host:     "ssh://core@podman.example",
			rootless: true,
			env:      map[string]string{"DBUS_SESSION_BUS_ADDRESS": "tcp:host=podman.example,port=55556"},
			want: &dbusSystemd{
				address: "tcp:host=podman.example,port=55556",
				scope:   types.PodmanSystemdScopeUser,
			},
		},
		{
			name:     "rootless prefers runtime dir bus",
			rootless: true,
			env:      map[string]string{"XDG_RUNTIME_DIR": "/run/xdg"},
			buses:    []string{"/run/xdg/bus", uidBus},
			statted:  []string{"/run/xdg/bus"},
			want: &dbusSystemd{
				address: "unix:path=/run/xdg/bus",
				scope:   types.PodmanSystemdScopeUser,
			},
		},
		{
			name:     "rootless falls back to uid bus",
			rootless: true,
			env:      map[string]string{"XDG_RUNTIME_DIR": "/run/xdg"},
			buses:    []string{uidBus},
			statted:  []string{"/run/xdg/bus", uidBus},
			want: &dbusSystemd{
				address: "unix:path=" + uidBus,
				scope:   types.PodmanSystemdScopeUser,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var statted []string
			assert.Equal(t, tt.want, NewSystemd(SystemdConfig{
				Getenv: func(key string) string {
					return tt.env[key]
				},
				Stat: func(name string) (os.FileInfo, error) {
					statted = append(statted, name)
					if !slices.Contains(tt.buses, name) {
						return nil, os.ErrNotExist
					}
					return nil, nil
				},
				Host:     tt.host,
				Rootless: tt.rootless,
			}))
			assert.Equal(t, tt.statted, statted)
		})
	}
}

func TestSystemdRootlessWithoutSessionBus(t *testing.T) {
	systemd := NewSystemd(SystemdConfig{
		Getenv: func(string) string {
			return ""
		},
		Stat: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		Rootless: true,
	})

	availability := systemd.Availability(t.Context())
	assert.False(t, availability.Available)
	assert.Equal(t, types.PodmanSystemdScopeUser, availability.Scope)
	assert.Contains(t, availability.Reason, "DBUS_SESSION_BUS_ADDRESS")

	require.ErrorIs(t, systemd.Start(t.Context(), "web.service"), common.ErrUnavailable)
	_, err := systemd.UnitStatus(t.Context(), "web.service")
	require.ErrorIs(t, err, common.ErrUnavailable)
}
