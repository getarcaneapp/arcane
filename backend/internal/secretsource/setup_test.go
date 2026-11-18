package secretsource

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/secretsource/children/setupplan"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	// infisicalStoreInternal is a stateful fake Infisical with a setup identity
	// that can do everything and a deploy identity that can only read projects
	// it was added to.
	infisicalStoreInternal struct {
		projects map[string]*fakeProjectInternal
		secrets  map[string]map[string]string // projectID|env|path -> key -> value
		folders  map[string][]string          // projectID|env|parent -> names
		nextID   int
		mu       sync.Mutex
	}

	fakeProjectInternal struct {
		members map[string]string // identity ID -> role
		name    string
	}

	// fakeProjectFilesInternal keeps one project's files in memory.
	fakeProjectFilesInternal struct {
		backups []string
		files   ProjectFiles
		mu      sync.Mutex
	}
)

const (
	setupClientIDTest  = "setup-client"
	deployClientIDTest = "client-id"

	setupComposeTest = `services:
	  app:
	    image: app:${APP_TAG:-latest}
	    environment:
	      DB_PASSWORD: ${DB_PASSWORD:?}
	      API_TOKEN: ${API_TOKEN}
	      TZ: ${TZ}
	      NEW_SECRET: ${NEW_SECRET}
	`

	setupEnvTest = "# app\nDB_PASSWORD=hunter2\nAPI_TOKEN=local-token\nTZ=America/New_York\nCOMPOSE_PROJECT_NAME=app\n"
)

func newInfisicalStoreInternal() *infisicalStoreInternal {
	return &infisicalStoreInternal{
		projects: map[string]*fakeProjectInternal{},
		secrets:  map[string]map[string]string{},
		folders:  map[string][]string{},
	}
}

func (f *infisicalStoreInternal) put(projectID, env, path string, values map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := projectID + "|" + env + "|" + path
	if f.secrets[key] == nil {
		f.secrets[key] = map[string]string{}
	}
	for k, v := range values {
		f.secrets[key][k] = v
	}
}

func (f *infisicalStoreInternal) get(projectID, env, path string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.secrets[projectID+"|"+env+"|"+path] {
		out[k] = v
	}
	return out
}

// decodeBodyInternal decodes a request body inside a fake handler, where
// require must not be used.
func decodeBodyInternal(t *testing.T, w http.ResponseWriter, r *http.Request, out any) bool {
	t.Helper()
	if err := json.UnmarshalRead(r.Body, out); err != nil {
		t.Errorf("decode %s %s: %v", r.Method, r.URL.Path, err)
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSONInternal(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	encoded, _ := json.Marshal(body)
	_, _ = w.Write(encoded)
}

func (f *infisicalStoreInternal) server(t *testing.T) *httptest.Server {
	t.Helper()
	identityFor := func(r *http.Request) string {
		return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer tok-")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/universal-auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if !decodeBodyInternal(t, w, r, &body) {
			return
		}
		identity := map[string]string{setupClientIDTest: "i-setup", deployClientIDTest: "i-deploy"}[body["clientId"]]
		if identity == "" {
			writeJSONInternal(w, http.StatusUnauthorized, map[string]string{"message": "Invalid credentials"})
			return
		}
		writeJSONInternal(w, http.StatusOK, map[string]any{"accessToken": "tok-" + identity, "expiresIn": 3600})
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		list := []map[string]any{}
		for id, project := range f.projects {
			list = append(list, map[string]any{"id": id, "name": project.name, "environments": []map[string]string{{"slug": "prod", "name": "Production"}}})
		}
		writeJSONInternal(w, http.StatusOK, map[string]any{"projects": list})
	})
	mux.HandleFunc("POST /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		if identityFor(r) != "i-setup" {
			writeJSONInternal(w, http.StatusForbidden, map[string]string{"message": "Permission denied"})
			return
		}
		var body map[string]any
		if !decodeBodyInternal(t, w, r, &body) {
			return
		}
		f.mu.Lock()
		f.nextID++
		id := fmt.Sprintf("p-%d", f.nextID)
		f.projects[id] = &fakeProjectInternal{name: body["projectName"].(string), members: map[string]string{"i-setup": "admin"}}
		f.mu.Unlock()
		writeJSONInternal(w, http.StatusOK, map[string]any{"project": map[string]any{
			"id": id, "name": body["projectName"], "slug": "slug",
			"environments": []map[string]string{{"id": "e1", "slug": "dev"}, {"id": "e2", "slug": "staging"}, {"id": "e3", "slug": "prod"}},
		}})
	})
	mux.HandleFunc("GET /api/v2/folders", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		names := f.folders[q.Get("projectId")+"|"+q.Get("environment")+"|"+q.Get("path")]
		f.mu.Unlock()
		items := []map[string]string{}
		for _, name := range names {
			items = append(items, map[string]string{"name": name})
		}
		writeJSONInternal(w, http.StatusOK, map[string]any{"folders": items})
	})
	mux.HandleFunc("POST /api/v2/folders", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if !decodeBodyInternal(t, w, r, &body) {
			return
		}
		f.mu.Lock()
		key := body["projectId"] + "|" + body["environment"] + "|" + body["path"]
		f.folders[key] = append(f.folders[key], body["name"])
		f.mu.Unlock()
		writeJSONInternal(w, http.StatusOK, map[string]any{"folder": map[string]string{"id": "f"}})
	})
	mux.HandleFunc("GET /api/v4/secrets", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		project := f.projects[q.Get("projectId")]
		allowed := project != nil && project.members[identityFor(r)] != ""
		f.mu.Unlock()
		if !allowed {
			writeJSONInternal(w, http.StatusForbidden, map[string]string{"message": "You are not allowed to read secrets"})
			return
		}
		items := []map[string]string{}
		for k, v := range f.get(q.Get("projectId"), q.Get("environment"), q.Get("secretPath")) {
			items = append(items, map[string]string{"secretKey": k, "secretValue": v})
		}
		writeJSONInternal(w, http.StatusOK, map[string]any{"secrets": items, "imports": []any{}})
	})
	batch := func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ProjectID   string `json:"projectId"`
			Environment string `json:"environment"`
			SecretPath  string `json:"secretPath"`
			Secrets     []struct {
				SecretKey   string `json:"secretKey"`
				SecretValue string `json:"secretValue"`
			} `json:"secrets"`
		}
		if !decodeBodyInternal(t, w, r, &body) {
			return
		}
		values := map[string]string{}
		for _, secret := range body.Secrets {
			values[secret.SecretKey] = secret.SecretValue
		}
		f.put(body.ProjectID, body.Environment, body.SecretPath, values)
		writeJSONInternal(w, http.StatusOK, map[string]any{"secrets": []any{}})
	}
	mux.HandleFunc("POST /api/v4/secrets/batch", batch)
	mux.HandleFunc("PATCH /api/v4/secrets/batch", batch)
	mux.HandleFunc("GET /api/v1/identities/details", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONInternal(w, http.StatusOK, map[string]any{"identityDetails": map[string]any{"organization": map[string]string{"id": "org1"}}})
	})
	mux.HandleFunc("GET /api/v1/identities", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONInternal(w, http.StatusOK, map[string]any{"identities": []map[string]any{
			{"identityId": "i-setup", "identity": map[string]string{"id": "i-setup", "name": "arcane-setup"}},
			{"identityId": "i-deploy", "identity": map[string]string{"id": "i-deploy", "name": "arcane-deploy"}},
		}})
	})
	mux.HandleFunc("GET /api/v1/auth/universal-auth/identities/{id}", func(w http.ResponseWriter, r *http.Request) {
		clientID := map[string]string{"i-setup": setupClientIDTest, "i-deploy": deployClientIDTest}[r.PathValue("id")]
		writeJSONInternal(w, http.StatusOK, map[string]any{"identityUniversalAuth": map[string]string{"clientId": clientID}})
	})
	mux.HandleFunc("POST /api/v1/projects/{project}/memberships/identities/{identity}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		project := f.projects[r.PathValue("project")]
		if project == nil {
			writeJSONInternal(w, http.StatusNotFound, map[string]string{"message": "Project not found"})
			return
		}
		project.members[r.PathValue("identity")] = "viewer"
		writeJSONInternal(w, http.StatusOK, map[string]any{"identityMembership": map[string]string{"id": "m"}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func (f *fakeProjectFilesInternal) SecretSetupFiles(_ context.Context, _ string) (ProjectFiles, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files, nil
}

func (f *fakeProjectFilesInternal) RemoveSecretEnvKeys(_ context.Context, _ string, expected map[string]string, keepBackup bool, _ types.Actor) (EnvRemoval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, err := projects.ParseProjectEnvContent(f.files.EnvContent, nil)
	if err != nil {
		return EnvRemoval{}, err
	}
	keys := []string{}
	changed := []string{}
	for key, value := range expected {
		if current[key] == value {
			keys = append(keys, key)
		} else {
			changed = append(changed, key)
		}
	}
	updated, removed, referenced := setupplan.PlanEnvRemoval(f.files.EnvContent, keys)
	result := EnvRemoval{Removed: removed, Referenced: referenced, Changed: changed}
	if keepBackup {
		f.backups = append(f.backups, f.files.EnvContent)
		result.BackupFile = ".env.before-secrets-test"
	}
	f.files.EnvContent = updated
	return result, nil
}

func newSetupFixtureInternal(t *testing.T, withSetupIdentity bool) (*SecretSourceService, *infisicalStoreInternal, *fakeProjectFilesInternal, *types.SecretSource, ProjectRef) {
	t.Helper()
	service, _ := setupSecretSourceServiceTestInternal(t)
	store := newInfisicalStoreInternal()
	server := store.server(t)
	files := &fakeProjectFilesInternal{files: ProjectFiles{ComposeContents: []string{setupComposeTest}, EnvContent: setupEnvTest}}
	service.SetProjectFileAccess(files)

	settings := infisicalSettingsInternal(server.URL, deployClientIDTest)
	req := types.SecretSourceCreateRequest{Name: "Infisical", Provider: types.SecretSourceProviderInfisical, Settings: settings, Credential: "deploy-secret"}
	if withSetupIdentity {
		req.Settings.Infisical.SetupClientID = setupClientIDTest
		req.SetupCredential = "setup-secret"
	}
	source, err := service.CreateSource(t.Context(), req)
	require.NoError(t, err)
	return service, store, files, source, ProjectRef{ID: "project-1", Name: "My App"}
}

func stepStatusesInternal(result types.SecretSourceSetupResult) map[string]string {
	statuses := map[string]string{}
	for _, step := range result.Steps {
		statuses[step.ID] = step.Status
	}
	return statuses
}

func TestPlanSetupWithoutSetupIdentityListsVariables(t *testing.T) {
	service, _, _, source, project := newSetupFixtureInternal(t, false)
	assert.False(t, source.HasSetupCredential)

	plan, err := service.PlanSetup(t.Context(), project, types.SecretSourceSetupPlanRequest{SourceID: source.ID})
	require.NoError(t, err)
	assert.False(t, plan.CanWrite)
	assert.Equal(t, "My App", plan.SuggestedProjectName)
	assert.Equal(t, "/my-app", plan.SuggestedSecretPath)

	keys := []string{}
	for _, variable := range plan.Variables {
		keys = append(keys, variable.Key)
		assert.Equal(t, types.SecretSourceSetupRemoteUnknown, variable.Remote)
	}
	assert.Equal(t, []string{"API_TOKEN", "APP_TAG", "DB_PASSWORD", "NEW_SECRET", "TZ"}, keys)

	_, err = service.ApplySetup(t.Context(), project, types.SecretSourceSetupApplyRequest{SourceID: source.ID}, types.SystemUser)
	require.Error(t, err)
}

func TestPlanSetupComparesWithExistingProject(t *testing.T) {
	service, store, _, source, project := newSetupFixtureInternal(t, true)
	store.projects["p-shared"] = &fakeProjectInternal{name: "homelab", members: map[string]string{"i-setup": "admin"}}
	store.put("p-shared", "prod", "/my-app", map[string]string{"DB_PASSWORD": "hunter2", "API_TOKEN": "rotated", "REMOTE_ONLY": "x"})

	plan, err := service.PlanSetup(t.Context(), project, types.SecretSourceSetupPlanRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeSharedFolder, ProjectID: "p-shared", Environment: "prod"},
	})
	require.NoError(t, err)
	assert.True(t, plan.CanWrite)
	assert.Empty(t, plan.RemoteError)
	require.NotNil(t, plan.DeployIdentity)
	assert.Equal(t, "i-deploy", plan.DeployIdentity.ID)

	remote := map[string]string{}
	for _, variable := range plan.Variables {
		remote[variable.Key] = variable.Remote
	}
	assert.Equal(t, map[string]string{
		"DB_PASSWORD": types.SecretSourceSetupRemoteSame,
		"API_TOKEN":   types.SecretSourceSetupRemoteDifferent,
		"TZ":          types.SecretSourceSetupRemoteMissing,
		"APP_TAG":     types.SecretSourceSetupRemoteMissing,
		"NEW_SECRET":  types.SecretSourceSetupRemoteMissing,
	}, remote)

	plan, err = service.PlanSetup(t.Context(), project, types.SecretSourceSetupPlanRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeNewProject, ProjectName: "homelab", Environment: "prod"},
	})
	require.NoError(t, err)
	assert.True(t, plan.ProjectNameTaken)
}

func TestApplySetupNewProjectMovesSecretsAndCleansEnv(t *testing.T) {
	service, store, files, source, project := newSetupFixtureInternal(t, true)

	result, err := service.ApplySetup(t.Context(), project, types.SecretSourceSetupApplyRequest{
		SourceID:            source.ID,
		Target:              types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeNewProject, Environment: "prod"},
		Keys:                []string{"DB_PASSWORD", "API_TOKEN", "NEW_SECRET"},
		Values:              types.SecretSourceSetupValuesImport,
		EnvFile:             types.SecretSourceSetupEnvFileRemove,
		KeepBackup:          true,
		GrantDeployIdentity: true,
		Required:            true,
		AutoRedeploy:        true,
	}, types.SystemUser)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Steps)
	assert.Equal(t, map[string]string{
		"project": "done", "folder": "skipped", "secrets": "done", "grant": "done",
		"binding": "done", "verify": "done", "envFile": "done",
	}, stepStatusesInternal(result))

	require.Len(t, store.projects, 1)
	var projectID string
	for id, p := range store.projects {
		projectID = id
		assert.Equal(t, "My App", p.name)
		assert.Equal(t, "viewer", p.members["i-deploy"])
	}
	assert.Equal(t, map[string]string{"DB_PASSWORD": "hunter2", "API_TOKEN": "local-token", "NEW_SECRET": ""}, store.get(projectID, "prod", "/"))

	require.NotNil(t, result.Binding)
	assert.Equal(t, projectID, result.Binding.Target.Infisical.ProjectID)
	assert.True(t, result.Binding.AutoRedeploy)
	assert.True(t, result.Binding.Required)

	// NEW_SECRET was never in the .env, so only the two imported keys leave it.
	assert.Equal(t, []string{"API_TOKEN", "DB_PASSWORD"}, result.RemovedKeys)
	assert.Equal(t, "# app\nTZ=America/New_York\nCOMPOSE_PROJECT_NAME=app\n", files.files.EnvContent)
	assert.Equal(t, []string{setupEnvTest}, files.backups)

	// A deploy now gets the values from Infisical.
	env, err := service.ResolveDeployEnv(t.Context(), project, types.SystemUser)
	require.NoError(t, err)
	assert.Equal(t, "hunter2", env.Values["DB_PASSWORD"])
}

func TestApplySetupKeepsDifferingValuesUnlessOverwritten(t *testing.T) {
	service, store, files, source, project := newSetupFixtureInternal(t, true)
	store.projects["p-existing"] = &fakeProjectInternal{name: "app", members: map[string]string{"i-setup": "admin", "i-deploy": "viewer"}}
	store.put("p-existing", "prod", "/", map[string]string{"API_TOKEN": "rotated"})

	request := types.SecretSourceSetupApplyRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeExistingProject, ProjectID: "p-existing", Environment: "prod"},
		Keys:     []string{"DB_PASSWORD", "API_TOKEN"},
		Values:   types.SecretSourceSetupValuesImport,
		EnvFile:  types.SecretSourceSetupEnvFileRemove,
	}
	result, err := service.ApplySetup(t.Context(), project, request, types.SystemUser)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Steps)
	assert.Equal(t, "rotated", store.get("p-existing", "prod", "/")["API_TOKEN"], "an existing value is kept by default")
	assert.Equal(t, []string{"DB_PASSWORD"}, result.RemovedKeys, "a key whose values differ stays in the .env")
	assert.Contains(t, files.files.EnvContent, "API_TOKEN=local-token")

	request.OverwriteKeys = []string{"API_TOKEN"}
	result, err = service.ApplySetup(t.Context(), project, request, types.SystemUser)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Steps)
	assert.Equal(t, "local-token", store.get("p-existing", "prod", "/")["API_TOKEN"])
	assert.Equal(t, []string{"API_TOKEN"}, result.RemovedKeys)
	assert.NotContains(t, files.files.EnvContent, "API_TOKEN")
}

func TestApplySetupLeavesEnvAloneWhenDeployIdentityCannotRead(t *testing.T) {
	service, store, files, source, project := newSetupFixtureInternal(t, true)
	store.projects["p-shared"] = &fakeProjectInternal{name: "homelab", members: map[string]string{"i-setup": "admin"}}

	result, err := service.ApplySetup(t.Context(), project, types.SecretSourceSetupApplyRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeSharedFolder, ProjectID: "p-shared", Environment: "prod", SecretPath: "apps/my-app"},
		Keys:     []string{"DB_PASSWORD"},
		Values:   types.SecretSourceSetupValuesImport,
		EnvFile:  types.SecretSourceSetupEnvFileRemove,
		Required: true,
	}, types.SystemUser)
	require.NoError(t, err)
	assert.False(t, result.OK)
	require.NotNil(t, result.Binding)
	assert.False(t, result.Binding.Required, "a binding that cannot be read stays optional")
	statuses := stepStatusesInternal(result)
	assert.Equal(t, "done", statuses["folder"])
	assert.Equal(t, "skipped", statuses["grant"])
	assert.Equal(t, "failed", statuses["verify"])
	assert.Equal(t, "skipped", statuses["envFile"])
	assert.Equal(t, setupEnvTest, files.files.EnvContent)
	assert.Equal(t, []string{"apps"}, store.folders["p-shared|prod|/"])
	assert.Equal(t, []string{"my-app"}, store.folders["p-shared|prod|/apps"])
	assert.Equal(t, "hunter2", store.get("p-shared", "prod", "/apps/my-app")["DB_PASSWORD"])
}

func TestApplySetupPlaceholdersNeverOverwrite(t *testing.T) {
	service, store, files, source, project := newSetupFixtureInternal(t, true)
	store.projects["p-existing"] = &fakeProjectInternal{name: "app", members: map[string]string{"i-setup": "admin", "i-deploy": "viewer"}}
	store.put("p-existing", "prod", "/", map[string]string{"API_TOKEN": "rotated"})

	result, err := service.ApplySetup(t.Context(), project, types.SecretSourceSetupApplyRequest{
		SourceID:      source.ID,
		Target:        types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeExistingProject, ProjectID: "p-existing", Environment: "prod"},
		Keys:          []string{"API_TOKEN", "NEW_SECRET"},
		Values:        types.SecretSourceSetupValuesPlaceholder,
		OverwriteKeys: []string{"API_TOKEN"},
		EnvFile:       types.SecretSourceSetupEnvFileKeep,
	}, types.SystemUser)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Steps)
	assert.Equal(t, map[string]string{"API_TOKEN": "rotated", "NEW_SECRET": ""}, store.get("p-existing", "prod", "/"))
	assert.Equal(t, setupEnvTest, files.files.EnvContent)
}

func TestApplySetupValidatesBeforeWriting(t *testing.T) {
	service, store, _, source, project := newSetupFixtureInternal(t, true)
	base := types.SecretSourceSetupApplyRequest{
		SourceID: source.ID,
		Target:   types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeNewProject, Environment: "prod"},
		Keys:     []string{"DB_PASSWORD"},
		Values:   types.SecretSourceSetupValuesImport,
		EnvFile:  types.SecretSourceSetupEnvFileKeep,
	}
	for name, mutate := range map[string]func(*types.SecretSourceSetupApplyRequest){
		"placeholder removal": func(r *types.SecretSourceSetupApplyRequest) {
			r.Values, r.EnvFile = types.SecretSourceSetupValuesPlaceholder, types.SecretSourceSetupEnvFileRemove
		},
		"no keys":      func(r *types.SecretSourceSetupApplyRequest) { r.Keys = nil },
		"compose key":  func(r *types.SecretSourceSetupApplyRequest) { r.Keys = []string{"COMPOSE_PROJECT_NAME"} },
		"bad key":      func(r *types.SecretSourceSetupApplyRequest) { r.Keys = []string{"not valid"} },
		"bad values":   func(r *types.SecretSourceSetupApplyRequest) { r.Values = "copy" },
		"no env":       func(r *types.SecretSourceSetupApplyRequest) { r.Target.Environment = "" },
		"bad env mode": func(r *types.SecretSourceSetupApplyRequest) { r.EnvFile = "" },
	} {
		req := base
		req.Keys = slices.Clone(base.Keys)
		mutate(&req)
		_, err := service.ApplySetup(t.Context(), project, req, types.SystemUser)
		assert.Error(t, err, name)
	}
	assert.Empty(t, store.projects, "nothing is created when validation fails")
}

func TestSetupCredentialFollowsSetupClientID(t *testing.T) {
	service, _ := setupSecretSourceServiceTestInternal(t)
	settings := infisicalSettingsInternal("https://infisical.example", deployClientIDTest)
	settings.Infisical.SetupClientID = setupClientIDTest

	_, err := service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name: "No secret", Provider: types.SecretSourceProviderInfisical, Settings: settings, Credential: "deploy",
	})
	require.Error(t, err, "a setup client ID needs its secret")

	source, err := service.CreateSource(t.Context(), types.SecretSourceCreateRequest{
		Name: "With setup", Provider: types.SecretSourceProviderInfisical, Settings: settings, Credential: "deploy", SetupCredential: "setup",
	})
	require.NoError(t, err)
	assert.True(t, source.HasSetupCredential)

	// Renaming keeps the stored setup secret.
	source, err = service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Name: new("Renamed"), Settings: &settings})
	require.NoError(t, err)
	assert.True(t, source.HasSetupCredential)

	// A different setup identity needs its own secret.
	changed := infisicalSettingsInternal("https://infisical.example", deployClientIDTest)
	changed.Infisical.SetupClientID = "other-setup"
	_, err = service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Settings: &changed})
	require.Error(t, err)

	// Clearing the setup client ID deletes the secret.
	cleared := infisicalSettingsInternal("https://infisical.example", deployClientIDTest)
	source, err = service.UpdateSource(t.Context(), source.ID, types.SecretSourceUpdateRequest{Settings: &cleared})
	require.NoError(t, err)
	assert.False(t, source.HasSetupCredential)
}

func TestApplySetupKeepsReferencedKeysAndRefusesDuplicateProjects(t *testing.T) {
	service, store, files, source, project := newSetupFixtureInternal(t, true)
	files.files.EnvContent = setupEnvTest + "DATABASE_URL=postgres://app:${DB_PASSWORD}@db/app\n"

	request := types.SecretSourceSetupApplyRequest{
		SourceID:            source.ID,
		Target:              types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeNewProject, ProjectName: "app", Environment: "prod"},
		Keys:                []string{"DB_PASSWORD", "API_TOKEN"},
		Values:              types.SecretSourceSetupValuesImport,
		EnvFile:             types.SecretSourceSetupEnvFileRemove,
		GrantDeployIdentity: true,
	}
	result, err := service.ApplySetup(t.Context(), project, request, types.SystemUser)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Steps)
	assert.Equal(t, []string{"API_TOKEN"}, result.RemovedKeys)
	assert.Contains(t, files.files.EnvContent, "DB_PASSWORD=hunter2", "DATABASE_URL still uses it")
	assert.Contains(t, result.Steps[len(result.Steps)-1].Detail, "DB_PASSWORD stay because other .env entries use them")

	_, err = service.ApplySetup(t.Context(), project, request, types.SystemUser)
	require.Error(t, err, "a re-run must not create a second project")
	assert.Len(t, store.projects, 1)
}
