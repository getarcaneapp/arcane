package authz

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
)

func TestResourceScopeMatches(t *testing.T) {
	web := ResourceAttributes{
		Names:   []string{"web-1"},
		Labels:  map[string]string{"team": "web", "tier": "front"},
		Project: "shop",
		OwnerID: "user-1",
	}
	tests := []struct {
		name      string
		selectors []types.ResourceSelector
		attrs     ResourceAttributes
		want      bool
	}{
		{name: "no selectors", attrs: web},
		{name: "empty selector", selectors: []types.ResourceSelector{{}}, attrs: web},
		{name: "label match", selectors: []types.ResourceSelector{{Labels: map[string]string{"team": "web"}}}, attrs: web, want: true},
		{name: "label value mismatch", selectors: []types.ResourceSelector{{Labels: map[string]string{"team": "db"}}}, attrs: web},
		{name: "labels are ANDed", selectors: []types.ResourceSelector{{Labels: map[string]string{"team": "web", "tier": "back"}}}, attrs: web},
		{name: "missing labels", selectors: []types.ResourceSelector{{Labels: map[string]string{"team": ""}}}, attrs: ResourceAttributes{Names: []string{"web-1"}}},
		{name: "project match", selectors: []types.ResourceSelector{{Projects: []string{"blog", "shop"}}}, attrs: web, want: true},
		{name: "project mismatch", selectors: []types.ResourceSelector{{Projects: []string{"blog"}}}, attrs: web},
		{name: "missing project", selectors: []types.ResourceSelector{{Projects: []string{"shop"}}}, attrs: ResourceAttributes{Names: []string{"web-1"}}},
		{name: "owner match", selectors: []types.ResourceSelector{{OwnerOnly: true}}, attrs: web, want: true},
		{name: "owner mismatch", selectors: []types.ResourceSelector{{OwnerOnly: true}}, attrs: ResourceAttributes{OwnerID: "user-2"}},
		{name: "missing owner", selectors: []types.ResourceSelector{{OwnerOnly: true}}, attrs: ResourceAttributes{}},
		{name: "name glob match", selectors: []types.ResourceSelector{{Names: []string{"db-*", "web-*"}}}, attrs: web, want: true},
		{name: "name glob mismatch", selectors: []types.ResourceSelector{{Names: []string{"db-*"}}}, attrs: web},
		{name: "bad glob never matches", selectors: []types.ResourceSelector{{Names: []string{"web-["}}}, attrs: web},
		{name: "fields are ANDed", selectors: []types.ResourceSelector{{Projects: []string{"shop"}, Names: []string{"db-*"}}}, attrs: web},
		{name: "selectors are ORed", selectors: []types.ResourceSelector{{Names: []string{"db-*"}}, {Projects: []string{"shop"}}}, attrs: web, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := ResourceScope{
				subjectID: "user-1",
				selectors: tt.selectors,
			}
			require.Equal(t, tt.want, scope.Matches(tt.attrs))
		})
	}

	require.True(t, UnrestrictedScope().Matches(ResourceAttributes{}))
	require.True(t, UnrestrictedScope().IsUnrestricted())

	anonymous := ResourceScope{selectors: []types.ResourceSelector{{OwnerOnly: true}}}
	require.False(t, anonymous.Matches(ResourceAttributes{}))
}

func TestResourceScopeCovers(t *testing.T) {
	scope := ResourceScope{selectors: []types.ResourceSelector{{Labels: map[string]string{"team": "web"}}}}

	require.False(t, scope.Covers(nil))
	require.True(t, scope.Covers(&types.ResourceSelector{Labels: map[string]string{"team": "web", "tier": "front"}}))
	require.False(t, scope.Covers(&types.ResourceSelector{Labels: map[string]string{"team": "db"}}))
	require.True(t, UnrestrictedScope().Covers(nil))
	require.True(t, UnrestrictedScope().Covers(&types.ResourceSelector{OwnerOnly: true}))
}

func TestResourceScopeCoversScope(t *testing.T) {
	scope := ResourceScope{selectors: []types.ResourceSelector{{Labels: map[string]string{"team": "web"}}}}

	require.True(t, UnrestrictedScope().CoversScope(UnrestrictedScope()))
	require.True(t, UnrestrictedScope().CoversScope(scope))
	require.False(t, scope.CoversScope(UnrestrictedScope()))
	require.True(t, scope.CoversScope(ResourceScope{selectors: []types.ResourceSelector{{Labels: map[string]string{"team": "web", "tier": "front"}}}}))
	require.False(t, scope.CoversScope(ResourceScope{selectors: []types.ResourceSelector{
		{Labels: map[string]string{"team": "web"}},
		{Labels: map[string]string{"team": "db"}},
	}}))
}

func TestResourceAttributes(t *testing.T) {
	labels := map[string]string{
		"com.docker.compose.project": "shop",
		libarcane.LabelOwner:         "user-1",
	}

	container := ContainerAttributes([]string{"/web-1"}, labels)
	require.Equal(t, []string{"web-1"}, container.Names)
	require.Equal(t, "shop", container.Project)
	require.Equal(t, "user-1", container.OwnerID)

	volume := VolumeAttributes("data", labels)
	require.Equal(t, []string{"data"}, volume.Names)
	require.Equal(t, "shop", volume.Project)
	require.Equal(t, "user-1", volume.OwnerID)

	network := NetworkAttributes("net", nil)
	require.Equal(t, []string{"net"}, network.Names)
	require.Empty(t, network.Project)
	require.Empty(t, network.OwnerID)

	image := ImageAttributes([]string{"nginx:latest", "<none>:<none>"}, labels)
	require.Equal(t, []string{"nginx:latest"}, image.Names)
	require.Empty(t, image.Project)
	require.Empty(t, image.OwnerID)

	require.Equal(t, ResourceAttributes{
		Names:   []string{"shop"},
		Project: "shop",
		OwnerID: "user-1",
	}, ProjectAttributes("shop", new("user-1")))
	require.Empty(t, ProjectAttributes("shop", nil).OwnerID)
}

func TestValidateSelector(t *testing.T) {
	tests := []struct {
		name    string
		sel     types.ResourceSelector
		wantErr bool
	}{
		{name: "empty", wantErr: true},
		{name: "labels", sel: types.ResourceSelector{Labels: map[string]string{"team": "web"}}},
		{name: "empty label key", sel: types.ResourceSelector{Labels: map[string]string{" ": "web"}}, wantErr: true},
		{name: "projects", sel: types.ResourceSelector{Projects: []string{"shop"}}},
		{name: "empty project", sel: types.ResourceSelector{Projects: []string{""}}, wantErr: true},
		{name: "owner", sel: types.ResourceSelector{OwnerOnly: true}},
		{name: "names", sel: types.ResourceSelector{Names: []string{"web-*"}}},
		{name: "empty name", sel: types.ResourceSelector{Names: []string{""}}, wantErr: true},
		{name: "bad glob", sel: types.ResourceSelector{Names: []string{"web-["}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSelector(tt.sel)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidSelector)
			require.ErrorIs(t, err, common.ErrValidation)
		})
	}
}
