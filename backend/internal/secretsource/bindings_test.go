package secretsource

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

func namedInfisicalSourceInternal(t *testing.T, service *SecretSourceService, name, secrets string) (*types.SecretSource, *fakeInfisicalInternal) {
	t.Helper()
	fake := &fakeInfisicalInternal{}
	fake.setSecrets(secrets)
	source, err := service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name:       name,
		Provider:   types.SecretSourceProviderInfisical,
		Settings:   infisicalSettingsInternal(fake.server(t).URL, "client-id"),
		Credential: "client-secret",
	})
	require.NoError(t, err)
	return source, fake
}

func infisicalBindingInternal(sourceID, path string, autoRedeploy bool) types.SecretSourceBindingUpsertRequest {
	return types.SecretSourceBindingUpsertRequest{
		SourceID: sourceID,
		Target: types.SecretSourceBindingTarget{Infisical: &types.SecretSourceInfisicalTarget{
			ProjectID: "p1", Environment: "prod", SecretPath: path, IncludeImports: true, ExpandReferences: true,
		}},
		Required:     true,
		Enabled:      true,
		AutoRedeploy: autoRedeploy,
	}
}

func TestBindingsMergeWithEarlierWinning(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	app, _ := namedInfisicalSourceInternal(t, service, "App", `{"secrets":[{"secretKey":"DB_PASS","secretValue":"from-app"},{"secretKey":"API_KEY","secretValue":"k"}]}`)
	shared, _ := namedInfisicalSourceInternal(t, service, "Shared", `{"secrets":[{"secretKey":"DB_PASS","secretValue":"from-shared"},{"secretKey":"SMTP","secretValue":"s"}]}`)
	project := ProjectRef{ID: "project-1", Name: "billing-api"}

	first, err := service.CreateBinding(t.Context(), project.ID, infisicalBindingInternal(app.ID, "/", false))
	require.NoError(t, err)
	second, err := service.CreateBinding(t.Context(), project.ID, infisicalBindingInternal(shared.ID, "/", false))
	require.NoError(t, err)
	assert.Equal(t, 0, first.Position)
	assert.Equal(t, 1, second.Position)

	env, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB_PASS": "from-app", "API_KEY": "k", "SMTP": "s"}, env.Values)
	assert.NotEmpty(t, env.Hash)

	check, err := service.CheckBindings(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	require.Len(t, check.Bindings, 2)
	assert.Equal(t, first.ID, check.Bindings[0].BindingID)
	assert.Empty(t, check.Bindings[0].ShadowedKeys)
	assert.Equal(t, []string{"DB_PASS"}, check.Bindings[1].ShadowedKeys)

	// Moving the shared source first flips the winner.
	moved := infisicalBindingInternal(shared.ID, "/", false)
	moved.Position = new(0)
	updated, err := service.UpdateBinding(t.Context(), project.ID, second.ID, moved)
	require.NoError(t, err)
	assert.Equal(t, 0, updated.Position)
	env, err = service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, "from-shared", env.Values["DB_PASS"])

	bindings, err := service.ListBindings(t.Context(), project.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	assert.Equal(t, []string{second.ID, first.ID}, []string{bindings[0].ID, bindings[1].ID})
	assert.Equal(t, []int{0, 1}, []int{bindings[0].Position, bindings[1].Position})
}

func TestBindingsRejectDuplicatesAndRenumberOnDelete(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	source, _ := namedInfisicalSourceInternal(t, service, "App", `{"secrets":[{"secretKey":"A","secretValue":"1"}]}`)
	projectID := "project-1"

	a, err := service.CreateBinding(t.Context(), projectID, infisicalBindingInternal(source.ID, "a", false))
	require.NoError(t, err)
	_, err = service.CreateBinding(t.Context(), projectID, infisicalBindingInternal(source.ID, "a", false))
	require.ErrorIs(t, err, common.ErrSecretBindingConflict, "the same target twice only shadows itself")
	b, err := service.CreateBinding(t.Context(), projectID, infisicalBindingInternal(source.ID, "b", false))
	require.NoError(t, err)
	c, err := service.CreateBinding(t.Context(), projectID, infisicalBindingInternal(source.ID, "c", false))
	require.NoError(t, err)

	// Changing b's target to a's is a duplicate too.
	_, err = service.UpdateBinding(t.Context(), projectID, b.ID, infisicalBindingInternal(source.ID, "a", false))
	require.ErrorIs(t, err, common.ErrSecretBindingConflict)

	require.NoError(t, service.DeleteBinding(t.Context(), projectID, a.ID))
	require.ErrorIs(t, service.DeleteBinding(t.Context(), projectID, a.ID), common.ErrSecretBindingNotFound)
	require.ErrorIs(t, service.DeleteBinding(t.Context(), "other-project", b.ID), common.ErrSecretBindingNotFound)

	bindings, err := service.ListBindings(t.Context(), projectID)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	assert.Equal(t, b.ID, bindings[0].ID)
	assert.Equal(t, 0, bindings[0].Position)
	assert.Equal(t, c.ID, bindings[1].ID)
	assert.Equal(t, 1, bindings[1].Position)

	// A position past the end is clamped.
	last := infisicalBindingInternal(source.ID, "b", false)
	last.Position = new(99)
	moved, err := service.UpdateBinding(t.Context(), projectID, b.ID, last)
	require.NoError(t, err)
	assert.Equal(t, 1, moved.Position)
}

func TestOptionalBindingFailureKeepsTheOthers(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	good, _ := namedInfisicalSourceInternal(t, service, "Good", `{"secrets":[{"secretKey":"A","secretValue":"1"}]}`)
	bad, badFake := namedInfisicalSourceInternal(t, service, "Bad", `{"secrets":[]}`)
	badFake.setFail(true)
	project := ProjectRef{ID: "project-1", Name: "billing-api"}

	_, err := service.CreateBinding(t.Context(), project.ID, infisicalBindingInternal(good.ID, "/", false))
	require.NoError(t, err)
	optional := infisicalBindingInternal(bad.ID, "/", false)
	optional.Required = false
	badBinding, err := service.CreateBinding(t.Context(), project.ID, optional)
	require.NoError(t, err)

	env, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "1"}, env.Values)

	check, err := service.CheckBindings(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	require.Len(t, check.Bindings, 2)
	assert.Empty(t, check.Bindings[0].Error)
	assert.Equal(t, badBinding.ID, check.Bindings[1].BindingID)
	assert.NotEmpty(t, check.Bindings[1].Error, "a failing binding reports its error without failing the check")

	required := infisicalBindingInternal(bad.ID, "/", false)
	_, err = service.UpdateBinding(t.Context(), project.ID, badBinding.ID, required)
	require.NoError(t, err)
	_, err = service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.ErrorIs(t, err, common.ErrSecretFetchFailed)
}

func TestRecordDeployedTracksEachBindingAndItsKeys(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	app, appFake := namedInfisicalSourceInternal(t, service, "App", `{"secrets":[{"secretKey":"DB_PASS","secretValue":"one"}]}`)
	shared, _ := namedInfisicalSourceInternal(t, service, "Shared", `{"secrets":[{"secretKey":"SMTP","secretValue":"s"},{"secretKey":"DB_PASS","secretValue":"x"}]}`)
	project := ProjectRef{ID: "project-1", Name: "billing-api"}
	resolve := func(_ context.Context, _ string) (ProjectRef, error) { return project, nil }

	_, err := service.CreateBinding(t.Context(), project.ID, infisicalBindingInternal(app.ID, "/", true))
	require.NoError(t, err)
	_, err = service.CreateBinding(t.Context(), project.ID, infisicalBindingInternal(shared.ID, "/", false))
	require.NoError(t, err)

	keys, err := service.DeployedKeys(t.Context(), project.ID)
	require.NoError(t, err)
	assert.Empty(t, keys, "nothing is deployed yet")

	env, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	service.RecordDeployed(t.Context(), project.ID, env)

	keys, err = service.DeployedKeys(t.Context(), project.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_PASS", "SMTP"}, keys)

	assert.Empty(t, service.CheckDrift(t.Context(), resolve))
	appFake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"two"}]}`)
	changes := service.CheckDrift(t.Context(), resolve)
	require.Len(t, changes, 1, "one change per project")
	assert.True(t, changes[0].AutoRedeploy, "the changed binding asks for a redeploy")

	bindings, err := service.ListBindings(t.Context(), project.ID)
	require.NoError(t, err)
	assert.True(t, bindings[0].RedeployNeeded)
	assert.False(t, bindings[1].RedeployNeeded)
}

func TestCheckBindingsWithoutBindings(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	_, err := service.CheckBindings(t.Context(), ProjectRef{ID: "project-1"}, types.Actor{})
	require.ErrorIs(t, err, common.ErrSecretBindingNotFound)
}
