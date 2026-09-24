package projects

import (
	"context"
	"io"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceDependents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  composetypes.Services
		disabled composetypes.Services
		profiles []string
		services []string
		want     []string
	}{
		{
			name:     "transitive network_mode chain",
			enabled:  composetypes.Services{"a": {Name: "a"}},
			disabled: composetypes.Services{"b": {Name: "b", NetworkMode: "service:a"}, "c": {Name: "c", NetworkMode: "service:b"}, "d": {Name: "d"}},
			services: []string{"a"},
			want:     []string{"b", "c"},
		},
		{
			name:     "ipc and pid refs",
			enabled:  composetypes.Services{"a": {Name: "a"}},
			disabled: composetypes.Services{"z": {Name: "z", Ipc: "service:a"}, "y": {Name: "y", Pid: "service:a"}, "x": {Name: "x", Ipc: "shareable"}},
			services: []string{"a"},
			want:     []string{"y", "z"},
		},
		{
			name:     "volumes_from service matched and container skipped",
			enabled:  composetypes.Services{"a": {Name: "a"}},
			disabled: composetypes.Services{"b": {Name: "b", VolumesFrom: []string{"a:ro"}}, "c": {Name: "c", VolumesFrom: []string{"container:a"}}},
			services: []string{"a"},
			want:     []string{"b"},
		},
		{
			name:     "profile-gated dependent excluded without profile",
			enabled:  composetypes.Services{"a": {Name: "a"}},
			disabled: composetypes.Services{"b": {Name: "b", NetworkMode: "service:a", Profiles: []string{"extra"}}},
			services: []string{"a"},
		},
		{
			name:     "profile-gated dependent included with profile",
			enabled:  composetypes.Services{"a": {Name: "a"}},
			disabled: composetypes.Services{"b": {Name: "b", NetworkMode: "service:a", Profiles: []string{"extra"}}},
			profiles: []string{"extra"},
			services: []string{"a"},
			want:     []string{"b"},
		},
		{
			name:     "inputs are excluded",
			enabled:  composetypes.Services{"a": {Name: "a"}, "b": {Name: "b", NetworkMode: "service:a"}},
			disabled: composetypes.Services{"c": {Name: "c", NetworkMode: "service:b"}},
			services: []string{"a", "b"},
			want:     []string{"c"},
		},
		{
			name:     "nil when nothing matches",
			enabled:  composetypes.Services{"a": {Name: "a"}},
			disabled: composetypes.Services{"b": {Name: "b", NetworkMode: "host"}},
			services: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := &composetypes.Project{Services: tt.enabled, DisabledServices: tt.disabled, Profiles: tt.profiles}
			assert.Equal(t, tt.want, NamespaceDependents(model, tt.services))
		})
	}
}

func TestCoordinatorUpdateServicesScopesDependents(t *testing.T) {
	t.Parallel()

	var stopped, upped, created, pulled, calls []string
	var selectedNames []string
	forceRecreate := false
	coordinator := NewCoordinator(projecttypes.ComposeCommands{
		Stop: func(_ context.Context, _ *composetypes.Project, services []string) error {
			stopped = services
			calls = append(calls, "stop")
			return nil
		},
		Up: func(_ context.Context, selected *composetypes.Project, services []string, _ bool, force bool, _ bool, _ map[string]registry.AuthConfig, _ time.Duration) error {
			upped = services
			selectedNames = selected.ServiceNames()
			forceRecreate = force
			calls = append(calls, "up")
			return nil
		},
		Create: func(_ context.Context, selected *composetypes.Project, services []string, _ map[string]registry.AuthConfig) error {
			created = services
			assert.Equal(t, selectedNames, selected.ServiceNames(), "create reuses the scoped model")
			calls = append(calls, "create")
			return nil
		},
	})

	model := &composetypes.Project{
		Name: "stack",
		Services: composetypes.Services{
			"app":     {Name: "app", Image: "example.com/app:2"},
			"sidecar": {Name: "sidecar", Image: "example.com/sidecar:1", NetworkMode: "service:app"},
			"dormant": {Name: "dormant", Image: "example.com/dormant:1", Pid: "service:app"},
		},
	}
	err := coordinator.UpdateServices(context.Background(), projecttypes.ComposeServiceUpdate{
		Project:           model,
		Services:          []string{"app"},
		Dependents:        []string{"sidecar"},
		StoppedDependents: []string{"dormant"},
		Images: projecttypes.ComposeImageOperations{Pull: func(_ context.Context, ref string, _ io.Writer) error {
			pulled = append(pulled, ref)
			return nil
		}},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"example.com/app:2"}, pulled)
	assert.Equal(t, []string{"app", "sidecar"}, stopped)
	assert.Equal(t, []string{"app", "sidecar"}, upped, "stopped dependents are not started")
	assert.Equal(t, []string{"app", "dormant", "sidecar"}, selectedNames)
	assert.True(t, forceRecreate)
	assert.Equal(t, []string{"dormant"}, created)
	assert.Equal(t, []string{"stop", "up", "create"}, calls, "stopped dependents are recreated after the providers")
}
