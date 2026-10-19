package podman

import (
	"testing"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

func TestNewSystemd(t *testing.T) {
	tests := []struct {
		name     string
		rootless bool
		env      map[string]string
		want     *dbusSystemd
	}{
		{
			name: "rootful default system bus",
			want: &dbusSystemd{
				address: defaultSystemBusAddress,
				scope:   podmantypes.SystemdScopeSystem,
			},
		},
		{
			name: "rootful custom system bus",
			env:  map[string]string{"DBUS_SYSTEM_BUS_ADDRESS": "unix:path=/host/bus"},
			want: &dbusSystemd{
				address: "unix:path=/host/bus",
				scope:   podmantypes.SystemdScopeSystem,
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
				scope:   podmantypes.SystemdScopeUser,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewSystemd(tt.rootless, func(key string) string { return tt.env[key] }))
		})
	}
}

func TestSystemdRootlessWithoutSessionBus(t *testing.T) {
	systemd := NewSystemd(true, func(string) string { return "" })

	availability := systemd.Availability(t.Context())
	assert.False(t, availability.Available)
	assert.Equal(t, podmantypes.SystemdScopeUser, availability.Scope)
	assert.Contains(t, availability.Reason, "DBUS_SESSION_BUS_ADDRESS")

	require.ErrorIs(t, systemd.Start(t.Context(), "web.service"), common.ErrUnavailable)
	_, err := systemd.UnitStatus(t.Context(), "web.service")
	require.ErrorIs(t, err, common.ErrUnavailable)
}
