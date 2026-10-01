package job

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRemoteRunConfirmsOwnerAfterLostResponse(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "legacy operator"
		if automatic {
			name = "automatic settlement"
		}
		t.Run(name, func(t *testing.T) {
			db := setupSettingsTestDBInternal(t)
			require.NoError(t, db.AutoMigrate(&environment.Environment{}))
			q := newJobCoordinatorForTestInternal(t, db)
			local, err := q.Submit(t.Context(), st.Request{JobID: "auto-update", EnvironmentID: "remote", Trigger: "scheduled"})
			require.NoError(t, err)
			require.NoError(t, q.UpdateRun(t.Context(), local, func(run *st.Run) error {
				run.Status = st.NeedsAttention
				if automatic {
					run.Status = st.Running
				}
				run.RemoteAccepted = true
				run.RemoteDeliveryAttempted = true
				run.Outcome = st.Outcome{Status: st.NeedsAttention, Message: "original failure"}
				return nil
			}))
			remote := st.Run{ID: local.ID, JobID: local.JobID, EnvironmentID: "0", Status: st.Running, Outcome: st.Outcome{Status: st.NeedsAttention, Message: "original failure", Targets: []st.TargetOutcome{{ID: "completed", Status: st.Succeeded}}}}
			mutations, acknowledgements := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Query().Has("page"):
					raw, marshalErr := json.Marshal(st.RunList{Runs: []st.Run{remote}, Total: 1})
					assert.NoError(t, marshalErr)
					w.Header().Set("Content-Type", "application/json")
					_, writeErr := w.Write(raw)
					assert.NoError(t, writeErr)
					return
				case r.Method == http.MethodGet:
				case strings.HasSuffix(r.URL.Path, "/resolve"):
					mutations++
					var body struct {
						ResolvedBy string `json:"resolvedBy"`
					}
					assert.NoError(t, json.UnmarshalRead(r.Body, &body))
					remote.Status = st.Canceled
					remote.Resolution = &st.RunResolution{ResolvedBy: body.ResolvedBy, ResolvedAt: time.Now().UTC(), Reason: "resolved_after_review"}
					http.Error(w, "lost response", http.StatusServiceUnavailable)
					return
				case strings.HasSuffix(r.URL.Path, "/ack"):
					acknowledgements++
					remote.RemoteSettled = true
					if automatic && acknowledgements == 1 {
						http.Error(w, "lost acknowledgement", http.StatusServiceUnavailable)
						return
					}
				default:
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				raw, marshalErr := json.Marshal(remote)
				assert.NoError(t, marshalErr)
				w.Header().Set("Content-Type", "application/json")
				_, writeErr := w.Write(raw)
				assert.NoError(t, writeErr)
			}))
			defer server.Close()
			require.NoError(t, db.Create(&environment.Environment{ID: "remote", Name: "agent", ApiUrl: server.URL, Enabled: true}).Error)
			svc := &JobService{runs: q, environment: environment.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil)}
			ctx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.EnvironmentPermissionSet("remote"))
			path := "/api/environments/0/jobs/auto-update/runs/" + local.ID
			if !automatic {
				_, err = svc.ResolveRun(ctx, "remote", local.JobID, local.ID, "operator")
				require.ErrorContains(t, err, "agent run must need attention")
			} else {
				outcome, confirmErr := svc.confirmRemoteRunInternal(ctx, local, remote, path)
				require.NoError(t, confirmErr)
				require.Equal(t, st.Waiting, outcome.Status)
			}
			require.Zero(t, mutations)
			remote.Status = st.NeedsAttention
			if !automatic {
				_, err = svc.ResolveRun(ctx, "remote", local.JobID, local.ID, "operator")
				require.Error(t, err)
				unchanged, getErr := q.Get(t.Context(), "remote", local.JobID, local.ID)
				require.NoError(t, getErr)
				require.Equal(t, st.NeedsAttention, unchanged.Status)
				resolved, resolveErr := svc.ResolveRun(ctx, "remote", local.JobID, local.ID, "operator")
				require.NoError(t, resolveErr)
				require.Equal(t, st.Canceled, resolved.Status)
				require.True(t, resolved.RemoteSettled)
				require.Equal(t, "original failure", resolved.Outcome.Message)
			} else {
				outcome, confirmErr := svc.confirmRemoteRunInternal(ctx, local, remote, path)
				require.Error(t, confirmErr)
				require.Equal(t, st.Waiting, outcome.Status)
				outcome, confirmErr = svc.confirmRemoteRunInternal(ctx, local, remote, path)
				require.Error(t, confirmErr)
				require.Equal(t, st.Waiting, outcome.Status)
				outcome, confirmErr = svc.confirmRemoteRunInternal(ctx, local, remote, path)
				require.NoError(t, confirmErr)
				require.Equal(t, st.Failed, outcome.Status)
				require.Equal(t, "original failure", outcome.Message)
				require.Equal(t, st.Succeeded, outcome.Targets[0].Status)
				stored, getErr := q.Get(t.Context(), "remote", local.JobID, local.ID)
				require.NoError(t, getErr)
				require.True(t, stored.RemoteSettled)
				require.Equal(t, st.Failed, stored.RemoteOutcome.Status)
				require.Equal(t, common.SystemUser.Username, stored.Resolution.ResolvedBy)
				require.NoError(t, q.UpdateRun(ctx, stored, func(current *st.Run) error {
					current.Status = st.Failed
					return nil
				}))
				_, retryErr := svc.RetryRemoteRun(ctx, "remote", local.JobID, local.ID)
				require.ErrorContains(t, retryErr, "upgrade the agent")
				remote.Status = st.Failed
				retried, retryErr := svc.RetryRemoteRun(ctx, "remote", local.JobID, local.ID)
				require.NoError(t, retryErr)
				require.Equal(t, st.Queued, retried.Status)
				require.Equal(t, local.ID, retried.ID)

				// Adopt agent-owned history without coalescing its explicit identity.
				agentOwned := remote
				agentOwned.ID = "d7a9a3c7-d364-4c36-831a-381249e5727e"
				agentOwned.Status = st.NeedsAttention
				agentOwned.CreatedAt = time.Now().UTC().Add(-time.Hour)
				agentOwned.FinishedAt = new(agentOwned.CreatedAt.Add(time.Minute))
				agentOwned.ActivityID = "agent-summary"
				agentOwned.RemoteSettled = false
				catalog := []jobschedule.JobStatus{{ID: "updates", Children: []jobschedule.JobStatus{{ID: agentOwned.JobID, LastRun: &agentOwned}}}}
				require.NoError(t, svc.reconcileLegacyRemoteCatalogInternal(ctx, "remote", catalog))
				require.NoError(t, svc.reconcileLegacyRemoteCatalogInternal(ctx, "remote", catalog))
				adopted, getErr := q.Get(ctx, "remote", agentOwned.JobID, agentOwned.ID)
				require.NoError(t, getErr)
				require.True(t, adopted.RemoteAccepted)
				require.True(t, adopted.RemoteDeliveryAttempted)
				require.Equal(t, agentOwned.CreatedAt, adopted.CreatedAt)
				require.Equal(t, st.Queued, adopted.Status)
				require.Equal(t, agentOwned.FinishedAt, adopted.FinishedAt)
				require.Equal(t, agentOwned.ActivityID, adopted.ActivityID)
				require.Equal(t, "remote", adopted.ActivityEnvironmentID)
				status := jobschedule.JobStatus{LastRun: &agentOwned}
				applyRunStatusInternal(&status, []st.Run{adopted})
				require.Nil(t, status.CurrentRun)
				require.Equal(t, st.Failed, status.LastRun.Status)
				require.Equal(t, "original failure", status.LastError)
				remote = agentOwned
				merged, listErr := svc.ListRuns(ctx, "remote", agentOwned.JobID, 1, 20)
				require.NoError(t, listErr)
				detail, detailErr := svc.GetRun(ctx, "remote", agentOwned.JobID, agentOwned.ID)
				require.NoError(t, detailErr)
				require.Equal(t, st.Failed, detail.Status)
				require.Equal(t, "original failure", detail.Outcome.Message)
				require.Equal(t, agentOwned.ActivityID, detail.ActivityID)
				require.Equal(t, "remote", detail.ActivityEnvironmentID)
				require.Contains(t, merged.Runs, detail)
				history, listErr := q.List(ctx, "remote", agentOwned.JobID, 1, 20)
				require.NoError(t, listErr)
				require.Equal(t, 2, history.Total)
			}
			require.Equal(t, 1, mutations)
		})
	}
}
