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
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	moduleapi "github.com/getarcaneapp/updater/api"
	updaterlabels "github.com/getarcaneapp/updater/pkg/labels"
	moduletypes "github.com/getarcaneapp/updater/types"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	results []moduletypes.ResourceResult
}

func (f *fakeRunRecorderInternal) RecordUpdateRun(_ context.Context, result moduletypes.ResourceResult) error {
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
	projects   map[string]moduletypes.ComposeProject
	updateErrs map[string]error
	calls      []string
}

func (f *fakeProjectUpdaterInternal) ProjectByComposeName(_ context.Context, composeName string) (moduletypes.ComposeProject, error) {
	if project, ok := f.projects[composeName]; ok {
		return project, nil
	}
	return moduletypes.ComposeProject{}, errors.New("project not found")
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
	triggerCalled bool
	triggerError  error
	capturedUser  *models.User
}

func (m *mockSystemUpgradeServiceInternal) TriggerUpgradeViaCLI(_ context.Context, user models.User) error {
	m.triggerCalled = true
	m.capturedUser = &user
	return m.triggerError
}

func TestUpdaterService_ApplyPendingNoRecordsInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	svc := NewUpdaterService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.ApplyPending(ctx, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Zero(t, result.Checked)
	assert.Zero(t, result.Updated)
	assert.Zero(t, result.Skipped)
	assert.Zero(t, result.Failed)
	assert.Empty(t, result.Items)
}

func TestUpdaterService_ConfigUsesComposeStandaloneFallbackSettingInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("defaults disabled without settings service", func(t *testing.T) {
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		cfg := svc.configInternal(ctx)

		assert.False(t, cfg.AllowComposeStandaloneFallback)
	})

	t.Run("reads settings service", func(t *testing.T) {
		db := setupProjectTestDB(t)
		settingsSvc, err := NewSettingsService(ctx, db)
		require.NoError(t, err)
		require.NoError(t, settingsSvc.SetBoolSetting(ctx, autoUpdateComposeStandaloneFallbackSettingKeyInternal, true))
		svc := NewUpdaterService(db, settingsSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		cfg := svc.configInternal(ctx)

		assert.True(t, cfg.AllowComposeStandaloneFallback)
	})
}

func TestUpdaterService_TriggerSelfUpdateViaCLIInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("server label triggers upgrade with system user", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{}
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane", map[string]string{
			updaterlabels.LabelArcane: "true",
		})

		require.NoError(t, err)
		assert.True(t, mockUpgrade.triggerCalled)
		require.NotNil(t, mockUpgrade.capturedUser)
		assert.Equal(t, systemUser.ID, mockUpgrade.capturedUser.ID)
		assert.Equal(t, systemUser.Username, mockUpgrade.capturedUser.Username)
	})

	t.Run("agent label triggers upgrade", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{}
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane-agent", map[string]string{
			updaterlabels.LabelArcaneAgent: "true",
		})

		require.NoError(t, err)
		assert.True(t, mockUpgrade.triggerCalled)
	})

	t.Run("non Arcane labels fail without triggering upgrade", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{}
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "demo", map[string]string{
			"com.example.app": "demo",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an Arcane self-update target")
		assert.False(t, mockUpgrade.triggerCalled)
	})

	t.Run("missing upgrade service reports required hook", func(t *testing.T) {
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane", map[string]string{
			updaterlabels.LabelArcane: "true",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "self-update requires CLI upgrade service")
	})

	t.Run("upgrade errors are wrapped", func(t *testing.T) {
		mockUpgrade := &mockSystemUpgradeServiceInternal{triggerError: errors.New("upgrade failed")}
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUpgrade, nil)

		err := svc.TriggerSelfUpdateViaCLI(ctx, "test", "container-1", "arcane", map[string]string{
			updaterlabels.LabelArcane: "true",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "CLI upgrade failed")
	})
}

func TestUpdaterService_StatusTrackingInternal(t *testing.T) {
	svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

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
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		cli, err := svc.DockerClient(ctx)

		require.Error(t, err)
		assert.Nil(t, cli)
		assert.Contains(t, err.Error(), "docker service unavailable")
	})

	t.Run("delegates to configured docker service", func(t *testing.T) {
		server := newProjectImagePullServer(t, nil)
		wantClient := newTestDockerClient(t, server)
		dockerSvc := &DockerClientService{client: wantClient}
		svc := NewUpdaterService(nil, nil, dockerSvc, nil, nil, nil, nil, nil, nil, nil, nil)

		gotClient, err := svc.DockerClient(ctx)

		require.NoError(t, err)
		assert.Same(t, wantClient, gotClient)
	})
}

func TestUpdaterService_PullImageAdapterInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("missing image service returns unavailable error", func(t *testing.T) {
		svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		err := svc.PullImage(ctx, "registry.example.com/app:1.2.3", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image service unavailable")
	})

	t.Run("delegates to Arcane image puller", func(t *testing.T) {
		db := setupProjectTestDB(t)
		server := newProjectImagePullServer(t, nil)
		dockerSvc := &DockerClientService{client: newTestDockerClient(t, server)}
		imageSvc := NewImageService(db, dockerSvc, nil, nil, nil, NewEventService(db, nil, nil))
		svc := NewUpdaterService(db, nil, dockerSvc, nil, nil, nil, nil, imageSvc, nil, nil, nil)
		var progress bytes.Buffer

		err := svc.PullImage(ctx, "nginx:latest", &progress)

		require.NoError(t, err)
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
	svc := NewUpdaterService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	records, err := svc.PendingImageUpdates(ctx)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "pending", records[0].ID)
	assert.Equal(t, "registry.example.com/team/app", records[0].Repository)
	assert.Equal(t, "1.2.3", records[0].Tag)
	assert.True(t, records[0].HasUpdate)
	assert.Equal(t, moduletypes.UpdateTypeTag, records[0].UpdateType)
	assert.Equal(t, "1.2.3", records[0].CurrentVersion)
	assert.Equal(t, &latest, records[0].LatestVersion)
	assert.Equal(t, &currentDigest, records[0].CurrentDigest)
	assert.Equal(t, &latestDigest, records[0].LatestDigest)
	assert.Equal(t, &lastError, records[0].LastError)
}

func TestUpdaterService_RecordUpdateRunAdapterInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.AutoUpdateRecord{}))
	svc := NewUpdaterService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	err := svc.RecordUpdateRun(ctx, moduletypes.ResourceResult{
		ResourceID:      "container-1",
		ResourceName:    "web",
		ResourceType:    moduletypes.ResourceTypeContainer,
		Status:          moduletypes.StatusUpdated,
		UpdateAvailable: true,
		UpdateApplied:   true,
		OldImages:       map[string]string{"main": "nginx:1.2.3"},
		NewImages:       map[string]string{"main": "nginx:1.2.4"},
		Details:         map[string]any{"source": "test"},
	})

	require.NoError(t, err)
	var record models.AutoUpdateRecord
	require.NoError(t, db.First(&record, "resource_id = ?", "container-1").Error)
	assert.Equal(t, "web", record.ResourceName)
	assert.Equal(t, "container", record.ResourceType)
	assert.Equal(t, models.AutoUpdateStatus(moduletypes.StatusUpdated), record.Status)
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
		projects: map[string]moduletypes.ComposeProject{
			"proj-fail":    {ID: "project-fail", Name: "proj-fail"},
			"proj-success": {ID: "project-success", Name: "proj-success"},
		},
		updateErrs: map[string]error{
			"project-fail": errors.New("pull updated service images: unauthorized"),
		},
	}

	svc := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.engine = moduleapi.NewService(moduleapi.Config{
		DockerClientProvider: dockerProvider,
		ImagePuller:          puller,
		PendingStore: moduleapi.NewMemoryPendingStore(
			moduletypes.ImageUpdateRecord{
				ID:             oldImageIDFailed,
				Repository:     "registry.example.com/team/fail",
				Tag:            "1.0.0",
				HasUpdate:      true,
				UpdateType:     moduletypes.UpdateTypeTag,
				CurrentVersion: "1.0.0",
				LatestVersion:  ptr("1.0.1"),
			},
			moduletypes.ImageUpdateRecord{
				ID:             oldImageIDUpdated,
				Repository:     "registry.example.com/team/success",
				Tag:            "2.0.0",
				HasUpdate:      true,
				UpdateType:     moduletypes.UpdateTypeTag,
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
		LabelPolicy: updaterlabels.DefaultLabelPolicy(),
	})

	result, err := svc.ApplyPending(ctx, false)
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

	assert.Equal(t, moduletypes.StatusFailed, statusByResource["container-fail"])
	assert.Contains(t, errorByResource["container-fail"], "project-level update failed")
	assert.Equal(t, moduletypes.StatusUpdated, statusByResource["container-success"])

	var recordedStatuses []string
	for _, recorded := range recorder.results {
		recordedStatuses = append(recordedStatuses, recorded.Status)
	}
	assert.Contains(t, recordedStatuses, moduletypes.StatusFailed)
	assert.Contains(t, recordedStatuses, moduletypes.StatusUpdated)
}

func TestResolvePullableImageRefInternal(t *testing.T) {
	tests := []struct {
		name           string
		summaryImage   string
		inspectImage   string
		repoTags       []string
		expectedRef    string
		expectedSource string
	}{
		{
			name:           "inspect config image wins",
			summaryImage:   "nginx:latest",
			inspectImage:   "registry.example.com/nginx:stable",
			expectedRef:    "registry.example.com/nginx:stable",
			expectedSource: "container_inspect_config",
		},
		{
			name:           "summary image used when inspect is image ID",
			summaryImage:   "redis:7",
			inspectImage:   "sha256:abcdef",
			expectedRef:    "redis:7",
			expectedSource: "container_summary",
		},
		{
			name:           "repo tag fallback skips none tag",
			summaryImage:   "sha256:abcdef",
			inspectImage:   "sha256:abcdef",
			repoTags:       []string{"<none>:<none>", "postgres:16"},
			expectedRef:    "postgres:16",
			expectedSource: "image_repo_tag",
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
			gotRef, gotSource := moduleapi.ResolvePullableImageRef(tt.summaryImage, tt.inspectImage, tt.repoTags)
			assert.Equal(t, tt.expectedRef, gotRef)
			assert.Equal(t, tt.expectedSource, gotSource)
		})
	}
}
