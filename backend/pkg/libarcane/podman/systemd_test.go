package podman

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

func TestNewSystemd(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		rootless bool
		env      map[string]string
		want     *dbusSystemd
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewSystemd(tt.host, tt.rootless, func(key string) string {
				return tt.env[key]
			}))
		})
	}
}

func TestSystemdRootlessWithoutSessionBus(t *testing.T) {
	systemd := NewSystemd("", true, func(string) string {
		return ""
	})

	availability := systemd.Availability(t.Context())
	assert.False(t, availability.Available)
	assert.Equal(t, types.PodmanSystemdScopeUser, availability.Scope)
	assert.Contains(t, availability.Reason, "DBUS_SESSION_BUS_ADDRESS")

	require.ErrorIs(t, systemd.Start(t.Context(), "web.service"), common.ErrUnavailable)
	_, err := systemd.UnitStatus(t.Context(), "web.service")
	require.ErrorIs(t, err, common.ErrUnavailable)
}
