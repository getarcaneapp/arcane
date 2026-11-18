package infisical

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestSuggestFolder(t *testing.T) {
	assert.Equal(t, "/my-app", SuggestFolder(" My App! "))
	assert.Equal(t, "/immich_server", SuggestFolder("immich_server"))
	assert.Equal(t, "/app", SuggestFolder("***"))
}

func TestNormalizeSetupTarget(t *testing.T) {
	target, err := NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: "new-project", Environment: " prod "}, "immich")
	require.NoError(t, err)
	assert.Equal(t, types.SecretSourceSetupTarget{Mode: "new-project", ProjectName: "immich", Environment: "prod", SecretPath: "/"}, target)

	target, err = NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: "shared-folder", ProjectID: "p1", Environment: "prod"}, "Ghost Blog")
	require.NoError(t, err)
	assert.Equal(t, "/ghost-blog", target.SecretPath)

	target, err = NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: "shared-folder", ProjectID: "p1", Environment: "prod", SecretPath: "apps/ghost/"}, "ghost")
	require.NoError(t, err)
	assert.Equal(t, "/apps/ghost", target.SecretPath)

	target, err = NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: "existing-project", ProjectID: "p1", Environment: "dev"}, "x")
	require.NoError(t, err)
	assert.Equal(t, "/", target.SecretPath)
	assert.Empty(t, target.ProjectName)

	for _, bad := range []types.SecretSourceSetupTarget{
		{Mode: "new-project"},
		{Mode: "existing-project", Environment: "prod"},
		{Mode: "somewhere", Environment: "prod"},
		{Mode: "new-project", Environment: "prod", ProjectName: string(make([]byte, 65))},
	} {
		_, err = NormalizeSetupTarget(bad, "app")
		assert.Error(t, err, "%+v", bad)
	}
}

func TestNormalizeSettingsKeepsSeparateSetupIdentity(t *testing.T) {
	settings, err := NormalizeSettings(&types.SecretSourceInfisicalSettings{ClientID: "deploy", SetupClientID: " setup "})
	require.NoError(t, err)
	assert.Equal(t, "setup", settings.SetupClientID)

	_, err = NormalizeSettings(&types.SecretSourceInfisicalSettings{ClientID: "same", SetupClientID: "same"})
	require.Error(t, err)
}

func TestNewSetupRequiresIdentity(t *testing.T) {
	_, err := NewSetup(nil, types.SecretSourceInfisicalSettings{ClientID: "deploy"}, "secret")
	require.ErrorIs(t, err, ErrNoSetupIdentity)
	_, err = NewSetup(nil, types.SecretSourceInfisicalSettings{ClientID: "deploy", SetupClientID: "setup"}, "")
	require.ErrorIs(t, err, ErrNoSetupIdentity)
}
