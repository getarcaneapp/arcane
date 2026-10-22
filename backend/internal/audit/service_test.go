package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

func setupAuditService(t *testing.T) (*AuditService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&AuditLog{}))
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

func TestRecordInTxRollsBackWithTransaction(t *testing.T) {
	service, db := setupAuditService(t)
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

	logs, page, err := service.List(t.Context(), pagination.QueryParams{}, types.AuditFilter{})
	require.NoError(t, err)
	require.Empty(t, logs)
	require.Zero(t, page.TotalItems)
}

func TestRecordCapturesActorFromContext(t *testing.T) {
	service, _ := setupAuditService(t)
	ctx := context.WithValue(actorContext(t, "alice", "alice"), middleware.ContextKeyApiKeyID, "key-1")
	ctx = context.WithValue(ctx, middleware.ContextKeyRemoteAddr, "10.0.0.1")
	require.NoError(t, service.Record(ctx, types.AuditEntry{
		Action: types.AuditActionGroupCreate,
		After:  map[string]any{"name": "ops"},
	}))
	require.NoError(t, service.Record(t.Context(), types.AuditEntry{Action: types.AuditActionGroupDelete}))

	logs, _, err := service.List(t.Context(), pagination.QueryParams{}, types.AuditFilter{Action: types.AuditActionGroupCreate})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, types.AuditActorApiKey, logs[0].ActorType)
	require.Equal(t, "alice", *logs[0].ActorUserID)
	require.Equal(t, "key-1", *logs[0].ApiKeyID)
	require.Equal(t, "10.0.0.1", logs[0].RemoteAddr)
	require.Equal(t, types.AuditOutcomeSuccess, logs[0].Outcome)
	require.Equal(t, map[string]any{"name": "ops"}, logs[0].After)

	system, _, err := service.List(t.Context(), pagination.QueryParams{}, types.AuditFilter{Action: types.AuditActionGroupDelete})
	require.NoError(t, err)
	require.Len(t, system, 1)
	require.Equal(t, types.AuditActorSystem, system[0].ActorType)
	require.Nil(t, system[0].ActorUserID)

	fetched, err := service.Get(t.Context(), logs[0].ID)
	require.NoError(t, err)
	require.Equal(t, logs[0].ID, fetched.ID)
}

func TestListFiltersAndPaginates(t *testing.T) {
	service, db := setupAuditService(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, entry := range []AuditLog{
		{Action: types.AuditActionRoleCreate, ActorUserID: new("alice"), Outcome: types.AuditOutcomeSuccess},
		{Action: types.AuditActionRoleUpdate, ActorUserID: new("alice"), Outcome: types.AuditOutcomeSuccess},
		{Action: types.AuditActionAuthzDenied, ActorUserID: new("bob"), Outcome: types.AuditOutcomeDenied, EnvironmentID: new("env-1")},
		{Action: types.AuditActionRoleDelete, ActorUserID: new("bob"), Outcome: types.AuditOutcomeSuccess},
	} {
		require.NoError(t, db.Create(&AuditLog{
			ID:            string(rune('a' + i)),
			CreatedAt:     base.Add(time.Duration(i) * time.Hour),
			ActorType:     types.AuditActorUser,
			ActorUserID:   entry.ActorUserID,
			Action:        entry.Action,
			Outcome:       entry.Outcome,
			EnvironmentID: entry.EnvironmentID,
		}).Error)
	}

	params := pagination.QueryParams{
		SortParams: pagination.SortParams{
			Sort:  "createdAt",
			Order: pagination.SortDesc,
		},
		Params: pagination.Params{Limit: 2},
	}
	page, resp, err := service.List(t.Context(), params, types.AuditFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 4, resp.TotalItems)
	require.Equal(t, []string{"d", "c"}, []string{page[0].ID, page[1].ID})

	alice, _, err := service.List(t.Context(), pagination.QueryParams{}, types.AuditFilter{ActorUserID: "alice"})
	require.NoError(t, err)
	require.Len(t, alice, 2)

	denied, _, err := service.List(t.Context(), pagination.QueryParams{}, types.AuditFilter{
		Outcome:       types.AuditOutcomeDenied,
		EnvironmentID: "env-1",
	})
	require.NoError(t, err)
	require.Len(t, denied, 1)
	require.Equal(t, "c", denied[0].ID)

	windowed, _, err := service.List(t.Context(), pagination.QueryParams{}, types.AuditFilter{
		From: new(base.Add(time.Hour)),
		To:   new(base.Add(3 * time.Hour)),
	})
	require.NoError(t, err)
	require.Len(t, windowed, 2)
}

func TestPruneRemovesOlderEntries(t *testing.T) {
	service, db := setupAuditService(t)
	now := time.Now()
	for id, createdAt := range map[string]time.Time{
		"old":    now.AddDate(0, 0, -400),
		"recent": now.AddDate(0, 0, -10),
	} {
		require.NoError(t, db.Create(&AuditLog{
			ID:        id,
			CreatedAt: createdAt,
			ActorType: types.AuditActorSystem,
			Action:    types.AuditActionRoleCreate,
			Outcome:   types.AuditOutcomeSuccess,
		}).Error)
	}

	removed, err := service.Prune(t.Context(), now.AddDate(0, 0, -365))
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)

	var remaining []string
	require.NoError(t, db.Model(&AuditLog{}).Pluck("id", &remaining).Error)
	require.Equal(t, []string{"recent"}, remaining)
}

func TestExportWritesCSVAndNDJSON(t *testing.T) {
	service, _ := setupAuditService(t)
	ctx := actorContext(t, "alice", "=alice")
	require.NoError(t, service.Record(ctx, types.AuditEntry{
		Action:     types.AuditActionRoleCreate,
		TargetType: types.AuditTargetRole,
		TargetID:   "role-1",
		TargetName: "Operators",
		After:      map[string]any{"permissions": []string{"containers:read"}},
	}))
	require.NoError(t, service.Record(ctx, types.AuditEntry{
		Action:     types.AuditActionRoleDelete,
		TargetType: types.AuditTargetRole,
		TargetID:   "role-1",
	}))

	var csvOut bytes.Buffer
	require.NoError(t, service.Export(t.Context(), types.AuditFilter{}, types.AuditExportFormatCSV, &csvOut))
	records, err := csv.NewReader(&csvOut).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3)
	require.Equal(t, csvHeader, records[0])
	require.Equal(t, "'=alice", records[1][4])
	require.Equal(t, types.AuditActionRoleCreate, records[1][6])
	require.JSONEq(t, `{"permissions":["containers:read"]}`, records[1][18])

	var ndjsonOut bytes.Buffer
	require.NoError(t, service.Export(t.Context(), types.AuditFilter{Action: types.AuditActionRoleDelete}, types.AuditExportFormatNDJSON, &ndjsonOut))
	lines := strings.Split(strings.TrimSpace(ndjsonOut.String()), "\n")
	require.Len(t, lines, 1)
	var decoded types.AuditLog
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &decoded))
	require.Equal(t, types.AuditActionRoleDelete, decoded.Action)
	require.Equal(t, "alice", *decoded.ActorUserID)

	require.Error(t, service.Export(t.Context(), types.AuditFilter{}, "xml", &bytes.Buffer{}))
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

	var rows []AuditLog
	require.NoError(t, db.Order("request_path, actor_user_id").Find(&rows).Error)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, types.AuditActionAuthzDenied, row.Action)
		require.Equal(t, types.AuditOutcomeDenied, row.Outcome)
		require.Equal(t, "env-1", *row.EnvironmentID)
	}
	require.Equal(t, "bob", *rows[0].ActorUserID)
	require.Equal(t, "carol", *rows[1].ActorUserID)
	require.Equal(t, "/api/environments/env-1/containers/def/start", rows[2].RequestPath)
}

func TestRecordDenialSkipsWhenDisabled(t *testing.T) {
	_, db := setupAuditService(t)
	service := NewAuditService(&database.DB{DB: db}, func(context.Context) bool {
		return false
	})
	require.NoError(t, service.Start(t.Context()))
	service.RecordDenial(actorContext(t, "bob", "bob"), types.AuthzDenial{Permission: "containers:start"})
	require.NoError(t, service.Stop(t.Context()))

	var count int64
	require.NoError(t, db.Model(&AuditLog{}).Count(&count).Error)
	require.Zero(t, count)
}
