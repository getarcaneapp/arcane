package backupbrowser

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestBuildEntriesSynthesizesAndSortsDirectoriesInternal(t *testing.T) {
	entries := BuildEntries([]string{"z.txt", "alpha/nested/file.txt", "Beta/", "a.txt"}, "", false)
	require.Equal(t, []backuptypes.BackupFileEntry{
		{Path: "alpha", Name: "alpha", IsDirectory: true},
		{Path: "Beta", Name: "Beta", IsDirectory: true},
		{Path: "a.txt", Name: "a.txt"},
		{Path: "z.txt", Name: "z.txt"},
	}, entries)
}

func TestBuildEntriesNestedBrowseInternal(t *testing.T) {
	entries := BuildEntries([]string{"folder/nested/", "folder/file.txt"}, "folder", false)
	require.Equal(t, []backuptypes.BackupFileEntry{
		{Path: "folder/nested", Name: "nested", IsDirectory: true},
		{Path: "folder/file.txt", Name: "file.txt"},
	}, entries)
}

func TestBrowseWalkReachesEveryEntryInternal(t *testing.T) {
	entries := BuildEntries([]string{"d/", "a.txt", "b.txt", "c.txt", "d/e.txt"}, "", true)
	var walked []backuptypes.BackupFileEntry
	start := 0
	for {
		items, page := Browse(entries, pagination.QueryParams{Params: pagination.Params{Start: start, Limit: 2}})
		walked = append(walked, items...)
		start += len(items)
		if int64(start) >= page.TotalItems {
			break
		}
	}
	require.Equal(t, entries, walked)
}

func TestBrowseSearchOrderingAndPaginationInternal(t *testing.T) {
	entries := []backuptypes.BackupFileEntry{
		{Path: "zeta/file.txt", Name: "file.txt"},
		{Path: "Alpha", Name: "Alpha", IsDirectory: true},
		{Path: "alpha/Second.txt", Name: "Second.txt"},
		{Path: "alpha/first.txt", Name: "first.txt"},
	}
	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: "ALPHA"},
		Params:      pagination.Params{Start: 1, Limit: 1},
	}
	items, page := Browse(entries, params)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "alpha/first.txt", Name: "first.txt"}}, items)
	require.Equal(t, int64(3), page.TotalItems)
	require.Equal(t, int64(4), page.GrandTotalItems)
	require.Equal(t, 2, page.CurrentPage)
	require.Equal(t, 1, page.ItemsPerPage)

	items, page = Browse(entries, pagination.QueryParams{Params: pagination.Params{Start: 10, Limit: 2}})
	require.Empty(t, items)
	require.Equal(t, int64(4), page.TotalItems)
}

func TestNormalizeSelectionInternal(t *testing.T) {
	eligible := BuildEntries([]string{"folder/a.txt", "folder/nested/b.txt", "other.txt"}, "", true)
	selected, err := NormalizeSelection(backuptypes.RestoreSelection{Paths: []string{"folder/nested/b.txt", "folder", "folder/a.txt", "folder"}}, eligible)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "folder", Name: "folder", IsDirectory: true}}, selected)

	_, err = NormalizeSelection(backuptypes.RestoreSelection{Paths: []string{"../arcane.db"}}, eligible)
	require.ErrorContains(t, err, "restore root")
	_, err = NormalizeSelection(backuptypes.RestoreSelection{Paths: []string{"missing"}}, eligible)
	require.ErrorContains(t, err, "does not exist")
	_, err = NormalizeSelection(backuptypes.RestoreSelection{Paths: []string{"other.txt/"}}, eligible)
	require.ErrorContains(t, err, "not a folder")
	_, err = NormalizeSelection(backuptypes.RestoreSelection{Paths: []string{"other.txt"}, SelectAll: true}, eligible)
	require.ErrorContains(t, err, "cannot be combined")
	_, err = NormalizeSelection(backuptypes.RestoreSelection{}, eligible)
	require.ErrorContains(t, err, "paths are required")
}

func TestNormalizeSearchSelectionCoversDescendantsInternal(t *testing.T) {
	eligible := BuildEntries([]string{"match/a.txt", "match/nested/b.txt", "other.txt"}, "", true)
	selected, err := NormalizeSelection(backuptypes.RestoreSelection{SelectAll: true, Search: "match"}, eligible)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "match", Name: "match", IsDirectory: true}}, selected)
}
