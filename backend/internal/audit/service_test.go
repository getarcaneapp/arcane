package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
)

func setupAuditService(t *testing.T) (*AuditService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&event.Event{}))
	return NewAuditService(&database.DB{DB: db}, func(context.Context) bool {
		return true
	}), db
}

func actorContext(t *testing.T, userID, username string) context.Context {
	return context.WithValue(t.Context(), types.CurrentUserContextKey{}, &types.Actor{
		ID:       userID,
		Username: username,
	})
}

func auditEvents(t *testing.T, db *gorm.DB, eventType string) []event.Event {
	t.Helper()
	var rows []event.Event
	query := db.Where("audit = ?", true).Order("created_at, id")
	if eventType != "" {
		query = query.Where("type = ?", eventType)
	}
	require.NoError(t, query.Find(&rows).Error)
	return rows
}

func TestRecordInTxRollsBackWithTransaction(t *testing.T) {
	_, db := setupAuditService(t)
	errAbort := errors.New("abort")
	err := db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, RecordInTx(t.Context(), tx, types.AuditEntry{
			Action:     types.AuditActionRoleCreate,
			TargetType: types.AuditTargetRole,
			TargetID:   "role-1",
		}))
		return errAbort
	})
	require.ErrorIs(t, err, errAbort)
	require.Empty(t, auditEvents(t, db, ""))
}

func TestRecordCapturesActorFromContext(t *testing.T) {
	service, db := setupAuditService(t)
	ctx := context.WithValue(actorContext(t, "alice", "alice"), middleware.ContextKeyApiKeyID, "key-1")
	ctx = context.WithValue(ctx, middleware.ContextKeyRemoteAddr, "10.0.0.1")
	require.NoError(t, service.Record(ctx, types.AuditEntry{
		Action:     types.AuditActionGroupCreate,
		TargetType: types.AuditTargetGroup,
		TargetID:   "group-1",
		TargetName: "ops",
		After:      map[string]any{"name": "ops"},
	}))
	require.NoError(t, service.Record(t.Context(), types.AuditEntry{Action: types.AuditActionGroupDelete}))

	created := auditEvents(t, db, types.AuditActionGroupCreate)
	require.Len(t, created, 1)
	row := created[0]
	require.True(t, row.Audit)
	require.Equal(t, event.EventSeverityInfo, row.Severity)
	require.Equal(t, "Group created: ops", row.Title)
	require.Equal(t, "Group created: ops by alice", row.Description)
	require.Equal(t, types.AuditTargetGroup, *row.ResourceType)
	require.Equal(t, "group-1", *row.ResourceID)
	require.Equal(t, "alice", *row.UserID)
	require.Equal(t, types.AuditActorApiKey, row.Metadata["actorType"])
	require.Equal(t, "key-1", row.Metadata["apiKeyId"])
	require.Equal(t, types.AuditOutcomeSuccess, row.Metadata["outcome"])
	require.Equal(t, map[string]any{"remoteAddr": "10.0.0.1"}, row.Metadata["request"])
	require.Equal(t, map[string]any{"name": "ops"}, row.Metadata["after"])

	system := auditEvents(t, db, types.AuditActionGroupDelete)
	require.Len(t, system, 1)
	require.Equal(t, types.AuditActorSystem, system[0].Metadata["actorType"])
	require.Equal(t, "system", *system[0].UserID)
	require.Equal(t, "System", *system[0].Username)
}

func TestRecordMapsErrorOutcomeToErrorSeverity(t *testing.T) {
	service, db := setupAuditService(t)
	require.NoError(t, service.Record(t.Context(), types.AuditEntry{
		Action:     types.AuditActionRoleDelete,
		Outcome:    types.AuditOutcomeError,
		TargetType: types.AuditTargetRole,
		TargetName: "Operators",
	}))

	rows := auditEvents(t, db, types.AuditActionRoleDelete)
	require.Len(t, rows, 1)
	require.Equal(t, event.EventSeverityError, rows[0].Severity)
	require.Equal(t, "Role deleted (failed): Operators", rows[0].Title)
}

func TestRecordDenialDedupesWithinWindow(t *testing.T) {
	service, db := setupAuditService(t)
	require.NoError(t, service.Start(t.Context()))
	ctx := actorContext(t, "bob", "bob")
	denial := types.AuthzDenial{
		Permission:    "containers:start",
		EnvironmentID: "env-1",
		Method:        "POST",
		Path:          "/api/environments/env-1/containers/abc/start",
		RemoteAddr:    "10.0.0.2",
	}
	for range 5 {
		service.RecordDenial(ctx, denial)
	}
	service.RecordDenial(ctx, types.AuthzDenial{
		Permission:    denial.Permission,
		EnvironmentID: denial.EnvironmentID,
		Method:        denial.Method,
		Path:          "/api/environments/env-1/containers/def/start",
	})
	service.RecordDenial(actorContext(t, "carol", "carol"), denial)
	require.NoError(t, service.Stop(t.Context()))

	rows := auditEvents(t, db, types.AuditActionAuthzDenied)
	require.Len(t, rows, 3)
	paths := map[string][]string{}
	for _, row := range rows {
		require.Equal(t, event.EventSeverityWarning, row.Severity)
		require.Equal(t, "Permission denied: containers:start", row.Title)
		require.Equal(t, types.AuditOutcomeDenied, row.Metadata["outcome"])
		require.Equal(t, "containers:start", row.Metadata["permission"])
		require.Equal(t, "env-1", *row.EnvironmentID)
		request, ok := row.Metadata["request"].(map[string]any)
		require.True(t, ok)
		paths[*row.UserID] = append(paths[*row.UserID], request["path"].(string))
	}
	require.ElementsMatch(t, []string{
		"/api/environments/env-1/containers/abc/start",
		"/api/environments/env-1/containers/def/start",
	}, paths["bob"])
	require.Equal(t, []string{"/api/environments/env-1/containers/abc/start"}, paths["carol"])
}

func TestRecordDenialSkipsWhenDisabled(t *testing.T) {
	_, db := setupAuditService(t)
	service := NewAuditService(&database.DB{DB: db}, func(context.Context) bool {
		return false
	})
	require.NoError(t, service.Start(t.Context()))
	service.RecordDenial(actorContext(t, "bob", "bob"), types.AuthzDenial{Permission: "containers:start"})
	require.NoError(t, service.Stop(t.Context()))
	require.Empty(t, auditEvents(t, db, ""))
}
