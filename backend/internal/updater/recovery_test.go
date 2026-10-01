package updater

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/moby/moby/api/types/container"
	image "github.com/moby/moby/api/types/image"
	"github.com/stretchr/testify/require"
)

func TestFrozenTargetsPreserveAllSelectedContainers(t *testing.T) {
	db := setupProjectTestDBInternal(t)
	digest := "sha256:desired"
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{ID: "shared-image", Repository: "registry.example.com/app", Tag: "latest", HasUpdate: true, LatestDigest: &digest}).Error)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			require.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: "a", Names: []string{"/a"}, Image: "registry.example.com/app:latest"}, {ID: "b", Names: []string{"/b"}, Image: "registry.example.com/app:latest"}}))
			return
		}
		for _, id := range []string{"a", "b"} {
			if strings.HasSuffix(r.URL.Path, "/containers/"+id+"/json") {
				require.NoError(t, json.MarshalWrite(w, container.InspectResponse{ID: id, Name: "/" + id, Image: "sha256:old", Config: &container.Config{Image: "registry.example.com/app:latest"}, State: &container.State{StartedAt: "baseline"}}))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	svc, err := NewUpdaterService(db, nil, (&docker.DockerClientService{}).WithClient(newTestDockerClientInternal(t, server)), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	var evidence []byte
	ctx := jobcontext.WithExecution(t.Context(), schedulertypes.Run{ID: "run"}, func(target schedulertypes.TargetOutcome) error {
		if target.ID == "auto-update" {
			evidence = target.RecoveryData
		}
		return nil
	})
	frozenCtx, err := svc.freezePendingInternal(ctx)
	require.NoError(t, err)
	plan := frozenCtx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal)
	require.Len(t, plan.Records, 2)
	require.Equal(t, "a", plan.Records[0].ContainerID)
	require.Equal(t, "b", plan.Records[1].ContainerID)
	require.NotEmpty(t, evidence)
	require.Equal(t, digest, plan.Targets[1].DesiredDigest)
}

func TestFrozenTargetConfirmsReplacementAndRejectsUnknownEffect(t *testing.T) {
	for _, test := range []struct {
		name, id, image      string
		confirmed, unchanged bool
	}{
		{"replacement", "new", "sha256:desired", true, false},
		{"unchanged", "old", "sha256:baseline", false, true},
		{"unknown replacement", "new", "sha256:other", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					require.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: test.id, Names: []string{"/app"}}}))
				case strings.Contains(r.URL.Path, "/images/"):
					require.NoError(t, json.MarshalWrite(w, image.InspectResponse{ID: "sha256:desired"}))
				case strings.Contains(r.URL.Path, "/containers/"):
					require.NoError(t, json.MarshalWrite(w, container.InspectResponse{ID: test.id, Name: "/app", Image: test.image, Config: &container.Config{}, State: &container.State{StartedAt: "baseline"}}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			svc, err := NewUpdaterService(nil, nil, (&docker.DockerClientService{}).WithClient(newTestDockerClientInternal(t, server)), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			require.NoError(t, err)
			confirmed, unchanged, err := svc.confirmFrozenTargetInternal(t.Context(), arcaneupdater.FrozenUpdateTarget{ContainerID: "old", ContainerName: "app", BaselineImageID: "sha256:baseline", BaselineStartedAt: "baseline", DesiredImageRef: "registry.example.com/app:latest", DesiredDigest: "sha256:desired"})
			require.NoError(t, err)
			require.Equal(t, test.confirmed, confirmed)
			require.Equal(t, test.unchanged, unchanged)
		})
	}
}

func TestSingleUpdateRepairsAcceptedIntentBeforeDispatch(t *testing.T) {
	svc, err := NewUpdaterService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	runtime := francistest.New(t)
	require.NoError(t, svc.RegisterActors(runtime))
	francistest.Start(t, runtime)
	command := arcaneupdater.SingleUpdateCommand{ContainerID: "missing", ActivityID: "accepted"}
	require.NoError(t, runtime.Service().SetState(t.Context(), singleUpdateStateTypeInternal, command.ActivityID, arcaneupdater.SingleUpdateState{Command: command, Status: "queued"}, nil))
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Stop(ctx))
	})
	require.Eventually(t, func() bool {
		var state arcaneupdater.SingleUpdateState
		return runtime.Service().GetState(t.Context(), singleUpdateStateTypeInternal, command.ActivityID, &state) == nil && state.Status == "needs_attention"
	}, 3*time.Second, 10*time.Millisecond)
}

func TestSingleUpdatePersistsSuccessfulActivity(t *testing.T) {
	db := setupProjectTestDBInternal(t)
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}, &AutoUpdateRecord{}))
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	immutableRef := "registry.example.com/app@sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: "app", Names: []string{"/app"}, Image: immutableRef, State: "running"}}))
		case strings.HasSuffix(r.URL.Path, "/containers/app/json"):
			require.NoError(t, json.MarshalWrite(w, container.InspectResponse{ID: "app", Name: "/app", Image: "sha256:baseline", Config: &container.Config{Image: immutableRef}, State: &container.State{StartedAt: "baseline", Running: true}}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	activityService := activity.NewActivityService(db, nil)
	svc, err := NewUpdaterService(db, nil, (&docker.DockerClientService{}).WithClient(newTestDockerClientInternal(t, server)), nil, nil, nil, nil, nil, nil, nil, activityService, nil, nil, nil, nil)
	require.NoError(t, err)
	runtime := francistest.New(t)
	require.NoError(t, svc.RegisterActors(runtime))
	francistest.Start(t, runtime)
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Stop(ctx))
	})
	accepted, err := svc.AcceptSingleContainerUpdate(t.Context(), "app")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var state arcaneupdater.SingleUpdateState
		if runtime.Service().GetState(t.Context(), singleUpdateStateTypeInternal, accepted.ID, &state) != nil || state.Status != "completed" {
			return false
		}
		detail, err := activityService.GetActivityDetail(t.Context(), "0", accepted.ID, 1)
		return err == nil && detail.Activity.Status == activitytypes.StatusSuccess
	}, 3*time.Second, 10*time.Millisecond)
	detail, err := activityService.GetActivityDetail(t.Context(), "0", accepted.ID, 1)
	require.NoError(t, err)
	require.Equal(t, activitytypes.StatusSuccess, detail.Activity.Status)
}
