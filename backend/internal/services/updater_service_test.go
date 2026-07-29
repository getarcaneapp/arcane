package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/refs"
)

type fakeDockerClientProviderInternal struct {
	client any
}

func (f fakeDockerClientProviderInternal) DockerClient(context.Context) (*client.Client, error) {
	cli, _ := f.client.(*client.Client)
	if cli == nil {
		return nil, errors.New("docker client unavailable")
	}
	return cli, nil
}

type fakeImagePullerInternal struct {
	pulled []string
	fail   map[string]error
}

func (f *fakeImagePullerInternal) PullImage(_ context.Context, imageRef string, _ io.Writer) error {
	f.pulled = append(f.pulled, imageRef)
	if f.fail == nil {
		return nil
	}
	return f.fail[imageRef]
}

type fakeRunRecorderInternal struct {
	results []updater.ResourceResult
}

func (f *fakeRunRecorderInternal) RecordUpdateRun(_ context.Context, result updater.ResourceResult) error {
	f.results = append(f.results, result)
	return nil
}

type fakeSettingsProviderInternal struct{}

func (fakeSettingsProviderInternal) ExcludedContainers(context.Context) ([]string, error) {
	return nil, nil
}

type fakeUsedImageCollectorInternal struct {
	images map[string]struct{}
}

func (f fakeUsedImageCollectorInternal) UsedImages(context.Context) (map[string]struct{}, error) {
	return f.images, nil
}

type fakeProjectUpdaterInternal struct {
	projects   map[string]updater.ComposeProject
	updateErrs map[string]error
	calls      []string
}

func (f *fakeProjectUpdaterInternal) ProjectByComposeName(_ context.Context, composeName string) (updater.ComposeProject, error) {
	if project, ok := f.projects[composeName]; ok {
		return project, nil
	}
	return updater.ComposeProject{}, errors.New("project not found")
}

func (f *fakeProjectUpdaterInternal) UpdateServices(_ context.Context, projectID string, services []string) error {
	f.calls = append(f.calls, projectID+":"+strings.Join(services, ","))
	if f.updateErrs == nil {
		return nil
	}
	return f.updateErrs[projectID]
}

func newUpdaterApplyPendingDockerServerInternal(
	t *testing.T,
	containers []container.Summary,
	verificationByService map[string][]container.Summary,
	inspectByID map[string]container.InspectResponse,
	imageInspectByRef map[string]dockertypesimage.InspectResponse,
) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			_, _ = io.WriteString(w, "OK")
		case strings.HasSuffix(r.URL.Path, "/version"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"ApiVersion":    "1.41",
				"MinAPIVersion": "1.24",
				"Version":       "24.0.0",
			})
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]dockertypesimage.Summary{})
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			encodedRef := strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/images/")+len("/images/"):], "/json")
			imageRef, err := url.PathUnescape(encodedRef)
			require.NoError(t, err)
			inspect, ok := imageInspectByRef[imageRef]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(inspect)
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			response := containers
			if filters := strings.TrimSpace(r.URL.Query().Get("filters")); filters != "" {
				var raw map[string]map[string]bool
				require.NoError(t, json.Unmarshal([]byte(filters), &raw))
				projectName := ""
				serviceName := ""
				for value := range raw["label"] {
					switch {
					case strings.HasPrefix(value, "com.docker.compose.project="):
						projectName = strings.TrimPrefix(value, "com.docker.compose.project=")
					case strings.HasPrefix(value, "com.docker.compose.service="):
						serviceName = strings.TrimPrefix(value, "com.docker.compose.service=")
					}
				}
				if projectName != "" && serviceName != "" {
					if matched, ok := verificationByService[projectName+"/"+serviceName]; ok {
						response = matched
					} else {
						response = nil
					}
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		case strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			containerID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/containers/"):], "/containers/"), "/json")
			inspect, ok := inspectByID[containerID]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(inspect)
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(server.Close)
	return server
}

type mockSystemUpgradeServiceInternal struct {
	triggerCalled  bool
	triggerError   error
	capturedUser   *models.User
	capturedTarget *updater.SelfUpdateTarget
}

func (m *mockSystemUpgradeServiceInternal) TriggerUpgradeViaCLI(_ context.Context, user models.User, target updater.SelfUpdateTarget) (string, error) {
	m.triggerCalled = true
	m.capturedUser = &user
	m.capturedTarget = &target
	return "upgrader-container-id", m.triggerError
}

func TestUpdaterService_ApplyPendingNoRecordsInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	svc, svcErr := NewUpdaterService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svcErr)

	result, err := svc.ApplyPending(ctx, arcaneupdater.Options{DryRun: true})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Zero(t, result.Checked)
	assert.Zero(t, result.Updated)
	assert.Zero(t, result.Skipped)
	assert.Zero(t, result.Failed)
	assert.Empty(t, result.Items)
}

// Type and ResourceIds are Arcane-side scoping the engine never acted on;
// ApplyPending handles a scoped request itself before reaching the engine.
func TestUpdaterService_ModuleOptionsFromUpdaterOptionsInternal(t *testing.T) {
	got := moduleOptionsFromUpdaterOptionsInternal(arcaneupdater.Options{
		Type:        string(updater.ResourceTypeImage),
		ResourceIds: []string{"image-1", "image-2"},
		ForceUpdate: true,
		DryRun:      true,
	})

	assert.Equal(t, updater.Options{Force: true, DryRun: true}, got)
}

func TestUpdaterService_ResultFromModulePreservesRestartedInternal(t *testing.T) {
	got := resultFromModuleInternal(&updater.Result{
		Success:   true,
		Checked:   2,
		Updated:   1,
		Restarted: 1,
		Items: []updater.ResourceResult{
			{
				ResourceID:    "web-id",
				ResourceName:  "web",
				ResourceType:  updater.ResourceTypeContainer,
				Status:        updater.StatusRestarted,
				UpdateApplied: true,
			},
		},
	})

	require.NotNil(t, got)
	assert.Equal(t, 1, got.Restarted)
	require.Len(t, got.Items, 1)
	assert.Equal(t, arcaneupdater.StatusRestarted, got.Items[0].Status)
	assert.True(t, got.Items[0].UpdateApplied)
}

func TestUpdaterService_TriggerSelfUpdateViaCLIInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("server label triggers upgrade with system user", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{}
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)
		require.NoError(t, svcErr)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane", map[string]string{
			labels.LabelArcane: "true",
		})

		require.NoError(t, err)
		assert.True(t, mockUpgrade.triggerCalled)
		require.NotNil(t, mockUpgrade.capturedUser)
		assert.Equal(t, systemUser.ID, mockUpgrade.capturedUser.ID)
		assert.Equal(t, systemUser.Username, mockUpgrade.capturedUser.Username)
		require.NotNil(t, mockUpgrade.capturedTarget)
		assert.Equal(t, "container-1", mockUpgrade.capturedTarget.ContainerID)
		assert.Equal(t, "arcane", mockUpgrade.capturedTarget.ContainerName)
	})

	t.Run("agent label triggers upgrade", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{}
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)
		require.NoError(t, svcErr)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane-agent", map[string]string{
			labels.LabelArcaneAgent: "true",
		})

		require.NoError(t, err)
		assert.True(t, mockUpgrade.triggerCalled)
	})

	t.Run("non Arcane labels fail without triggering upgrade", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{}
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)
		require.NoError(t, svcErr)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "demo", map[string]string{
			"com.example.app": "demo",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an Arcane self-update target")
		assert.False(t, mockUpgrade.triggerCalled)
	})

	t.Run("missing upgrade service reports required hook", func(t *testing.T) {
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		require.NoError(t, svcErr)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane", map[string]string{
			labels.LabelArcane: "true",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "self-update requires CLI upgrade service")
	})

	t.Run("upgrade errors are wrapped", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{triggerError: errors.New("upgrade failed")}
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)
		require.NoError(t, svcErr)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane", map[string]string{
			labels.LabelArcane: "true",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "CLI upgrade failed")
	})
}

func TestUpdaterService_StatusTrackingInternal(t *testing.T) {
	svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svcErr)

	stopContainer := svc.BeginContainerUpdate("container-1")
	stopProject := svc.BeginProjectUpdate("project-a")

	status := svc.GetStatus()
	assert.Equal(t, 1, status.UpdatingContainers)
	assert.Equal(t, 1, status.UpdatingProjects)
	assert.Equal(t, []string{"container-1"}, status.ContainerIds)
	assert.Equal(t, []string{"project-a"}, status.ProjectIds)

	stopContainer()
	stopProject()

	status = svc.GetStatus()
	assert.Zero(t, status.UpdatingContainers)
	assert.Zero(t, status.UpdatingProjects)
	assert.Empty(t, status.ContainerIds)
	assert.Empty(t, status.ProjectIds)
}

func TestUpdaterService_DockerClientAdapterInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("missing docker service returns unavailable error", func(t *testing.T) {
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		require.NoError(t, svcErr)

		cli, err := svc.DockerClient(ctx)

		require.Error(t, err)
		assert.Nil(t, cli)
		assert.Contains(t, err.Error(), "docker service unavailable")
	})

	t.Run("delegates to configured docker service", func(t *testing.T) {
		server := newProjectImagePullServer(t, nil)
		wantClient := newTestDockerClient(t, server)
		dockerSvc := &DockerClientService{client: wantClient}
		svc, svcErr := NewUpdaterService(nil, nil, dockerSvc, nil, nil, nil, nil, nil, nil, nil, nil)
		require.NoError(t, svcErr)

		gotClient, err := svc.DockerClient(ctx)

		require.NoError(t, err)
		assert.Same(t, wantClient, gotClient)
	})
}

func TestUpdaterService_PullImageAdapterInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("missing image service returns unavailable error", func(t *testing.T) {
		svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		require.NoError(t, svcErr)

		err := svc.PullImage(ctx, "registry.example.com/app:1.2.3", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image service unavailable")
	})

	t.Run("delegates to Arcane image puller", func(t *testing.T) {
		db := setupProjectTestDB(t)
		server := newProjectImagePullServer(t, nil)
		dockerSvc := &DockerClientService{client: newTestDockerClient(t, server)}
		imageSvc := NewImageService(db, dockerSvc, nil, nil, nil, NewEventService(db, nil, nil))
		svc, svcErr := NewUpdaterService(db, nil, dockerSvc, nil, nil, nil, nil, imageSvc, nil, nil, nil)
		require.NoError(t, svcErr)
		var progress bytes.Buffer

		err := svc.PullImage(ctx, "nginx:latest", &progress)

		require.NoError(t, err)
		assert.Contains(t, progress.String(), "Pulled")
	})

	t.Run("passes database registry credentials to Arcane image puller", func(t *testing.T) {
		db := setupProjectTestDB(t)
		require.NoError(t, db.AutoMigrate(&models.ContainerRegistry{}, &models.KVEntry{}))
		crypto.InitEncryption(&crypto.Config{
			Environment:   string(config.AppEnvironmentTest),
			EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		})
		createTestPullRegistry(t, db, "https://registry.example.com", "db-user", "db-token")

		imageRef := "registry.example.com/team/app:latest"
		var gotAuth string
		server := newProjectImagePullServerWithObserverInternal(t, nil, func(fullRef string, authHeader string) {
			if fullRef == imageRef {
				gotAuth = authHeader
			}
		})
		dockerSvc := &DockerClientService{client: newTestDockerClient(t, server)}
		registrySvc := NewContainerRegistryService(db, nil, NewKVService(db))
		imageSvc := NewImageService(db, dockerSvc, registrySvc, nil, nil, NewEventService(db, nil, nil))
		envSvc := NewEnvironmentService(db, nil, nil, nil, nil, nil)
		projectSvc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, nil).
			WithRegistryCredentialsProvider(envSvc.GetEnabledRegistryCredentials)
		svc, svcErr := NewUpdaterService(db, nil, dockerSvc, projectSvc, nil, nil, nil, imageSvc, nil, nil, nil)
		require.NoError(t, svcErr)
		var progress bytes.Buffer

		err := svc.PullImage(ctx, imageRef, &progress)

		require.NoError(t, err)
		require.NotEmpty(t, gotAuth)
		authConfig := decodeRegistryAuth(t, gotAuth)
		assert.Equal(t, "db-user", authConfig.Username)
		assert.Equal(t, "db-token", authConfig.Password)
		assert.Contains(t, progress.String(), "Pulled")
	})
}

func TestUpdaterService_PendingImageUpdatesAdapterInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	latest := "1.2.4"
	currentDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	latestDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	lastError := "previous check failed"
	checkTime := time.Now().Add(-time.Hour).UTC()
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "pending",
		Repository:     "registry.example.com/team/app",
		Tag:            "1.2.3",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeTag,
		CurrentVersion: "1.2.3",
		LatestVersion:  &latest,
		CurrentDigest:  &currentDigest,
		LatestDigest:   &latestDigest,
		CheckTime:      checkTime,
		LastError:      &lastError,
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:         "not-pending",
		Repository: "registry.example.com/team/old",
		Tag:        "1.0.0",
		HasUpdate:  false,
		UpdateType: models.UpdateTypeDigest,
		CheckTime:  checkTime,
	}).Error)
	svc, svcErr := NewUpdaterService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svcErr)

	records, err := svc.PendingImageUpdates(ctx)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "pending", records[0].ID)
	assert.Equal(t, "registry.example.com/team/app", records[0].Repository)
	assert.Equal(t, "1.2.3", records[0].Tag)
	assert.True(t, records[0].HasUpdate)
	assert.Equal(t, updater.UpdateTypeTag, records[0].UpdateType)
	assert.Equal(t, "1.2.3", records[0].CurrentVersion)
	assert.Equal(t, &latest, records[0].LatestVersion)
	assert.Equal(t, &currentDigest, records[0].CurrentDigest)
	assert.Equal(t, &latestDigest, records[0].LatestDigest)
	assert.Equal(t, &lastError, records[0].LastError)
}

// TestUpdaterService_PendingImageUpdatesFlushesPendingNotificationsInternal guards
// issue #3132: the updater consumes and clears has_update records without checking
// notification_sent, so an "Updates Available" notification pending at consumption
// time was silently lost. PendingImageUpdates must flush it before the engine runs.
func TestUpdaterService_PendingImageUpdatesFlushesPendingNotificationsInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.ImageUpdateRecord{}, &models.NotificationSettings{}))

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	require.NoError(t, db.Create(&models.NotificationSettings{
		Provider: models.NotificationProviderGeneric,
		Enabled:  true,
		Config: models.JSON{
			"webhookUrl":  server.URL,
			"method":      "POST",
			"contentType": "application/json",
		},
	}).Error)

	notif := NewNotificationService(db, nil, nil, nil)
	imageUpdates := NewImageUpdateService(db, nil, nil, nil, nil, notif, nil)
	svc, svcErr := NewUpdaterService(db, nil, nil, nil, imageUpdates, nil, nil, nil, notif, nil, nil)
	require.NoError(t, svcErr)

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:               "sha256:pending-unnotified",
		Repository:       "test/repo",
		Tag:              "latest",
		HasUpdate:        true,
		NotificationSent: false,
	}).Error)

	records, err := svc.PendingImageUpdates(ctx)

	require.NoError(t, err)
	require.Len(t, records, 1)
	require.EqualValues(t, 1, calls.Load())
	var reloaded models.ImageUpdateRecord
	require.NoError(t, db.First(&reloaded, "id = ?", "sha256:pending-unnotified").Error)
	assert.True(t, reloaded.NotificationSent)
}

// With no providers configured, PendingImageUpdates still returns the records and
// leaves notification_sent false (preserves the #3079 "don't mark when nothing
// delivered" semantics).
func TestUpdaterService_PendingImageUpdatesNoProvidersLeavesUnnotifiedInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.ImageUpdateRecord{}, &models.NotificationSettings{}))

	notif := NewNotificationService(db, nil, nil, nil)
	imageUpdates := NewImageUpdateService(db, nil, nil, nil, nil, notif, nil)
	svc, svcErr := NewUpdaterService(db, nil, nil, nil, imageUpdates, nil, nil, nil, notif, nil, nil)
	require.NoError(t, svcErr)

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:               "sha256:pending-no-provider",
		Repository:       "test/repo",
		Tag:              "latest",
		HasUpdate:        true,
		NotificationSent: false,
	}).Error)

	records, err := svc.PendingImageUpdates(ctx)

	require.NoError(t, err)
	require.Len(t, records, 1)
	var reloaded models.ImageUpdateRecord
	require.NoError(t, db.First(&reloaded, "id = ?", "sha256:pending-no-provider").Error)
	assert.False(t, reloaded.NotificationSent)
}

func TestUpdaterService_RecordUpdateRunAdapterInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.AutoUpdateRecord{}))
	svc, svcErr := NewUpdaterService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svcErr)

	err := svc.RecordUpdateRun(ctx, updater.ResourceResult{
		ResourceID:      "container-1",
		ResourceName:    "web",
		ResourceType:    updater.ResourceTypeContainer,
		Status:          updater.StatusUpdated,
		UpdateAvailable: true,
		UpdateApplied:   true,
		OldImage:        "nginx:1.2.3",
		NewImage:        "nginx:1.2.4",
		Details:         map[string]any{"source": "test"},
	})

	require.NoError(t, err)
	var record models.AutoUpdateRecord
	require.NoError(t, db.First(&record, "resource_id = ?", "container-1").Error)
	assert.Equal(t, "web", record.ResourceName)
	assert.Equal(t, "container", record.ResourceType)
	assert.Equal(t, models.AutoUpdateStatus(updater.StatusUpdated), record.Status)
	assert.True(t, record.UpdateAvailable)
	assert.True(t, record.UpdateApplied)
	assert.Equal(t, "nginx:1.2.3", record.OldImageVersions["main"])
	assert.Equal(t, "nginx:1.2.4", record.NewImageVersions["main"])
	assert.Equal(t, "test", record.Details["source"])
}

func TestUpdaterService_ApplyPending_ProjectFailureDoesNotBlockOtherProjectsInternal(t *testing.T) {
	ctx := context.Background()

	oldRefFailed := "registry.example.com/team/fail:1.0.0"
	newRefFailed := "registry.example.com/team/fail:1.0.1"
	oldRefUpdated := "registry.example.com/team/success:2.0.0"
	newRefUpdated := "registry.example.com/team/success:2.0.1"
	oldImageIDFailed := "sha256:old-fail"
	newImageIDFailed := "sha256:new-fail"
	oldImageIDUpdated := "sha256:old-success"
	newImageIDUpdated := "sha256:new-success"

	failedLabels := map[string]string{
		"com.docker.compose.project": "proj-fail",
		"com.docker.compose.service": "app",
	}
	updatedLabels := map[string]string{
		"com.docker.compose.project": "proj-success",
		"com.docker.compose.service": "app",
	}

	containers := []container.Summary{
		{
			ID:      "container-fail",
			Names:   []string{"/proj-fail-app-1"},
			Image:   oldRefFailed,
			ImageID: oldImageIDFailed,
			Labels:  failedLabels,
			State:   "running",
		},
		{
			ID:      "container-success",
			Names:   []string{"/proj-success-app-1"},
			Image:   oldRefUpdated,
			ImageID: oldImageIDUpdated,
			Labels:  updatedLabels,
			State:   "running",
		},
	}

	verificationByService := map[string][]container.Summary{
		"proj-success/app": {
			{
				ID:      "container-success-new",
				Names:   []string{"/proj-success-app-1"},
				Image:   newRefUpdated,
				ImageID: newImageIDUpdated,
				Labels:  updatedLabels,
				State:   "running",
			},
		},
	}

	inspectByID := map[string]container.InspectResponse{
		"container-fail": {
			ID:    "container-fail",
			Image: oldImageIDFailed,
			Config: &container.Config{
				Image:  oldRefFailed,
				Labels: failedLabels,
			},
		},
		"container-success": {
			ID:    "container-success",
			Image: oldImageIDUpdated,
			Config: &container.Config{
				Image:  oldRefUpdated,
				Labels: updatedLabels,
			},
		},
	}

	imageInspectByRef := map[string]dockertypesimage.InspectResponse{
		newRefFailed: {
			ID:       newImageIDFailed,
			RepoTags: []string{newRefFailed},
		},
		newRefUpdated: {
			ID:       newImageIDUpdated,
			RepoTags: []string{newRefUpdated},
		},
	}

	server := newUpdaterApplyPendingDockerServerInternal(t, containers, verificationByService, inspectByID, imageInspectByRef)
	dockerProvider := fakeDockerClientProviderInternal{client: newTestDockerClient(t, server)}
	puller := &fakeImagePullerInternal{}
	recorder := &fakeRunRecorderInternal{}
	projectUpdater := &fakeProjectUpdaterInternal{
		projects: map[string]updater.ComposeProject{
			"proj-fail":    {ID: "project-fail", Name: "proj-fail"},
			"proj-success": {ID: "project-success", Name: "proj-success"},
		},
		updateErrs: map[string]error{
			"project-fail": errors.New("pull updated service images: unauthorized"),
		},
	}

	svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svcErr)
	engine, engineErr := updater.New(updater.Config{
		DockerClientProvider: dockerProvider,
		ImagePuller:          puller,
		PendingStore: updater.NewMemoryPendingStore(
			updater.ImageUpdateRecord{
				ID:             oldImageIDFailed,
				Repository:     "registry.example.com/team/fail",
				Tag:            "1.0.0",
				HasUpdate:      true,
				UpdateType:     updater.UpdateTypeTag,
				CurrentVersion: "1.0.0",
				LatestVersion:  ptr("1.0.1"),
			},
			updater.ImageUpdateRecord{
				ID:             oldImageIDUpdated,
				Repository:     "registry.example.com/team/success",
				Tag:            "2.0.0",
				HasUpdate:      true,
				UpdateType:     updater.UpdateTypeTag,
				CurrentVersion: "2.0.0",
				LatestVersion:  ptr("2.0.1"),
			},
		),
		RunRecorder:    recorder,
		Settings:       fakeSettingsProviderInternal{},
		ProjectUpdater: projectUpdater,
		UsedImageCollector: fakeUsedImageCollectorInternal{images: map[string]struct{}{
			oldRefFailed:  {},
			oldRefUpdated: {},
		}},
		LabelPolicy: updater.DefaultLabelPolicy(),
	})
	require.NoError(t, engineErr)
	t.Cleanup(func() {
		require.NoError(t, engine.Close())
	})
	svc.engine = engine

	result, err := svc.ApplyPending(ctx, arcaneupdater.Options{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 3, result.Updated, "updated count should include both image pulls and the successfully updated container")
	assert.Equal(t, 1, result.Failed, "the failed project should be recorded as failed")
	assert.GreaterOrEqual(t, len(result.Items), 4)
	assert.ElementsMatch(t, []string{newRefFailed, newRefUpdated}, puller.pulled)
	assert.ElementsMatch(t, []string{"project-fail:app", "project-success:app"}, projectUpdater.calls)

	statusByResource := map[string]string{}
	errorByResource := map[string]string{}
	for _, item := range result.Items {
		statusByResource[item.ResourceID] = item.Status
		errorByResource[item.ResourceID] = item.Error
	}

	assert.Equal(t, string(updater.StatusFailed), statusByResource["container-fail"])
	assert.Contains(t, errorByResource["container-fail"], "project-level update failed")
	assert.Equal(t, string(updater.StatusUpdated), statusByResource["container-success"])

	var recordedStatuses []updater.ResourceStatus
	for _, recorded := range recorder.results {
		recordedStatuses = append(recordedStatuses, recorded.Status)
	}
	assert.Contains(t, recordedStatuses, updater.StatusFailed)
	assert.Contains(t, recordedStatuses, updater.StatusUpdated)
}

func TestUpdaterService_ApplyPending_RoutesLegacyArcaneServerThroughSelfUpgradeInternal(t *testing.T) {
	ctx := context.Background()

	oldRef := "ghcr.io/getarcaneapp/arcane:1.0.0"
	newRef := "ghcr.io/getarcaneapp/arcane:1.0.1"
	oldImageID := "sha256:old-arcane"
	newImageID := "sha256:new-arcane"
	arcaneLabels := map[string]string{
		"com.docker.compose.project":   "arcane",
		"com.docker.compose.service":   "server",
		labels.LabelArcaneLegacyServer: "true",
	}

	containers := []container.Summary{
		{
			ID:      "arcane-container",
			Names:   []string{"/arcane"},
			Image:   oldRef,
			ImageID: oldImageID,
			Labels:  arcaneLabels,
			State:   "running",
		},
	}
	verificationByService := map[string][]container.Summary{
		"arcane/server": {
			{
				ID:      "arcane-container-new",
				Names:   []string{"/arcane"},
				Image:   newRef,
				ImageID: newImageID,
				Labels:  arcaneLabels,
				State:   "running",
			},
		},
	}
	inspectByID := map[string]container.InspectResponse{
		"arcane-container": {
			ID:    "arcane-container",
			Name:  "/arcane",
			Image: oldImageID,
			Config: &container.Config{
				Image:  oldRef,
				Labels: arcaneLabels,
			},
		},
	}
	imageInspectByRef := map[string]dockertypesimage.InspectResponse{
		newRef: {
			ID:       newImageID,
			RepoTags: []string{newRef},
		},
	}

	server := newUpdaterApplyPendingDockerServerInternal(t, containers, verificationByService, inspectByID, imageInspectByRef)
	dockerProvider := fakeDockerClientProviderInternal{client: newTestDockerClient(t, server)}
	puller := &fakeImagePullerInternal{}
	projectUpdater := &fakeProjectUpdaterInternal{
		projects: map[string]updater.ComposeProject{
			"arcane": {ID: "project-arcane", Name: "arcane"},
		},
	}
	mockUpgrade := &mockSystemUpgradeServiceInternal{}

	svc, svcErr := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)
	require.NoError(t, svcErr)
	engine, engineErr := updater.New(updater.Config{
		DockerClientProvider: dockerProvider,
		ImagePuller:          puller,
		PendingStore: updater.NewMemoryPendingStore(updater.ImageUpdateRecord{
			ID:             oldImageID,
			Repository:     "ghcr.io/getarcaneapp/arcane",
			Tag:            "1.0.0",
			HasUpdate:      true,
			UpdateType:     updater.UpdateTypeTag,
			CurrentVersion: "1.0.0",
			LatestVersion:  ptr("1.0.1"),
		}),
		Settings:       fakeSettingsProviderInternal{},
		ProjectUpdater: projectUpdater,
		SelfUpdater:    svc,
		UsedImageCollector: fakeUsedImageCollectorInternal{images: map[string]struct{}{
			oldRef: {},
		}},
		LabelPolicy: updater.DefaultLabelPolicy(),
	})
	require.NoError(t, engineErr)
	t.Cleanup(func() {
		require.NoError(t, engine.Close())
	})
	svc.engine = engine

	result, err := svc.ApplyPending(ctx, arcaneupdater.Options{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, mockUpgrade.triggerCalled, "scheduled auto-update should use CLI self-upgrade for legacy Arcane server labels")
	assert.Empty(t, projectUpdater.calls, "legacy Arcane server should not be updated through project services")
}

// The engine picks the reference Arcane's updates are applied to, so its choice
// is worth pinning here as well as in the library.
func TestPullableImageRefInternal(t *testing.T) {
	tests := []struct {
		name         string
		summaryImage string
		inspectImage string
		repoTags     []string
		expectedRef  string
	}{
		{
			name:         "inspect config image wins",
			summaryImage: "nginx:latest",
			inspectImage: "registry.example.com/nginx:stable",
			expectedRef:  "registry.example.com/nginx:stable",
		},
		{
			name:         "summary image used when inspect is image ID",
			summaryImage: "redis:7",
			inspectImage: "sha256:abcdef",
			expectedRef:  "redis:7",
		},
		{
			name:         "repo tag fallback skips none tag",
			summaryImage: "sha256:abcdef",
			inspectImage: "sha256:abcdef",
			repoTags:     []string{"<none>:<none>", "postgres:16"},
			expectedRef:  "postgres:16",
		},
		{
			name:         "no pullable ref",
			summaryImage: "sha256:abcdef",
			inspectImage: "sha256:abcdef",
			repoTags:     []string{"<none>:<none>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedRef, refs.PullableImageRef(tt.summaryImage, tt.inspectImage, tt.repoTags))
		})
	}
}
