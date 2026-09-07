package job

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/queue"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

func TestResolveRunChecksCurrentOperatorPermission(t *testing.T) {
	db := setupSettingsTestDBInternal(t)
	svc := &JobService{Queue: queue.New(kv.NewKVService(db), nil, nil)}
	run, err := svc.Queue.Submit(t.Context(), st.Request{JobID: "auto-update", Trigger: "scheduled"})
	require.NoError(t, err)
	require.NoError(t, svc.Queue.UpdateRun(t.Context(), run, func(current *st.Run) error {
		current.Status = st.NeedsAttention
		current.Outcome = st.Outcome{Status: st.NeedsAttention, Message: "interrupted"}
		return nil
	}))
	_, err = svc.ResolveRun(t.Context(), "0", "auto-update", run.ID, "operator")
	require.ErrorContains(t, err, "permission denied")
	otherEnvironment := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.EnvironmentPermissionSet("different"))
	_, err = svc.ResolveRun(otherEnvironment, "0", "auto-update", run.ID, "operator")
	require.ErrorContains(t, err, "permission denied")
	authorized := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.EnvironmentPermissionSet("0"))
	_, err = svc.ResolveRun(authorized, "0", "auto-update", run.ID, "")
	require.ErrorContains(t, err, "identity")
	resolved, err := svc.ResolveRun(authorized, "0", "auto-update", run.ID, "operator")
	require.NoError(t, err)
	require.Equal(t, st.Canceled, resolved.Status)
	require.Equal(t, "interrupted", resolved.Outcome.Message)
	require.Equal(t, "operator", resolved.Resolution.ResolvedBy)
}
