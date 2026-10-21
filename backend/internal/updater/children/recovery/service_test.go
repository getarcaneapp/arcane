package recovery

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
)

// unresolvedDigests fails every registry digest lookup.
type unresolvedDigests struct{}

func (unresolvedDigests) RemoteDigest(context.Context, string, []types.ContainerRegistryCredential) (string, error) {
	return "", errors.New("registry digest unavailable")
}

var neverDeferred = func(updater.ImageUpdateRecord, container.Summary) bool { return false }

func newTestService(t *testing.T, server *httptest.Server, pending func(context.Context) ([]updater.ImageUpdateRecord, error)) *Service {
	t.Helper()
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"), client.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	return NewService(
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient),
		unresolvedDigests{},
		pending,
		func(context.Context) string { return "" },
		func(ctx context.Context, _ []string) (context.Context, func(), error) {
			return ctx, func() {}, nil
		},
		nil,
	)
}

func TestFrozenTargetsPreserveAllSelectedContainers(t *testing.T) {
	digest := "sha256:desired"
	pending := []updater.ImageUpdateRecord{{
		ID:           "shared-image",
		Repository:   "registry.example.com/app",
		Tag:          "latest",
		HasUpdate:    true,
		LatestDigest: &digest,
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			if !assert.NoError(
				t,
				json.MarshalWrite(
					w,
					[]container.Summary{
						{
							ID:    "a",
							Names: []string{"/a"},
							Image: "registry.example.com/app:latest",
						},
						{
							ID:    "b",
							Names: []string{"/b"},
							Image: "registry.example.com/app:latest",
						},
					},
				),
			) {
				return
			}
			return
		}
		for _, id := range []string{"a", "b"} {
			if strings.HasSuffix(r.URL.Path, "/containers/"+id+"/json") {
				if !assert.NoError(
					t,
					json.MarshalWrite(
						w,
						container.InspectResponse{
							ID:     id,
							Name:   "/" + id,
							Image:  "sha256:old",
							Config: &container.Config{Image: "registry.example.com/app:latest"},
							State:  &container.State{StartedAt: "baseline"},
						},
					),
				) {
					return
				}
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	svc := newTestService(t, server, func(context.Context) ([]updater.ImageUpdateRecord, error) { return pending, nil })
	var evidence []byte
	ctx := jobcontext.WithExecution(t.Context(), types.SchedulerRun{ID: "run"}, func(target types.TargetOutcome) error {
		if target.ID == "auto-update" {
			evidence = target.RecoveryData
		}
		return nil
	})
	frozenCtx, err := svc.FreezePending(ctx, types.SchedulerRun{ID: "run"}, nil, neverDeferred)
	require.NoError(t, err)
	plan := frozenCtx.Value(frozenPendingKey{}).(*frozenUpdatePlan)
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
		{
			name:      "replacement",
			id:        "new",
			image:     "sha256:desired",
			confirmed: true,
		},
		{
			name:      "unchanged",
			id:        "old",
			image:     "sha256:baseline",
			unchanged: true,
		},
		{
			name:  "unknown replacement",
			id:    "new",
			image: "sha256:other",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					if !assert.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: test.id, Names: []string{"/app"}}})) {
						return
					}
				case strings.Contains(r.URL.Path, "/images/"):
					if !assert.NoError(t, json.MarshalWrite(w, image.InspectResponse{ID: "sha256:desired"})) {
						return
					}
				case strings.Contains(r.URL.Path, "/containers/"):
					if !assert.NoError(
						t,
						json.MarshalWrite(
							w,
							container.InspectResponse{
								ID:     test.id,
								Name:   "/app",
								Image:  test.image,
								Config: &container.Config{},
								State:  &container.State{StartedAt: "baseline"},
							},
						),
					) {
						return
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			svc := newTestService(t, server, nil)
			confirmed, unchanged, err := svc.ConfirmTarget(
				t.Context(),
				types.FrozenUpdateTarget{
					ContainerID:       "old",
					ContainerName:     "app",
					BaselineImageID:   "sha256:baseline",
					BaselineStartedAt: "baseline",
					DesiredImageRef:   "registry.example.com/app:latest",
					DesiredDigest:     "sha256:desired",
				},
			)
			require.NoError(t, err)
			require.Equal(t, test.confirmed, confirmed)
			require.Equal(t, test.unchanged, unchanged)
		})
	}
}

func TestFreezePendingExcludesFailedChecksAndIsolatesFreezeFailures(t *testing.T) {
	desired := "sha256:desired"
	pending := []updater.ImageUpdateRecord{
		{
			ID:           "app",
			Repository:   "registry.example.com/app",
			Tag:          "latest",
			HasUpdate:    true,
			LatestDigest: &desired,
		},
		{
			ID:         "unresolvable",
			Repository: "registry.example.com/unresolvable",
			Tag:        "latest",
			HasUpdate:  true,
		},
		{
			ID:           "unchecked",
			Repository:   "registry.example.com/unchecked",
			Tag:          "latest",
			HasUpdate:    true,
			LatestDigest: &desired,
		},
	}
	running := []container.Summary{
		{
			ID:    "app",
			Names: []string{"/app"},
			Image: "registry.example.com/app:latest",
		},
		{
			ID:    "unresolvable",
			Names: []string{"/unresolvable"},
			Image: "registry.example.com/unresolvable:latest",
		},
		{
			ID:    "unchecked",
			Names: []string{"/unchecked"},
			Image: "registry.example.com/unchecked:latest",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			assert.NoError(t, json.MarshalWrite(w, running))
			return
		}
		for _, summary := range running {
			if strings.HasSuffix(r.URL.Path, "/containers/"+summary.ID+"/json") {
				assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
					ID:     summary.ID,
					Name:   summary.Names[0],
					Image:  "sha256:old",
					Config: &container.Config{Image: summary.Image},
					State:  &container.State{StartedAt: "baseline"},
				}))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	svc := newTestService(t, server, func(context.Context) ([]updater.ImageUpdateRecord, error) { return pending, nil })
	targets := map[string]types.TargetOutcome{}
	ctx := jobcontext.WithExecution(t.Context(), types.SchedulerRun{ID: "run"}, func(target types.TargetOutcome) error {
		targets[target.ID] = target
		return nil
	})

	frozenCtx, err := svc.FreezePending(ctx, types.SchedulerRun{ID: "run"}, map[string]bool{"registry.example.com/unchecked:latest": true}, neverDeferred)
	require.NoError(t, err)
	plan := frozenCtx.Value(frozenPendingKey{}).(*frozenUpdatePlan)
	require.Len(t, plan.Targets, 1)
	require.Equal(t, "app", plan.Targets[0].ContainerID)
	require.Equal(t, types.RunStatusQueued, targets["app"].Status)
	require.Equal(t, types.RunStatusFailed, targets["unresolvable"].Status, "a target that cannot be frozen fails alone")
	require.Contains(t, targets["unresolvable"].Message, "Update could not be planned")
	require.NotContains(t, targets, "unchecked", "images whose check failed stay out of the plan")
}

func TestFreezePendingSkipsDeferredCandidatesWithoutFailing(t *testing.T) {
	desired := "sha256:desired"
	pending := []updater.ImageUpdateRecord{
		{
			ID:           "app",
			Repository:   "registry.example.com/app",
			Tag:          "latest",
			HasUpdate:    true,
			LatestDigest: &desired,
		},
		{
			ID:           "young",
			Repository:   "registry.example.com/young",
			Tag:          "latest",
			HasUpdate:    true,
			LatestDigest: &desired,
		},
	}
	running := []container.Summary{
		{
			ID:    "app",
			Names: []string{"/app"},
			Image: "registry.example.com/app:latest",
		},
		{
			ID:     "young",
			Names:  []string{"/young"},
			Image:  "registry.example.com/young:latest",
			Labels: map[string]string{"delay": "true"},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			assert.NoError(t, json.MarshalWrite(w, running))
			return
		}
		for _, summary := range running {
			if strings.HasSuffix(r.URL.Path, "/containers/"+summary.ID+"/json") {
				assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
					ID:     summary.ID,
					Name:   summary.Names[0],
					Image:  "sha256:old",
					Config: &container.Config{Image: summary.Image},
					State:  &container.State{StartedAt: "baseline"},
				}))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	svc := newTestService(t, server, func(context.Context) ([]updater.ImageUpdateRecord, error) { return pending, nil })
	targets := map[string]types.TargetOutcome{}
	ctx := jobcontext.WithExecution(t.Context(), types.SchedulerRun{ID: "run"}, func(target types.TargetOutcome) error {
		targets[target.ID] = target
		return nil
	})

	frozenCtx, err := svc.FreezePending(ctx, types.SchedulerRun{ID: "run"}, nil, func(record updater.ImageUpdateRecord, candidate container.Summary) bool {
		return record.ID == "young" && candidate.Labels["delay"] == "true"
	})
	require.NoError(t, err)
	plan := frozenCtx.Value(frozenPendingKey{}).(*frozenUpdatePlan)
	require.Len(t, plan.Targets, 1)
	require.Equal(t, "app", plan.Targets[0].ContainerID)
	require.Equal(t, types.RunStatusQueued, targets["app"].Status)
	require.NotContains(t, targets, "young", "a deferred candidate is neither planned nor failed")
}

func TestResumePlanConfirmsRedeliveredChangeWithoutReapplying(t *testing.T) {
	target := types.FrozenUpdateTarget{
		ContainerID:       "old",
		ContainerName:     "app",
		BaselineImageID:   "sha256:baseline",
		BaselineStartedAt: "baseline",
		DesiredImageRef:   "registry.example.com/app:latest",
		DesiredDigest:     "sha256:desired",
	}
	raw, err := json.Marshal(frozenUpdatePlan{
		Records: []updater.ImageUpdateRecord{{
			ContainerID: "old",
			ID:          "image",
			Repository:  "registry.example.com/app",
			Tag:         "latest",
			HasUpdate:   true,
		}},
		Targets: []types.FrozenUpdateTarget{target},
	})
	require.NoError(t, err)
	run := types.SchedulerRun{ID: "run", Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{
			ID:           "auto-update",
			ResourceType: "update-batch",
			Status:       types.RunStatusRunning,
			RecoveryData: raw,
		},
		{
			ID:           "old",
			ResourceType: "container",
			Status:       types.RunStatusRunning,
		},
	}}}
	for _, test := range []struct {
		name, id, image string
		remaining       int
		status          types.RunStatus
	}{
		{
			name:   "applied before the redelivery",
			id:     "new",
			image:  "sha256:desired",
			status: types.RunStatusSucceeded,
		},
		{
			name:      "not yet applied",
			id:        "old",
			image:     "sha256:baseline",
			remaining: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					assert.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: test.id, Names: []string{"/app"}}}))
				case strings.Contains(r.URL.Path, "/images/"):
					assert.NoError(t, json.MarshalWrite(w, image.InspectResponse{ID: "sha256:desired"}))
				case strings.Contains(r.URL.Path, "/containers/"):
					assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
						ID:     test.id,
						Name:   "/app",
						Image:  test.image,
						Config: &container.Config{},
						State:  &container.State{StartedAt: "baseline"},
					}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			svc := newTestService(t, server, nil)
			var status types.RunStatus
			ctx := jobcontext.WithExecution(t.Context(), run, func(progress types.TargetOutcome) error {
				status = progress.Status
				return nil
			})

			resumed, remaining, unresolved, resumeErr := svc.ResumePlan(ctx, run)
			require.NoError(t, resumeErr)
			require.False(t, unresolved)
			require.Equal(t, test.remaining, remaining)
			require.Equal(t, test.status, status)
			records, ok := svc.FrozenRecords(resumed)
			require.True(t, ok)
			require.Len(t, records, test.remaining)
		})
	}
}

func TestConfirmTargetRequiresRunningReplacementWhenBaselineRan(t *testing.T) {
	for _, test := range []struct {
		name      string
		running   bool
		status    container.ContainerState
		confirmed bool
	}{
		{
			name:      "running replacement",
			running:   true,
			status:    container.StateRunning,
			confirmed: true,
		},
		{
			name:   "exited replacement",
			status: container.StateExited,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					assert.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: "new", Names: []string{"/app"}}}))
				case strings.Contains(r.URL.Path, "/images/"):
					assert.NoError(t, json.MarshalWrite(w, image.InspectResponse{ID: "sha256:desired"}))
				case strings.Contains(r.URL.Path, "/containers/"):
					assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
						ID:     "new",
						Name:   "/app",
						Image:  "sha256:desired",
						Config: &container.Config{},
						State: &container.State{
							Status:  test.status,
							Running: test.running,
						},
					}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			svc := newTestService(t, server, nil)
			confirmed, unchanged, err := svc.ConfirmTarget(t.Context(), types.FrozenUpdateTarget{
				ContainerID:       "old",
				ContainerName:     "app",
				BaselineImageID:   "sha256:baseline",
				BaselineStartedAt: "baseline",
				BaselineRunning:   true,
				DesiredImageRef:   "registry.example.com/app:latest",
				DesiredDigest:     "sha256:desired",
			})
			require.NoError(t, err)
			require.Equal(t, test.confirmed, confirmed)
			require.False(t, unchanged)
		})
	}
}

func TestConfirmTargetStartsCreatedReplacementWhenBaselineRan(t *testing.T) {
	var started atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: "new", Names: []string{"/app"}}}))
		case strings.HasSuffix(r.URL.Path, "/containers/new/start"):
			started.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/images/"):
			assert.NoError(t, json.MarshalWrite(w, image.InspectResponse{ID: "sha256:desired"}))
		case strings.Contains(r.URL.Path, "/containers/"):
			assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
				ID:     "new",
				Name:   "/app",
				Image:  "sha256:desired",
				Config: &container.Config{},
				State:  &container.State{Status: container.StateCreated},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc := newTestService(t, server, nil)

	confirmed, unchanged, err := svc.ConfirmTarget(t.Context(), types.FrozenUpdateTarget{
		ContainerID:       "old",
		ContainerName:     "app",
		BaselineImageID:   "sha256:baseline",
		BaselineStartedAt: "baseline",
		BaselineRunning:   true,
		DesiredImageRef:   "registry.example.com/app:latest",
		DesiredDigest:     "sha256:desired",
	})
	require.NoError(t, err)
	require.True(t, started.Load())
	require.True(t, confirmed)
	require.False(t, unchanged)
}

func TestResumePlanStartsStoppedOriginalAndPersistsNewBaseline(t *testing.T) {
	target := types.FrozenUpdateTarget{
		ContainerID:       "old",
		ContainerName:     "app",
		BaselineImageID:   "sha256:baseline",
		BaselineStartedAt: "baseline",
		BaselineRunning:   true,
		DesiredImageRef:   "registry.example.com/app:latest",
		DesiredDigest:     "sha256:desired",
	}
	raw, err := json.Marshal(frozenUpdatePlan{
		Records: []updater.ImageUpdateRecord{{
			ContainerID: "old",
			ID:          "image",
			Repository:  "registry.example.com/app",
			Tag:         "latest",
			HasUpdate:   true,
		}},
		Targets: []types.FrozenUpdateTarget{target},
	})
	require.NoError(t, err)
	run := types.SchedulerRun{ID: "run", Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{{
		ID:           "auto-update",
		ResourceType: "update-batch",
		Status:       types.RunStatusRunning,
		RecoveryData: raw,
	}}}}
	var started atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{{ID: "old", Names: []string{"/app"}}}))
		case strings.HasSuffix(r.URL.Path, "/containers/old/start"):
			started.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/images/"):
			assert.NoError(t, json.MarshalWrite(w, image.InspectResponse{ID: "sha256:desired"}))
		case strings.Contains(r.URL.Path, "/containers/"):
			assert.NoError(t, json.MarshalWrite(w, container.InspectResponse{
				ID:     "old",
				Name:   "/app",
				Image:  "sha256:baseline",
				Config: &container.Config{},
				State: &container.State{
					Running:   started.Load(),
					StartedAt: kit.Ternary(started.Load(), "restarted", "baseline"),
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc := newTestService(t, server, nil)
	var persisted []byte
	ctx := jobcontext.WithExecution(t.Context(), run, func(progress types.TargetOutcome) error {
		if progress.ID == "auto-update" {
			persisted = progress.RecoveryData
		}
		return nil
	})

	resumed, remaining, unresolved, err := svc.ResumePlan(ctx, run)
	require.NoError(t, err)
	require.True(t, started.Load(), "the stopped original is started before the update resumes")
	require.False(t, unresolved)
	require.Equal(t, 1, remaining)
	plan := resumed.Value(frozenPendingKey{}).(*frozenUpdatePlan)
	require.Equal(t, "restarted", plan.Targets[0].BaselineStartedAt)
	var saved frozenUpdatePlan
	require.NoError(t, json.Unmarshal(persisted, &saved))
	require.Equal(t, "restarted", saved.Targets[0].BaselineStartedAt)
}
