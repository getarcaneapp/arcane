package transfer

import (
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"slices"
	"strconv"
	"sync"
	"testing"
	"uuid"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/types/v3"
)

type (
	fakeUpload struct {
		session types.UploadSession
		chunks  map[int][]byte
	}

	fakeEnvironment struct {
		projects      map[string]types.Project
		uploads       map[string]*fakeUpload
		volumes       map[string][]byte
		restored      map[string]types.ComposeVolumeTarget
		failDeploy    error
		failImport    error
		onRestore     func()
		archive       []byte
		calls         []string
		volumeWrites  []int
		uploadCounter int
		dropChunk     bool
		mu            sync.Mutex
	}

	transferCase struct {
		request        types.ProjectTransferRequest
		source         types.Project
		targetProjects []types.Project
		failDeploy     error
		failImport     error
		onRestore      func()
		resume         *checkpoint
		verify         func(t *testing.T, source, target *fakeEnvironment, webhooks []types.ProjectWebhookRetarget)
		name           string
		wantStatus     types.RunStatus
		wantMessage    string
		blockedCodes   []string
		denied         []string
		webhooks       int64
		dropChunk      bool
		forbidden      bool
	}
)

const testChunkSize = 4

func newFakeEnvironment(tc transferCase, seeded ...types.Project) *fakeEnvironment {
	f := &fakeEnvironment{
		projects:   map[string]types.Project{},
		uploads:    map[string]*fakeUpload{},
		volumes:    map[string][]byte{},
		restored:   map[string]types.ComposeVolumeTarget{},
		failDeploy: tc.failDeploy,
		failImport: tc.failImport,
		onRestore:  tc.onRestore,
		archive:    []byte("compose-archive"),
		dropChunk:  tc.dropChunk,
	}
	for _, project := range seeded {
		f.projects[project.ID] = project
		for _, config := range project.Volumes {
			if !config.External {
				f.volumes[config.Name] = []byte("volume-data-bytes")
			}
		}
	}
	return f
}

func (f *fakeEnvironment) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeEnvironment) Availability(context.Context) Availability {
	return AvailabilityReady
}

func (f *fakeEnvironment) Project(_ context.Context, projectID string) (types.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[projectID]
	if !ok {
		return types.Project{}, errors.New("project not found")
	}
	return project, nil
}

func (f *fakeEnvironment) Projects(context.Context, string) ([]types.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Collect(maps.Values(f.projects)), nil
}

func (f *fakeEnvironment) VolumeExists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.volumes[name]
	return ok, nil
}

func (f *fakeEnvironment) NetworkExists(context.Context, string) (bool, error) {
	return false, nil
}

func (f *fakeEnvironment) store(kind string, data []byte) types.UploadSession {
	f.uploadCounter++
	chunks := map[int][]byte{}
	for index := 0; index*testChunkSize < len(data); index++ {
		chunks[index] = data[index*testChunkSize : min((index+1)*testChunkSize, len(data))]
	}
	session := types.UploadSession{
		ID:             kind + "-" + strconv.Itoa(f.uploadCounter),
		Kind:           kind,
		Filename:       kind + ".tar.gz",
		ReceivedChunks: slices.Sorted(maps.Keys(chunks)),
		Size:           int64(len(data)),
		ChunkSize:      testChunkSize,
		TotalChunks:    len(chunks),
		Complete:       true,
	}
	f.uploads[session.ID] = &fakeUpload{session: session, chunks: chunks}
	return session
}

func (f *fakeEnvironment) consume(uploadID string) ([]byte, error) {
	upload, ok := f.uploads[uploadID]
	if !ok || len(upload.chunks) != upload.session.TotalChunks {
		return nil, errors.New("upload incomplete")
	}
	var data []byte
	for index := range upload.session.TotalChunks {
		data = append(data, upload.chunks[index]...)
	}
	delete(f.uploads, uploadID)
	return data, nil
}

func (f *fakeEnvironment) ExportProject(context.Context, string) (types.UploadSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "export-project")
	return f.store(types.UploadKindProjectArchive, f.archive), nil
}

func (f *fakeEnvironment) ImportProject(_ context.Context, request types.ImportProjectArchive, _ types.Actor) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := f.consume(request.UploadID)
	if err != nil {
		return "", err
	}
	f.calls = append(f.calls, "import:"+string(data))
	f.projects["imported"] = types.Project{
		ID:          "imported",
		Name:        request.Name,
		ComposeName: request.Name,
		Volumes: composetypes.Volumes{"data": {
			Name:   request.Name + "_data",
			Driver: "local",
			Labels: composetypes.Labels{"tier": "db"},
		}},
	}
	return "imported", f.failImport
}

func (f *fakeEnvironment) CreateUpload(_ context.Context, kind string, request types.CreateUploadSessionRequest) (types.UploadSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploadCounter++
	session := types.UploadSession{
		ID:             "target-" + strconv.Itoa(f.uploadCounter),
		Kind:           kind,
		Filename:       request.Filename,
		ReceivedChunks: []int{},
		Size:           request.Size,
		ChunkSize:      request.ChunkSize,
		TotalChunks:    int((request.Size + request.ChunkSize - 1) / request.ChunkSize),
	}
	f.uploads[session.ID] = &fakeUpload{session: session, chunks: map[int][]byte{}}
	return session, nil
}

func (f *fakeEnvironment) Upload(_ context.Context, _, uploadID string) (types.UploadSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	upload, ok := f.uploads[uploadID]
	if !ok {
		return types.UploadSession{}, errors.New("upload not found")
	}
	session := upload.session
	session.ReceivedChunks = slices.Sorted(maps.Keys(upload.chunks))
	session.Complete = len(upload.chunks) == session.TotalChunks
	return session, nil
}

func (f *fakeEnvironment) ReadChunk(_ context.Context, _, uploadID string, index int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.uploads[uploadID].chunks[index]), nil
}

func (f *fakeEnvironment) WriteChunk(_ context.Context, kind, uploadID string, index int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[uploadID].chunks[index] = data
	if kind != types.UploadKindVolumeBackup {
		return nil
	}
	f.volumeWrites = append(f.volumeWrites, index)
	if index == 2 && f.dropChunk {
		f.dropChunk = false
		return errors.New("tunnel dropped the response")
	}
	return nil
}

func (f *fakeEnvironment) DeleteUpload(_ context.Context, _, uploadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.uploads, uploadID)
	return nil
}

func (f *fakeEnvironment) ExportVolume(_ context.Context, name string) (types.UploadSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "export-volume:"+name)
	return f.store(types.UploadKindVolumeBackup, f.volumes[name]), nil
}

func (f *fakeEnvironment) RestoreVolume(ctx context.Context, name string, request types.RestoreVolumeUploadRequest, _ types.Actor) error {
	if f.onRestore != nil {
		f.onRestore()
		<-ctx.Done()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.volumes[name]; exists {
		return common.Classify(common.ErrConflict, errors.New("volume already exists"))
	}
	data, err := f.consume(request.UploadID)
	if err != nil {
		return err
	}
	f.calls = append(f.calls, "restore:"+name)
	f.volumes[name] = data
	f.restored[name] = *request.ComposeVolume
	return nil
}

func (f *fakeEnvironment) DeployProject(_ context.Context, projectID string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "deploy:"+projectID)
	for _, config := range f.projects[projectID].Volumes {
		if _, exists := f.volumes[config.Name]; !exists && !bool(config.External) {
			f.volumes[config.Name] = []byte{}
		}
	}
	if f.failDeploy != nil {
		return f.failDeploy
	}
	project := f.projects[projectID]
	project.RunningCount = 1
	f.projects[projectID] = project
	return nil
}

func (f *fakeEnvironment) DownProject(_ context.Context, projectID string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "down:"+projectID)
	project := f.projects[projectID]
	project.RunningCount = 0
	f.projects[projectID] = project
	return nil
}

func (f *fakeEnvironment) DestroyProject(_ context.Context, projectID string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "destroy:"+projectID)
	delete(f.projects, projectID)
	return nil
}

func (f *fakeEnvironment) DeleteVolume(_ context.Context, name string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete-volume:"+name)
	delete(f.volumes, name)
	return nil
}

func TestProjectTransfer(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:project-transfer?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	activities := activity.NewActivityService(&database.DB{DB: db}, nil)
	harness := flowtest.New(t, activities)
	admission := runs.NewAdmission(harness.Runtime.Service(), t.Name())
	require.NoError(t, admission.Register(harness.Runtime))
	sides := map[string]*fakeEnvironment{}
	activityIDs := make(chan string, 1)
	var webhooks []types.ProjectWebhookRetarget
	var denied []string
	var webhookCount int64
	var mu sync.Mutex
	service := NewService(Dependencies{
		Environment: func(environmentID string) Environment {
			mu.Lock()
			defer mu.Unlock()
			return sides[environmentID]
		},
		RetargetWebhooks: func(_ context.Context, retarget types.ProjectWebhookRetarget) (int64, error) {
			mu.Lock()
			defer mu.Unlock()
			webhooks = append(webhooks, retarget)
			return webhookCount, nil
		},
		CountWebhooks: func(context.Context, string, string) (int64, error) {
			mu.Lock()
			defer mu.Unlock()
			return webhookCount, nil
		},
		Activities: activities,
		Backups: backup.NewEngine(admission, nil, func(_ context.Context, requester types.BackupRequester) error {
			mu.Lock()
			defer mu.Unlock()
			if slices.Contains(denied, requester.Permission) {
				return errors.New("permission denied")
			}
			return nil
		}),
	})
	require.NoError(t, service.RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)

	plainProject := types.Project{ID: "plain", Name: "app", RunningCount: 1}
	volumeProject := types.Project{
		ID:           "volume",
		Name:         "app",
		RunningCount: 1,
		Volumes:      composetypes.Volumes{"data": {Name: "app_data"}},
	}
	tests := []transferCase{
		{
			name:    "copy without volumes",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy"},
			source:  plainProject,
			verify: func(t *testing.T, source, target *fakeEnvironment, webhooks []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"export-project"}, source.callList())
				require.Equal(t, []string{"import:compose-archive"}, target.callList())
				require.Empty(t, source.uploads)
				require.Empty(t, target.uploads)
				require.Empty(t, webhooks)
			},
			wantStatus: types.RunStatusSucceeded,
		},
		{
			name:      "copy with a volume retries a dropped chunk",
			request:   types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", IncludeVolumes: true},
			source:    volumeProject,
			dropChunk: true,
			verify: func(t *testing.T, source, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"export-project", "down:volume", "export-volume:app_data", "deploy:volume"}, source.callList())
				require.Equal(t, []int{0, 1, 2, 2, 3, 4}, target.volumeWrites)
				require.Equal(t, []byte("volume-data-bytes"), target.volumes["copy_data"])
				require.Equal(t, types.ComposeVolumeTarget{
					Labels:  map[string]string{"tier": "db"},
					Project: "copy",
					Key:     "data",
					Driver:  "local",
				}, target.restored["copy_data"])
				require.Empty(t, source.uploads)
				require.Empty(t, target.uploads)
			},
			wantStatus: types.RunStatusSucceeded,
		},
		{
			name:    "move re-points webhooks and stops the source last",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeMove, Name: "moved", Start: true},
			source:  plainProject,
			verify: func(t *testing.T, source, target *fakeEnvironment, webhooks []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"export-project", "down:plain"}, source.callList())
				require.Equal(t, []string{"import:compose-archive", "deploy:imported"}, target.callList())
				require.Equal(t, []types.ProjectWebhookRetarget{{
					SourceEnvironmentID: "0",
					SourceProjectID:     "plain",
					TargetEnvironmentID: "remote",
					TargetProjectID:     "imported",
				}}, webhooks)
				require.Contains(t, source.projects, "plain")
			},
			webhooks:   1,
			wantStatus: types.RunStatusSucceeded,
		},
		{
			name:    "move without webhooks needs no webhook permission",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeMove, Name: "moved"},
			source:  plainProject,
			denied:  []string{"webhooks:update", "webhooks:create"},
			verify: func(t *testing.T, source, target *fakeEnvironment, webhooks []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"export-project", "down:plain"}, source.callList())
				require.Equal(t, []string{"import:compose-archive"}, target.callList())
				require.Empty(t, webhooks)
			},
			wantStatus:  types.RunStatusSucceeded,
			wantMessage: "0 webhook(s) re-pointed",
		},
		{
			name:      "move with webhooks needs webhook permission",
			request:   types.ProjectTransferRequest{Mode: types.ProjectTransferModeMove, Name: "moved"},
			source:    plainProject,
			denied:    []string{"webhooks:update"},
			webhooks:  1,
			forbidden: true,
		},
		{
			name:      "missing target read permission is rejected",
			request:   types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy"},
			source:    plainProject,
			denied:    []string{"volumes:read"},
			forbidden: true,
		},
		{
			name:       "failed import that still created the project removes it",
			request:    types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy"},
			source:     plainProject,
			failImport: errors.New("tunnel dropped the response"),
			verify: func(t *testing.T, _, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"import:compose-archive", "destroy:imported"}, target.callList())
				require.Empty(t, target.projects)
			},
			wantStatus:  types.RunStatusFailed,
			wantMessage: "Everything this transfer created on the target environment was removed.",
		},
		{
			name:    "resumed volume restore replaces the partly restored volume",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", IncludeVolumes: true},
			source:  volumeProject,
			targetProjects: []types.Project{{
				ID:          "imported",
				Name:        "copy",
				ComposeName: "copy",
				Volumes:     composetypes.Volumes{"data": {Name: "copy_data"}},
			}},
			resume: &checkpoint{
				Volumes: []types.ProjectTransferVolume{{
					Key:        "data",
					SourceName: "app_data",
					TargetName: "copy_data",
				}},
				CreatedVolumes:  []string{"copy_data"},
				TargetProjectID: "imported",
				Prepared:        true,
				Importing:       true,
				SourceRunning:   true,
				SourceStopped:   true,
			},
			verify: func(t *testing.T, source, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"down:volume", "export-volume:app_data", "deploy:volume"}, source.callList())
				require.Equal(t, []string{"delete-volume:copy_data", "restore:copy_data"}, target.callList())
				require.Equal(t, []byte("volume-data-bytes"), target.volumes["copy_data"])
			},
			wantStatus: types.RunStatusSucceeded,
		},
		{
			name:       "failed start removes the volumes it created",
			request:    types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", Start: true},
			source:     volumeProject,
			failDeploy: errors.New("image pull failed"),
			verify: func(t *testing.T, _, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"import:compose-archive", "deploy:imported", "destroy:imported", "delete-volume:copy_data"}, target.callList())
				require.Empty(t, target.volumes)
			},
			wantStatus:  types.RunStatusFailed,
			wantMessage: "Everything this transfer created on the target environment was removed.",
		},
		{
			name:       "failed start keeps a target volume that existed before",
			request:    types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", Start: true},
			source:     volumeProject,
			failDeploy: errors.New("image pull failed"),
			targetProjects: []types.Project{{
				ID:      "other",
				Name:    "other",
				Volumes: composetypes.Volumes{"data": {Name: "copy_data"}},
			}},
			verify: func(t *testing.T, _, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"import:compose-archive", "deploy:imported", "destroy:imported"}, target.callList())
				require.Equal(t, []byte("volume-data-bytes"), target.volumes["copy_data"])
			},
			wantStatus: types.RunStatusFailed,
		},
		{
			name:       "target deploy failure cleans up the target and restarts the source",
			request:    types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", IncludeVolumes: true, Start: true},
			source:     volumeProject,
			failDeploy: errors.New("image pull failed"),
			verify: func(t *testing.T, source, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"export-project", "down:volume", "export-volume:app_data", "deploy:volume"}, source.callList())
				require.Equal(t, []string{"import:compose-archive", "restore:copy_data", "deploy:imported", "destroy:imported", "delete-volume:copy_data"}, target.callList())
				require.Empty(t, target.projects)
				require.Empty(t, target.volumes)
				require.Equal(t, 1, source.projects["volume"].RunningCount)
			},
			wantStatus:  types.RunStatusFailed,
			wantMessage: "Everything this transfer created on the target environment was removed.",
		},
		{
			name:    "cancel after the volumes were copied restarts the source and cleans the target",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", IncludeVolumes: true},
			source:  volumeProject,
			onRestore: func() {
				activities.RequestCancel(<-activityIDs)
			},
			verify: func(t *testing.T, source, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"export-project", "down:volume", "export-volume:app_data", "deploy:volume"}, source.callList())
				require.Equal(t, []string{"import:compose-archive", "restore:copy_data", "destroy:imported", "delete-volume:copy_data"}, target.callList())
				require.Empty(t, target.projects)
				require.Empty(t, target.volumes)
				require.Equal(t, 1, source.projects["volume"].RunningCount)
			},
			wantStatus: types.RunStatusCanceled,
		},
		{
			name:    "blocking checks",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeMove, Name: "copy", IncludeVolumes: true},
			source: types.Project{
				ID:              "blocked",
				Name:            "app",
				GitOpsManagedBy: new("sync-1"),
				Volumes:         composetypes.Volumes{"data": {Name: "app_data"}},
				Networks:        composetypes.Networks{"edge": {Name: "proxy", External: true}},
			},
			targetProjects: []types.Project{{
				ID:      "other",
				Name:    "Copy",
				Volumes: composetypes.Volumes{"data": {Name: "copy_data"}},
			}},
			blockedCodes: []string{
				types.ProjectTransferGitOpsMove,
				types.ProjectTransferNameConflict,
				types.ProjectTransferVolumeExists,
				types.ProjectTransferNetworkMissing,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, target := newFakeEnvironment(transferCase{}, tc.source), newFakeEnvironment(tc, tc.targetProjects...)
			mu.Lock()
			sides["0"], sides["remote"] = source, target
			webhooks, denied, webhookCount = nil, tc.denied, tc.webhooks
			mu.Unlock()
			request := tc.request
			request.TargetEnvironmentID = "remote"
			opts := Options{
				User:                types.Actor{ID: "user-1", Username: "admin"},
				SourceEnvironmentID: "0",
				ProjectID:           tc.source.ID,
				Request:             request,
			}

			if tc.resume != nil {
				recovery, err := json.Marshal(tc.resume)
				require.NoError(t, err)
				activityID, err := service.flow.Submit(t.Context(), service.workflow, runInput{
					User:                opts.User,
					Requesters:          requesters(t.Context(), opts, false),
					RunID:               uuid.New().String(),
					SourceEnvironmentID: opts.SourceEnvironmentID,
					ProjectID:           opts.ProjectID,
					Request:             request,
				}, activitylib.StartRequest{EnvironmentID: opts.SourceEnvironmentID}, types.TargetOutcome{
					ResourceType: "project_transfer",
					ID:           checkpointID,
					Status:       types.RunStatusRunning,
					RecoveryData: recovery,
				})
				require.NoError(t, err)
				outcome, err := service.flow.Wait(t.Context(), service.workflow, activityID)
				require.NoError(t, err)
				require.Equal(t, tc.wantStatus, outcome.Status, outcome.Message)
				tc.verify(t, source, target, nil)
				return
			}

			plan, err := service.Preflight(t.Context(), opts)
			if tc.forbidden {
				require.ErrorIs(t, err, common.ErrForbidden)
				_, startErr := service.Start(t.Context(), opts)
				require.ErrorIs(t, startErr, common.ErrForbidden)
				require.Empty(t, source.callList())
				require.Empty(t, target.callList())
				return
			}
			require.NoError(t, err)
			if len(tc.blockedCodes) > 0 {
				require.False(t, plan.Allowed)
				var codes []string
				for _, check := range plan.Checks {
					if check.Severity == types.ProjectTransferSeverityError {
						codes = append(codes, check.Code)
					}
				}
				require.Equal(t, tc.blockedCodes, codes)
				_, startErr := service.Start(t.Context(), opts)
				require.Error(t, startErr)
				require.Empty(t, source.callList())
				require.Empty(t, target.callList())
				return
			}
			require.True(t, plan.Allowed)

			started, err := service.Start(t.Context(), opts)
			require.NoError(t, err)
			if tc.onRestore != nil {
				activityIDs <- started.ActivityID
			}
			outcome, err := service.flow.Wait(t.Context(), service.workflow, started.ActivityID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, outcome.Status, outcome.Message)
			require.Contains(t, outcome.Message, tc.wantMessage)
			mu.Lock()
			recorded := slices.Clone(webhooks)
			mu.Unlock()
			tc.verify(t, source, target, recorded)
		})
	}
}
