package policies

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	francistest "getarcane.dev/arcane/v3/pkg/francis/testing"
	"getarcane.dev/types/v3"
)

type recordingRemoteTransport struct {
	environment.RemoteTransport

	runRequests map[string]types.RunGlobalProjectBackupsRequest
	options     map[string][]types.GlobalProjectBackupOption
	failing     string
	mu          sync.Mutex
}

func (t *recordingRemoteTransport) ProxyJSONRequestForEnvironment(_ context.Context, env environment.Environment, method, path string, body []byte, out any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if env.ID == t.failing {
		return errors.New("agent is not connected")
	}
	switch method + " " + path {
	case http.MethodGet + " /api/backups/projects/options":
		payload, err := json.Marshal(types.ApiResponse[[]types.GlobalProjectBackupOption]{
			Success: true,
			Data:    t.options[env.ID],
		})
		if err != nil {
			return err
		}
		return json.Unmarshal(payload, out)
	case http.MethodPost + " /api/backups/projects/run":
		var req types.RunGlobalProjectBackupsRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return err
		}
		t.runRequests[env.ID] = req
		return nil
	default:
		return errors.New("unexpected proxy request: " + method + " " + path)
	}
}

func TestGlobalProjectBackupFansOutToRemoteEnvironments(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&backup.Policy{}, &environment.Environment{}))
	db := &database.DB{DB: gormDB}

	for _, id := range []string{"agent-1", "agent-offline"} {
		require.NoError(t, gormDB.Create(&environment.Environment{
			BaseModel: database.BaseModel{ID: id},
			Name:      id,
			ApiUrl:    "http://" + id,
			Enabled:   true,
		}).Error)
	}
	require.NoError(t, gormDB.Create(&environment.Environment{
		BaseModel: database.BaseModel{ID: "agent-disabled"},
		Name:      "agent-disabled",
		ApiUrl:    "http://agent-disabled",
	}).Error)

	policy := backup.Policy{
		BaseModel:      database.BaseModel{ID: "policy-1"},
		SubjectKind:    backup.PolicyProject,
		SubjectID:      "",
		Schedule:       "0 2 * * *",
		RetentionCount: 5,
		Enabled:        true,
		StopContainers: true,
		LocalEnabled:   true,
		Options: backup.PolicyOptions{
			SelectionMode: types.SystemVolumeSelectionBlocklist,
			ProjectIDs:    []string{"excluded-proj"},
		},
	}
	require.NoError(t, gormDB.Create(&policy).Error)

	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	engine := backup.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error { return nil })
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })

	transport := &recordingRemoteTransport{
		failing:     "agent-offline",
		runRequests: map[string]types.RunGlobalProjectBackupsRequest{},
		options: map[string][]types.GlobalProjectBackupOption{
			"agent-1": {{ID: "remote-proj-1", Name: "remote-app", Available: true}},
		},
	}
	envService := environment.NewEnvironmentService(environment.EnvironmentServiceParams{
		DB:              db,
		RemoteTransport: transport,
	})

	var localBackedUp []string
	svc := NewService(Dependencies{
		Engine:       engine,
		Policies:     backup.NewPolicies(db),
		Config:       &config.Config{LocalDockerEnabled: true},
		Environments: envService,
		ListProjects: func(context.Context) ([]types.ProjectReference, error) {
			return []types.ProjectReference{{ID: "local-proj-1", Name: "local-app"}}, nil
		},
		ProjectVolumes: func(context.Context, string, []string) ([]types.ProjectVolume, error) {
			return nil, nil
		},
		BackUp: func(_ context.Context, run ScheduledBackup) error {
			localBackedUp = append(localBackedUp, run.ProjectID)
			return nil
		},
	})

	options, err := svc.GlobalOptions(t.Context())
	require.NoError(t, err)
	require.Len(t, options, 3) // local-proj-1, remote-proj-1, excluded-proj (unavailable)
	require.Equal(t, "local-proj-1", options[0].ID)
	require.Equal(t, environment.LocalEnvironmentID, options[0].EnvironmentID)
	require.Equal(t, "remote-proj-1", options[1].ID)
	require.Equal(t, "agent-1", options[1].EnvironmentID)

	recorded := map[string]types.TargetOutcome{}
	ctx := jobcontext.WithExecution(t.Context(), types.SchedulerRun{}, func(target types.TargetOutcome) error {
		recorded[target.ID] = target
		return nil
	})
	outcome, err := svc.backUpGlobal(ctx, globalRun{PolicyID: policy.ID}, types.SchedulerRun{})
	require.NoError(t, err)
	require.Equal(t, types.RunStatusPartial, outcome.Status)
	require.Equal(t, []string{"local-proj-1"}, localBackedUp)
	require.Contains(t, transport.runRequests, "agent-1")
	require.Equal(t, policy.ID, transport.runRequests["agent-1"].PolicyID)
	require.NotNil(t, transport.runRequests["agent-1"].Policy)
	require.Equal(t, types.SystemVolumeSelectionBlocklist, transport.runRequests["agent-1"].Policy.SelectionMode)
	require.Equal(t, []string{"excluded-proj"}, transport.runRequests["agent-1"].Policy.ProjectIDs)
	require.Equal(t, types.RunStatusSucceeded, recorded["environment:agent-1"].Status)
	require.Equal(t, types.RunStatusFailed, recorded["environment:agent-offline"].Status)
}
