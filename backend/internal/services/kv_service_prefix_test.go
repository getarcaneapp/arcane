package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/stretchr/testify/require"
)

func TestKVService_ListByPrefix_EscapesLikeWildcards(t *testing.T) {
	ctx := context.Background()
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

func kvEntryKeysForPrefixTestInternal(entries []models.KVEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}
