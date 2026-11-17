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
	"time"
	"uuid"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

type (
	fakeUpload struct {
		chunks  map[int][]byte
		session types.UploadSession
	}

	fakeEnvironment struct {
		projects      map[string]types.Project
		uploads       map[string]*fakeUpload
		volumes       map[string][]byte
		volumeOwners  map[string]string
		restored      map[string]types.ComposeVolumeTarget
		external      map[string]types.Volume
		networks      map[string]types.NetworkInspect
		failDeploy    error
		failImport    error
		raceImport    *types.Project
		onRestore     func()
		archive       []byte
		importedID    string
		calls         []string
		volumeWrites  []int
		uploadCounter int
		dropChunk     bool
		mu            sync.Mutex
	}

	transferCase struct {
		failDeploy     error
		failImport     error
		resume         *checkpoint
		onRestore      func()
		verify         func(t *testing.T, source, target *fakeEnvironment, webhooks []types.ProjectWebhookRetarget)
		raceImport     *types.Project
		name           string
		wantStatus     types.RunStatus
		wantMessage    string
		request        types.ProjectTransferRequest
		targetProjects []types.Project
		blockedCodes   []string
		denied         []string
		source         types.Project
		webhooks       int64
		dropChunk      bool
		forbidden      bool
	}
)

const testChunkSize = 4

func newFakeEnvironment(tc transferCase, seeded ...types.Project) *fakeEnvironment {
	f := &fakeEnvironment{
		projects:     map[string]types.Project{},
		uploads:      map[string]*fakeUpload{},
		volumes:      map[string][]byte{},
		volumeOwners: map[string]string{},
		restored:     map[string]types.ComposeVolumeTarget{},
		external:     map[string]types.Volume{},
		networks:     map[string]types.NetworkInspect{},
		failDeploy:   tc.failDeploy,
		failImport:   tc.failImport,
		raceImport:   tc.raceImport,
		onRestore:    tc.onRestore,
		archive:      []byte("compose-archive"),
		dropChunk:    tc.dropChunk,
	}
	for _, project := range seeded {
		f.projects[project.ID] = project
		for _, config := range project.Volumes {
			if !config.External {
				f.volumes[config.Name] = []byte("volume-data-bytes")
				f.volumeOwners[config.Name] = project.ComposeName
			}
		}
	}
	return f
}

// label names the project a transfer imported "imported", whatever ID the run chose for it.
func (f *fakeEnvironment) label(projectID string) string {
	if projectID == f.importedID {
		return "imported"
	}
	return projectID
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

func (f *fakeEnvironment) Volume(_ context.Context, name string) (types.Volume, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if volume, ok := f.external[name]; ok {
		return volume, true, nil
	}
	_, ok := f.volumes[name]
	return types.Volume{
		Name:   name,
		Labels: map[string]string{docker.ComposeProjectLabelKey: f.volumeOwners[name]},
	}, ok, nil
}

func (f *fakeEnvironment) Network(_ context.Context, name string) (types.NetworkInspect, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	network, ok := f.networks[name]
	return network, ok, nil
}

func (f *fakeEnvironment) CreateVolume(_ context.Context, volume types.Volume, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create-volume:"+volume.Name)
	f.external[volume.Name] = volume
	return nil
}

func (f *fakeEnvironment) CreateNetwork(_ context.Context, network types.Network, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create-network:"+network.Name)
	f.networks[network.Name] = types.NetworkInspect{
		Name:       network.Name,
		Driver:     network.Options.Driver,
		IPAM:       kit.FromPtr(network.Options.IPAM),
		Options:    network.Options.Options,
		Labels:     network.Options.Labels,
		EnableIPv6: network.Options.EnableIPv6,
		Internal:   network.Options.Internal,
		Attachable: network.Options.Attachable,
	}
	return nil
}

func (f *fakeEnvironment) ComposeProjectInUse(context.Context, string) (bool, error) {
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
	if f.raceImport != nil {
		f.projects[f.raceImport.ID] = *f.raceImport
		return "", common.Classify(common.ErrConflict, errors.New("project already exists"))
	}
	f.importedID = request.ProjectID
	f.projects[request.ProjectID] = types.Project{
		ID:          request.ProjectID,
		Name:        request.Name,
		ComposeName: request.Name,
		Volumes: composetypes.Volumes{"data": {
			Name:   request.Name + "_data",
			Driver: "local",
			Labels: composetypes.Labels{"tier": "db"},
		}},
	}
	return request.ProjectID, f.failImport
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
	return types.UploadSession{
		ID:             upload.session.ID,
		Kind:           upload.session.Kind,
		Filename:       upload.session.Filename,
		ReceivedChunks: slices.Sorted(maps.Keys(upload.chunks)),
		Size:           upload.session.Size,
		ChunkSize:      upload.session.ChunkSize,
		TotalChunks:    upload.session.TotalChunks,
		Complete:       len(upload.chunks) == upload.session.TotalChunks,
	}, nil
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
	f.volumeOwners[name] = request.ComposeVolume.Project
	f.restored[name] = *request.ComposeVolume
	return nil
}

func (f *fakeEnvironment) DeployProject(_ context.Context, projectID string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "deploy:"+f.label(projectID))
	for _, config := range f.projects[projectID].Volumes {
		if _, exists := f.volumes[config.Name]; !exists && !bool(config.External) {
			f.volumes[config.Name] = []byte{}
			f.volumeOwners[config.Name] = f.projects[projectID].ComposeName
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
	f.calls = append(f.calls, "down:"+f.label(projectID))
	project := f.projects[projectID]
	project.RunningCount = 0
	f.projects[projectID] = project
	return nil
}

func (f *fakeEnvironment) DestroyProject(_ context.Context, projectID string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "destroy:"+f.label(projectID))
	delete(f.projects, projectID)
	return nil
}

func (f *fakeEnvironment) DeleteVolume(_ context.Context, name string, _ types.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "delete-volume:"+name)
	delete(f.volumes, name)
	delete(f.volumeOwners, name)
	return nil
}

func TestProjectTransfer(t *testing.T) {
	db, openErr := gorm.Open(sqlite.Open("file:project-transfer?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, openErr)
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
			if retarget.SourceProjectID != "plain" {
				return 0, nil
			}
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

	plainProject := types.Project{ID: "plain", Name: "app", ComposeName: "app", RunningCount: 1}
	volumeProject := types.Project{
		ID:           "volume",
		Name:         "app",
		ComposeName:  "app",
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
					TargetProjectID:     target.importedID,
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
				require.Eventually(t, func() bool {
					return slices.Equal([]string{"import:compose-archive", "restore:copy_data", "destroy:imported", "delete-volume:copy_data"}, target.callList())
				}, time.Second, 10*time.Millisecond)
				require.Equal(t, []string{"export-project", "down:volume", "export-volume:app_data", "deploy:volume"}, source.callList())
				require.Empty(t, target.projects)
				require.Empty(t, target.volumes)
				require.Equal(t, 1, source.projects["volume"].RunningCount)
			},
			wantStatus: types.RunStatusCanceled,
		},
		{
			name:       "failed import never removes a project another transfer created",
			request:    types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy"},
			source:     plainProject,
			raceImport: &types.Project{ID: "first", Name: "copy", ComposeName: "copy"},
			verify: func(t *testing.T, _, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"import:compose-archive"}, target.callList())
				require.Contains(t, target.projects, "first")
			},
			wantStatus:  types.RunStatusFailed,
			wantMessage: "if its interrupted import still finishes there, remove it",
		},
		{
			name:    "resumed volume restore keeps a volume this run did not create",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeCopy, Name: "copy", IncludeVolumes: true},
			source:  volumeProject,
			targetProjects: []types.Project{
				{ID: "imported", Name: "copy", ComposeName: "copy", Volumes: composetypes.Volumes{"data": {Name: "copy_data"}}},
				{ID: "other", Name: "other", ComposeName: "other", Volumes: composetypes.Volumes{"data": {Name: "copy_data"}}},
			},
			resume: &checkpoint{
				Volumes:         []types.ProjectTransferVolume{{Key: "data", SourceName: "app_data", TargetName: "copy_data"}},
				CreatedVolumes:  []string{"copy_data"},
				TargetProjectID: "imported",
				Prepared:        true,
				Importing:       true,
				SourceRunning:   true,
				SourceStopped:   true,
			},
			verify: func(t *testing.T, _, target *fakeEnvironment, _ []types.ProjectWebhookRetarget) {
				require.Equal(t, []string{"destroy:imported"}, target.callList())
				require.Equal(t, []byte("volume-data-bytes"), target.volumes["copy_data"])
			},
			wantStatus:  types.RunStatusFailed,
			wantMessage: "created on the target environment by something other than this transfer",
		},
		{
			name:    "failure after re-pointing webhooks points them back before removing the target",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeMove, Name: "copy"},
			source:  plainProject,
			denied:  []string{authz.PermProjectsCreate},
			targetProjects: []types.Project{
				{ID: "imported", Name: "copy", ComposeName: "copy", Volumes: composetypes.Volumes{"data": {Name: "copy_data"}}},
				{ID: "other", Name: "other", ComposeName: "other", Volumes: composetypes.Volumes{"data": {Name: "copy_data"}}},
			},
			resume: &checkpoint{
				CreatedVolumes:  []string{"copy_data"},
				TargetProjectID: "imported",
				TargetCompose:   "copy",
				Prepared:        true,
				Importing:       true,
				SourceRunning:   true,
				SourceStopped:   true,
				Retargeting:     true,
			},
			verify: func(t *testing.T, source, target *fakeEnvironment, webhooks []types.ProjectWebhookRetarget) {
				require.Equal(t, []types.ProjectWebhookRetarget{{
					SourceEnvironmentID: "remote",
					SourceProjectID:     "imported",
					TargetEnvironmentID: "0",
					TargetProjectID:     "plain",
				}}, webhooks)
				require.Equal(t, []string{"deploy:plain"}, source.callList())
				require.Equal(t, []string{"destroy:imported"}, target.callList())
			},
			wantStatus:  types.RunStatusFailed,
			wantMessage: "Webhooks point at the source project again.",
		},
		{
			name:    "blocking checks",
			request: types.ProjectTransferRequest{Mode: types.ProjectTransferModeMove, Name: "copy", IncludeVolumes: true},
			source: types.Project{
				ID:              "blocked",
				Name:            "app",
				ComposeName:     "app",
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
			request := types.ProjectTransferRequest{
				TargetEnvironmentID: "remote",
				Mode:                tc.request.Mode,
				Name:                tc.request.Name,
				IncludeVolumes:      tc.request.IncludeVolumes,
				Start:               tc.request.Start,
			}
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
					TargetProjectID:     tc.resume.TargetProjectID,
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
				require.Contains(t, outcome.Message, tc.wantMessage)
				mu.Lock()
				recorded := slices.Clone(webhooks)
				mu.Unlock()
				tc.verify(t, source, target, recorded)
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

func TestCreateTransferResource(t *testing.T) {
	nfs := types.Volume{
		Name:    "media",
		Driver:  "local",
		Options: map[string]string{"type": "nfs", "o": "addr=10.0.0.5,rw", "device": ":/export/media"},
		Labels:  map[string]string{"tier": "storage"},
	}
	vlan := types.NetworkInspect{
		Name:    "vlan25",
		Driver:  "macvlan",
		Options: map[string]string{"parent": "eth0.25"},
		IPAM: types.NetworkIPAM{
			Driver: "default",
			Config: []types.NetworkIPAMConfig{{Subnet: "192.168.25.0/24", Gateway: "192.168.25.1", IPRange: "192.168.25.128/25"}},
		},
		Labels: map[string]string{"site": "lab"},
	}
	project := types.Project{
		ID:          "app",
		Name:        "app",
		ComposeName: "app",
		Volumes: composetypes.Volumes{
			"media": {Name: "media", External: true},
			"data":  {Name: "app_data"},
		},
		Networks: composetypes.Networks{
			"vlan25": {Name: "vlan25", External: true},
			"ghost":  {Name: "ghost", External: true},
		},
	}
	tests := []struct {
		wantErr  error
		verify   func(t *testing.T, target *fakeEnvironment)
		request  types.ProjectTransferResource
		name     string
		denied   []string
		onTarget bool
	}{
		{
			name:    "volume keeps driver, options, and labels",
			request: types.ProjectTransferResource{Kind: types.ProjectTransferResourceVolume, Name: "media"},
			verify: func(t *testing.T, target *fakeEnvironment) {
				t.Helper()
				require.Equal(t, nfs, target.external["media"])
			},
		},
		{
			name:    "macvlan network keeps parent and IPAM",
			request: types.ProjectTransferResource{Kind: types.ProjectTransferResourceNetwork, Name: "vlan25"},
			verify: func(t *testing.T, target *fakeEnvironment) {
				t.Helper()
				require.Equal(t, vlan, target.networks["vlan25"])
			},
		},
		{
			name:    "rejects a resource the project does not reference as external",
			request: types.ProjectTransferResource{Kind: types.ProjectTransferResourceVolume, Name: "app_data"},
			wantErr: common.ErrBadRequest,
		},
		{
			name:    "rejects a resource missing on the source",
			request: types.ProjectTransferResource{Kind: types.ProjectTransferResourceNetwork, Name: "ghost"},
			wantErr: common.ErrNotFound,
		},
		{
			name:     "rejects a volume that already exists on the target",
			request:  types.ProjectTransferResource{Kind: types.ProjectTransferResourceVolume, Name: "media"},
			onTarget: true,
			wantErr:  common.ErrConflict,
		},
		{
			name:    "requires create permission on the target",
			request: types.ProjectTransferResource{Kind: types.ProjectTransferResourceNetwork, Name: "vlan25"},
			denied:  []string{authz.PermNetworksCreate},
			wantErr: common.ErrForbidden,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, target := newFakeEnvironment(transferCase{}, project), newFakeEnvironment(transferCase{})
			source.external["media"] = nfs
			source.networks["vlan25"] = vlan
			if tc.onTarget {
				target.external["media"] = types.Volume{Name: "media"}
			}
			service := NewService(Dependencies{
				Environment: func(environmentID string) Environment {
					return kit.Ternary(environmentID == "remote", target, source)
				},
				Backups: backup.NewEngine(nil, nil, func(_ context.Context, requester types.BackupRequester) error {
					if slices.Contains(tc.denied, requester.Permission) {
						return errors.New("permission denied")
					}
					return nil
				}),
			})
			err := service.CreateResource(t.Context(), ResourceOptions{
				User:                types.Actor{ID: "user-1", Username: "admin"},
				SourceEnvironmentID: "0",
				ProjectID:           project.ID,
				Request: types.ProjectTransferResource{
					TargetEnvironmentID: "remote",
					Kind:                tc.request.Kind,
					Name:                tc.request.Name,
				},
			})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Empty(t, target.callList())
				return
			}
			require.NoError(t, err)
			tc.verify(t, target)
		})
	}
}
