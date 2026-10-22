package kv

import (
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

func setupKVServiceInternal(t *testing.T) *KVService {
	t.Helper()

	db := setupSettingsTestDB(t)
	require.NoError(t, db.AutoMigrate(&KVEntry{}))

	return NewKVService(db)
}

func TestKVService_Get_MissingKey(t *testing.T) {
	ctx := t.Context()
	svc := setupKVServiceInternal(t)

	value, ok, err := svc.Get(ctx, "missing")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, value)
}

func TestKVService_Set_UpsertsValue(t *testing.T) {
	ctx := t.Context()
	svc := setupKVServiceInternal(t)

	require.NoError(t, svc.Set(ctx, "analytics.heartbeat.last_attempt_at", "2026-03-10T00:00:00Z"))

	value, ok, err := svc.Get(ctx, "analytics.heartbeat.last_attempt_at")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "2026-03-10T00:00:00Z", value)

	require.NoError(t, svc.Set(ctx, "analytics.heartbeat.last_attempt_at", "2026-03-11T00:00:00Z"))

	updatedValue, updatedOK, err := svc.Get(ctx, "analytics.heartbeat.last_attempt_at")
	require.NoError(t, err)
	require.True(t, updatedOK)
	require.Equal(t, "2026-03-11T00:00:00Z", updatedValue)
}

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	return &database.DB{DB: db}
}

func TestKVService_ListByPrefix_EscapesLikeWildcards(t *testing.T) {
	ctx := t.Context()
	svc := setupKVServiceInternal(t)

	entries := map[string]string{
		`project_rename_journal:real`:       "journal",
		`projectXrenameYjournalZ:false`:     "false-positive",
		`registry%pulls:real`:               "registry",
		`registryXpulls:false`:              "registry-false-positive",
		`path\prefix:real`:                  "path",
		`pathXprefix:false`:                 "path-false-positive",
		`project_rename_journal:real:other`: "journal-other",
	}
	for key, value := range entries {
		require.NoError(t, svc.Set(ctx, key, value))
	}

	journalEntries, err := svc.ListByPrefix(ctx, "project_rename_journal:")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		`project_rename_journal:real`,
		`project_rename_journal:real:other`,
	}, kvEntryKeysForPrefixTestInternal(journalEntries))

	registryEntries, err := svc.ListByPrefix(ctx, "registry%pulls:")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{`registry%pulls:real`}, kvEntryKeysForPrefixTestInternal(registryEntries))

	pathEntries, err := svc.ListByPrefix(ctx, `path\prefix:`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{`path\prefix:real`}, kvEntryKeysForPrefixTestInternal(pathEntries))
}

func kvEntryKeysForPrefixTestInternal(entries []KVEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}
