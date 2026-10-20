package secretsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

// fakeInfisicalInternal serves login and a mutable secret set.
type fakeInfisicalInternal struct {
	secrets string
	mu      sync.Mutex
	fail    bool
}

func (f *fakeInfisicalInternal) setSecrets(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets = body
}

func (f *fakeInfisicalInternal) setFail(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
}

func (f *fakeInfisicalInternal) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/universal-auth/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"tok","expiresIn":3600,"accessTokenMaxTTL":7200,"tokenType":"Bearer"}`))
	})
	mux.HandleFunc("GET /api/v4/secrets", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Permission denied"}`))
			return
		}
		_, _ = w.Write([]byte(f.secrets))
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"Apps","slug":"apps","environments":[{"id":"e1","name":"Production","slug":"prod"}]}]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func setupSecretSourceServiceTestInternal(t *testing.T) (*SecretSourceService, *database.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SecretSource{}, &ProjectSecretBinding{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	crypto.InitEncryption(crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})

	wrapped := &database.DB{DB: db}
	return NewSecretSourceService(wrapped, nil), wrapped
}

func infisicalSettingsInternal(siteURL, clientID string) types.SecretSourceSettings {
	return types.SecretSourceSettings{Infisical: &types.SecretSourceInfisicalSettings{SiteURL: siteURL, ClientID: clientID}}
}

func createInfisicalSourceInternal(t *testing.T, service *SecretSourceService, siteURL string) *types.SecretSource {
	t.Helper()
	source, err := service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name:       "Homelab Infisical",
		Provider:   types.SecretSourceProviderInfisical,
		Settings:   infisicalSettingsInternal(siteURL, "client-id"),
		Credential: "client-secret",
	})
	require.NoError(t, err)
	return source
}

func bindInfisicalInternal(t *testing.T, service *SecretSourceService, sourceID string, required, autoRedeploy bool) {
	t.Helper()
	_, err := upsertBindingForTestInternal(t.Context(), service, "project-1", types.SecretSourceBindingUpsertRequest{
		SourceID: sourceID,
		Target: types.SecretSourceBindingTarget{Infisical: &types.SecretSourceInfisicalTarget{
			ProjectID: "p1", Environment: "prod", SecretPath: "billing", IncludeImports: true, ExpandReferences: true,
		}},
		Required:     required,
		Enabled:      true,
		AutoRedeploy: autoRedeploy,
	})
	require.NoError(t, err)
}

// The helpers below keep the single-binding tests readable: each project in
// them has at most one binding.

func upsertBindingForTestInternal(
	ctx context.Context,
	service *SecretSourceService,
	projectID string,
	req types.SecretSourceBindingUpsertRequest,
) (*types.SecretSourceBinding, error) {
	existing, err := service.ListBindings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return service.UpdateBinding(ctx, projectID, existing[0].ID, req)
	}
	return service.CreateBinding(ctx, projectID, req)
}

func firstBindingForTestInternal(ctx context.Context, service *SecretSourceService, projectID string) (*types.SecretSourceBinding, error) {
	bindings, err := service.ListBindings(ctx, projectID)
	if err != nil || len(bindings) == 0 {
		return nil, err
	}
	return &bindings[0], nil
}

func checkFirstBindingForTestInternal(
	ctx context.Context,
	service *SecretSourceService,
	project ProjectRef,
	actor types.Actor,
) (types.SecretSourceCheckResult, error) {
	result, err := service.CheckBindings(ctx, project, actor)
	if err != nil {
		return types.SecretSourceCheckResult{}, err
	}
	first := result.Bindings[0]
	if first.Error != "" {
		return first, errors.New(first.Error)
	}
	return first, nil
}

func deleteBindingsForTestInternal(ctx context.Context, service *SecretSourceService, projectID string) error {
	bindings, err := service.ListBindings(ctx, projectID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if deleteErr := service.DeleteBinding(ctx, projectID, binding.ID); deleteErr != nil {
			return deleteErr
		}
	}
	return nil
}

func TestCreateSourceValidatesPerProvider(t *testing.T) {
	service, db := setupSecretSourceServiceTestInternal(t)

	source := createInfisicalSourceInternal(t, service, "https://infisical.example.ts.net/")
	assert.True(t, source.HasCredential)
	require.NotNil(t, source.Settings.Infisical)
	assert.Equal(t, "https://infisical.example.ts.net", source.Settings.Infisical.SiteURL)

	var stored SecretSource
	require.NoError(t, db.First(&stored, "id = ?", source.ID).Error)
	assert.NotEqual(t, "client-secret", stored.Credential)
	plain, err := crypto.Decrypt(stored.Credential)
	require.NoError(t, err)
	assert.Equal(t, "client-secret", plain)

	_, err = service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name: "homelab infisical", Provider: types.SecretSourceProviderInfisical,
		Settings: infisicalSettingsInternal("", "other"), Credential: "other",
	})
	require.ErrorIs(t, err, common.ErrSecretSourceConflict)

	_, err = service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name: "No secret", Provider: types.SecretSourceProviderInfisical, Settings: infisicalSettingsInternal("", "id"),
	})
	require.ErrorIs(t, err, common.ErrValidation)

	_, err = service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name: "No client ID", Provider: types.SecretSourceProviderInfisical, Credential: "secret",
		Settings: types.SecretSourceSettings{Infisical: &types.SecretSourceInfisicalSettings{SiteURL: "https://app.infisical.com"}},
	})
	require.ErrorIs(t, err, common.ErrValidation)

	_, err = service.CreateSource(t.Context(), types.SecretSourceCreateRequest{Name: "Unknown", Provider: "nope"})
	require.ErrorIs(t, err, common.ErrValidation)
}

func TestUpdateSourceKeepsCredentialAndTestResultUnlessConnectionChanges(t *testing.T) {
	service, db := setupSecretSourceServiceTestInternal(t)
	server := (&fakeInfisicalInternal{}).server(t)
	source := createInfisicalSourceInternal(t, service, server.URL)

	_, err := service.TestSource(t.Context(), types.SecretSourceTestRequest{
		SourceID: source.ID, Settings: infisicalSettingsInternal(server.URL, "client-id"), Credential: "client-secret",
	})
	require.NoError(t, err)

	blank := ""
	newName := "Renamed"
	updated, err := service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Name: &newName, Credential: &blank})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	require.NotNil(t, updated.LastTestedAt, "a rename must keep the test result")

	var stored SecretSource
	require.NoError(t, db.First(&stored, "id = ?", source.ID).Error)
	plain, err := crypto.Decrypt(stored.Credential)
	require.NoError(t, err)
	assert.Equal(t, "client-secret", plain)

	changed := infisicalSettingsInternal(server.URL, "other-client")
	updated, err = service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Settings: &changed})
	require.NoError(t, err)
	assert.Nil(t, updated.LastTestedAt, "changing the connection must clear the test result")

	// Testing unsaved settings must not overwrite the stored status.
	_, err = service.TestSource(t.Context(), types.SecretSourceTestRequest{
		SourceID: source.ID, Settings: infisicalSettingsInternal(server.URL, "unsaved-client"),
	})
	require.NoError(t, err)
	current, err := service.GetSource(t.Context(), source.ID)
	require.NoError(t, err)
	assert.Nil(t, current.LastTestedAt)
}

func TestDeleteSourceInUseIsRejected(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	source := createInfisicalSourceInternal(t, service, "")
	bindInfisicalInternal(t, service, source.ID, true, false)

	require.ErrorIs(t, service.DeleteSource(t.Context(), source.ID), common.ErrSecretSourceInUse)
	require.NoError(t, deleteBindingsForTestInternal(t.Context(), service, "project-1"))
	require.NoError(t, service.DeleteSource(t.Context(), source.ID))
}

func TestUpsertBindingValidatesTargetForProvider(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	source := createInfisicalSourceInternal(t, service, "")

	_, err := upsertBindingForTestInternal(t.Context(), service, "project-1", types.SecretSourceBindingUpsertRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceBindingTarget{},
	})
	require.ErrorIs(t, err, common.ErrValidation, "an Infisical source needs an Infisical target")

	_, err = upsertBindingForTestInternal(t.Context(), service, "project-1", types.SecretSourceBindingUpsertRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceBindingTarget{Infisical: &types.SecretSourceInfisicalTarget{ProjectID: "p1"}},
	})
	require.ErrorIs(t, err, common.ErrValidation, "an environment is required")
}

func TestResolveDeployEnvDropsInvalidKeysAndHashesStably(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	fake := &fakeInfisicalInternal{}
	fake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"s3cret"},{"secretKey":"bad-key","secretValue":"x"}]}`)
	source := createInfisicalSourceInternal(t, service, fake.server(t).URL)
	bindInfisicalInternal(t, service, source.ID, true, false)

	project := ProjectRef{ID: "project-1", Name: "billing-api"}
	first, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB_PASS": "s3cret"}, first.Values)
	require.NotEmpty(t, first.Hash)

	second, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, first.Hash, second.Hash)
}

func TestResolveDeployEnvRequiredVersusOptional(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	fake := &fakeInfisicalInternal{}
	fake.setFail(true)
	source := createInfisicalSourceInternal(t, service, fake.server(t).URL)
	project := ProjectRef{ID: "project-1", Name: "billing-api"}

	bindInfisicalInternal(t, service, source.ID, true, false)
	_, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.ErrorIs(t, err, common.ErrSecretFetchFailed)
	assert.Contains(t, err.Error(), "Permission denied")

	binding, err := firstBindingForTestInternal(t.Context(), service, "project-1")
	require.NoError(t, err)
	require.NotNil(t, binding.LastFetchError)

	bindInfisicalInternal(t, service, source.ID, false, false)
	env, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Nil(t, env.Values)
}

func TestResolveDeployEnvEmptyResultFailsOnlyWhenRequired(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	fake := &fakeInfisicalInternal{}
	fake.setSecrets(`{"secrets":[{"secretKey":"bad-key","secretValue":"x"}]}`)
	source := createInfisicalSourceInternal(t, service, fake.server(t).URL)
	project := ProjectRef{ID: "project-1", Name: "billing-api"}

	bindInfisicalInternal(t, service, source.ID, true, false)
	_, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.ErrorIs(t, err, common.ErrSecretFetchFailed)
	assert.Contains(t, err.Error(), "no usable secrets")

	result, err := checkFirstBindingForTestInternal(t.Context(), service, project, types.Actor{})
	require.NoError(t, err)
	assert.Empty(t, result.Keys)
	assert.Equal(t, []string{"bad-key"}, result.InvalidKeys)

	bindInfisicalInternal(t, service, source.ID, false, false)
	env, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	assert.Empty(t, env.Values)
}

func TestResolveDeployEnvSkipsDisabledAndUnboundProjects(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	source := createInfisicalSourceInternal(t, service, "http://127.0.0.1:1")

	env, err := service.ResolveDeployEnv(t.Context(), ProjectRef{ID: "unbound"}, types.Actor{})
	require.NoError(t, err)
	assert.Nil(t, env.Values)

	_, err = upsertBindingForTestInternal(t.Context(), service, "project-1", types.SecretSourceBindingUpsertRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceBindingTarget{Infisical: &types.SecretSourceInfisicalTarget{ProjectID: "p1", Environment: "prod"}},
		Required: true,
		Enabled:  false,
	})
	require.NoError(t, err)
	env, err = service.ResolveDeployEnv(t.Context(), ProjectRef{ID: "project-1"}, types.Actor{})
	require.NoError(t, err)
	assert.Nil(t, env.Values)
}

func TestCheckBindingReportsOverridesAndDrift(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	fake := &fakeInfisicalInternal{}
	fake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"one"},{"secretKey":"API_KEY","secretValue":"k"}]}`)
	source := createInfisicalSourceInternal(t, service, fake.server(t).URL)
	bindInfisicalInternal(t, service, source.ID, true, false)

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env"), []byte("DB_PASS=local\nOTHER=1\n"), 0o600))
	project := ProjectRef{ID: "project-1", Name: "billing-api", Path: projectDir}

	result, err := checkFirstBindingForTestInternal(t.Context(), service, project, types.Actor{})
	require.NoError(t, err)
	assert.Equal(t, []string{"API_KEY", "DB_PASS"}, result.Keys)
	assert.Equal(t, []string{"DB_PASS"}, result.OverriddenKeys)
	assert.True(t, result.NeverDeployed)

	deployed, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	service.RecordDeployed(t.Context(), project.ID, deployed)

	result, err = checkFirstBindingForTestInternal(t.Context(), service, project, types.Actor{})
	require.NoError(t, err)
	assert.False(t, result.RedeployNeeded)

	fake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"two"},{"secretKey":"API_KEY","secretValue":"k"}]}`)
	result, err = checkFirstBindingForTestInternal(t.Context(), service, project, types.Actor{})
	require.NoError(t, err)
	assert.True(t, result.RedeployNeeded)

	binding, err := firstBindingForTestInternal(t.Context(), service, project.ID)
	require.NoError(t, err)
	assert.True(t, binding.RedeployNeeded, "the binding remembers what the last check saw")
}

func TestCheckDriftReportsEachChangeOnce(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	fake := &fakeInfisicalInternal{}
	fake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"one"}]}`)
	source := createInfisicalSourceInternal(t, service, fake.server(t).URL)
	bindInfisicalInternal(t, service, source.ID, true, true)
	project := ProjectRef{ID: "project-1", Name: "billing-api"}
	resolve := func(_ context.Context, _ string) (ProjectRef, error) { return project, nil }

	// Never deployed: nothing to compare against.
	assert.Empty(t, service.CheckDrift(t.Context(), resolve))

	deployed, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	service.RecordDeployed(t.Context(), project.ID, deployed)
	assert.Empty(t, service.CheckDrift(t.Context(), resolve), "unchanged secrets are not drift")

	fake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"two"}]}`)
	changes := service.CheckDrift(t.Context(), resolve)
	require.Len(t, changes, 1)
	assert.Equal(t, "project-1", changes[0].ProjectID)
	assert.True(t, changes[0].AutoRedeploy)

	assert.Empty(t, service.CheckDrift(t.Context(), resolve), "the same change is reported only once")

	fake.setSecrets(`{"secrets":[{"secretKey":"DB_PASS","secretValue":"three"}]}`)
	assert.Len(t, service.CheckDrift(t.Context(), resolve), 1, "a further change is reported again")

	redeployed, err := service.ResolveDeployEnv(t.Context(), project, types.Actor{})
	require.NoError(t, err)
	service.RecordDeployed(t.Context(), project.ID, redeployed)
	binding, err := firstBindingForTestInternal(t.Context(), service, project.ID)
	require.NoError(t, err)
	assert.False(t, binding.RedeployNeeded)
}

func TestStoredCredentialOnlyGoesToItsOwnAddress(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)

	var bodies []string
	capture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(capture.Close)

	source := createInfisicalSourceInternal(t, service, "https://infisical.example.ts.net")

	// Testing an edited address with a blank credential must not send the
	// stored client secret there.
	moved := infisicalSettingsInternal(capture.URL, "client-id")
	result, err := service.TestSource(t.Context(), types.SecretSourceTestRequest{SourceID: source.ID, Settings: moved})
	require.NoError(t, err)
	assert.False(t, result.OK)
	for _, body := range bodies {
		assert.NotContains(t, body, "client-secret")
	}

	// Saving the new address needs the secret again.
	_, err = service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Settings: &moved})
	require.ErrorIs(t, err, common.ErrValidation)
	updated, err := service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Settings: &moved, Credential: new("new-secret")})
	require.NoError(t, err)
	assert.True(t, updated.HasCredential)
	assert.Equal(t, strings.TrimSuffix(capture.URL, "/"), updated.Settings.Infisical.SiteURL)
}
