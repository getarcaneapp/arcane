package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/gitops"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// adminCtx returns a context where the auth middleware has marked the user as admin.
func adminCtx() context.Context {
	return context.WithValue(context.Background(), humamw.ContextKeyUserIsAdmin, true)
}

// nonAdminCtx returns a context where the auth middleware has marked the user as non-admin.
// Equivalent to context.Background() since IsAdminFromContext defaults to false when the key
// is absent, but the explicit setting makes the test intent clear.
func nonAdminCtx() context.Context {
	return context.WithValue(context.Background(), humamw.ContextKeyUserIsAdmin, false)
}

func TestValidateGitOpsSyncTargetType(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		targetType  string
		wantErr     bool
		wantStatus  int
		wantMessage string
	}{
		{name: "empty allowed for non-admin", ctx: nonAdminCtx(), targetType: "", wantErr: false},
		{name: "project allowed for non-admin", ctx: nonAdminCtx(), targetType: "project", wantErr: false},
		{name: "swarm_stack forbidden for non-admin", ctx: nonAdminCtx(), targetType: "swarm_stack", wantErr: true, wantStatus: http.StatusForbidden, wantMessage: "admin access required"},
		{name: "swarm_stack allowed for admin", ctx: adminCtx(), targetType: "swarm_stack", wantErr: false},
		{name: "unknown value rejected as 400", ctx: nonAdminCtx(), targetType: "garbage", wantErr: true, wantStatus: http.StatusBadRequest, wantMessage: "invalid targetType"},
		{name: "unknown value rejected as 400 even for admin", ctx: adminCtx(), targetType: "garbage", wantErr: true, wantStatus: http.StatusBadRequest, wantMessage: "invalid targetType"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitOpsSyncTargetTypeInternal(tt.ctx, tt.targetType)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var statusErr huma.StatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, tt.wantStatus, statusErr.GetStatus())
			require.Contains(t, statusErr.Error(), tt.wantMessage)
		})
	}
}

// TestGitOpsSyncHandler_CreateSync_TargetTypeGate verifies that the swarm_stack admin
// gate fires before any service call, matching the precondition used by the test
// scaffolding (empty service struct — handler must not reach it).
func TestGitOpsSyncHandler_CreateSync_TargetTypeGate(t *testing.T) {
	handler := &GitOpsSyncHandler{syncService: &services.GitOpsSyncService{}}

	t.Run("non-admin with swarm_stack body is forbidden", func(t *testing.T) {
		_, err := handler.CreateSync(nonAdminCtx(), &CreateGitOpsSyncInput{
			EnvironmentID: "env-1",
			Body:          gitops.CreateSyncRequest{TargetType: "swarm_stack"},
		})
		require.Error(t, err)
		var statusErr huma.StatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusForbidden, statusErr.GetStatus())
		require.Contains(t, statusErr.Error(), "admin access required")
	})

	t.Run("non-admin with unknown targetType is rejected as 400", func(t *testing.T) {
		_, err := handler.CreateSync(nonAdminCtx(), &CreateGitOpsSyncInput{
			EnvironmentID: "env-1",
			Body:          gitops.CreateSyncRequest{TargetType: "garbage"},
		})
		require.Error(t, err)
		var statusErr huma.StatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
		require.Contains(t, statusErr.Error(), "invalid targetType")
	})
}

// TestGitOpsSyncHandler_UpdateSync_TargetTypeGate verifies that an UpdateSync body
// setting TargetType=swarm_stack is rejected for non-admins before any service call.
// (The stored-sync admin check for existing swarm_stack syncs requires a working DB
// and is covered by manual smoke testing.)
func TestGitOpsSyncHandler_UpdateSync_TargetTypeGate(t *testing.T) {
	handler := &GitOpsSyncHandler{syncService: &services.GitOpsSyncService{}}

	swarmType := "swarm_stack"
	garbageType := "garbage"

	t.Run("non-admin promoting to swarm_stack is forbidden", func(t *testing.T) {
		_, err := handler.UpdateSync(nonAdminCtx(), &UpdateGitOpsSyncInput{
			EnvironmentID: "env-1",
			SyncID:        "sync-1",
			Body:          gitops.UpdateSyncRequest{TargetType: &swarmType},
		})
		require.Error(t, err)
		var statusErr huma.StatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusForbidden, statusErr.GetStatus())
		require.Contains(t, statusErr.Error(), "admin access required")
	})

	t.Run("unknown targetType in update is rejected as 400", func(t *testing.T) {
		_, err := handler.UpdateSync(nonAdminCtx(), &UpdateGitOpsSyncInput{
			EnvironmentID: "env-1",
			SyncID:        "sync-1",
			Body:          gitops.UpdateSyncRequest{TargetType: &garbageType},
		})
		require.Error(t, err)
		var statusErr huma.StatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
		require.Contains(t, statusErr.Error(), "invalid targetType")
	})
}

func TestGitOpsSyncHandler_DeleteSync_SwarmStackRequiresAdmin(t *testing.T) {
	db, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GitOpsSync{}))

	sync := models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: "sync-1"},
		Name:          "swarm sync",
		EnvironmentID: "env-1",
		RepositoryID:  "repo-1",
		TargetType:    "swarm_stack",
	}
	require.NoError(t, db.Create(&sync).Error)

	handler := &GitOpsSyncHandler{
		syncService: services.NewGitOpsSyncService(&database.DB{DB: db}, nil, nil, nil, nil, nil),
	}

	_, err = handler.DeleteSync(nonAdminCtx(), &DeleteGitOpsSyncInput{
		EnvironmentID: "env-1",
		SyncID:        "sync-1",
	})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusForbidden, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), "admin access required")

	var count int64
	require.NoError(t, db.Model(&models.GitOpsSync{}).Where("id = ?", "sync-1").Count(&count).Error)
	require.Equal(t, int64(1), count)
}
