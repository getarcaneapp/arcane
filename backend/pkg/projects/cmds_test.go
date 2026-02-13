package projects

import (
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestComposeUpOptions_DefaultRecreateMode(t *testing.T) {
	proj := &types.Project{Name: "demo"}

	createOpts, _ := composeUpOptions(proj, nil, ComposeUpOptions{RemoveOrphans: true})

	assert.Equal(t, api.RecreateDiverged, createOpts.Recreate)
	assert.Equal(t, api.RecreateDiverged, createOpts.RecreateDependencies)
	assert.True(t, createOpts.RemoveOrphans)
}

func TestComposeUpOptions_ForceRecreateMode(t *testing.T) {
	proj := &types.Project{Name: "demo"}

	createOpts, _ := composeUpOptions(proj, nil, ComposeUpOptions{ForceRecreate: true})

	assert.Equal(t, api.RecreateForce, createOpts.Recreate)
	assert.Equal(t, api.RecreateForce, createOpts.RecreateDependencies)
}
