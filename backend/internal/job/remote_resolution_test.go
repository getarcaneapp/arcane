package job

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRemoteRunConfirmsOwnerAfterLostResponse(t *testing.T) {
	db := setupSettingsTestDBInternal(t)
	require.NoError(t, db.AutoMigrate(&environment.Environment{}))
	q := queue.New(kv.NewKVService(db), nil, nil)
	local, err := q.Submit(t.Context(), st.Request{JobID: "auto-update", EnvironmentID: "remote"})
	require.NoError(t, err)
	require.NoError(t, q.UpdateRun(t.Context(), local, func(run *st.Run) error {
		run.Status = st.NeedsAttention
		run.RemoteAccepted = true
		run.RemoteDeliveryAttempted = true
		run.Outcome = st.Outcome{Status: st.NeedsAttention, Message: "original failure"}
		return nil
	}))
	remote := st.Run{ID: local.ID, JobID: local.JobID, EnvironmentID: "0", Status: st.Running}
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
		case strings.HasSuffix(r.URL.Path, "/resolve"):
			mutations++
			remote.Status = st.Canceled
			remote.Resolution = &st.RunResolution{ResolvedBy: "operator", ResolvedAt: time.Now().UTC(), Reason: "resolved_after_review"}
			http.Error(w, "lost response", http.StatusServiceUnavailable)
			return
		case strings.HasSuffix(r.URL.Path, "/ack"):
			remote.RemoteSettled = true
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
	require.NoError(t, db.Create(&environment.Environment{BaseModel: database.BaseModel{ID: "remote"}, Name: "agent", ApiUrl: server.URL, Enabled: true}).Error)
	svc := &JobService{Queue: q, environment: environment.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil)}
	ctx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.EnvironmentPermissionSet("remote"))
	_, err = svc.ResolveRun(ctx, "remote", local.JobID, local.ID, "operator")
	require.ErrorContains(t, err, "agent run must need attention")
	require.Zero(t, mutations)
	remote.Status = st.NeedsAttention
	_, err = svc.ResolveRun(ctx, "remote", local.JobID, local.ID, "operator")
	require.Error(t, err)
	unchanged, err := q.Get(t.Context(), "remote", local.JobID, local.ID)
	require.NoError(t, err)
	require.Equal(t, st.NeedsAttention, unchanged.Status)
	resolved, err := svc.ResolveRun(ctx, "remote", local.JobID, local.ID, "operator")
	require.NoError(t, err)
	require.Equal(t, st.Canceled, resolved.Status)
	require.True(t, resolved.RemoteSettled)
	require.Equal(t, "original failure", resolved.Outcome.Message)
	require.Equal(t, 1, mutations)
}
