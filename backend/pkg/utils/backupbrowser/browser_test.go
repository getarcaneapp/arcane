package backupbrowser

import (
	"testing"

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

func TestPageWalkReachesEveryEntryInternal(t *testing.T) {
	entries := BuildEntries([]string{"d/", "a.txt", "b.txt", "c.txt", "d/e.txt"}, "", true)
	var walked []backuptypes.BackupFileEntry
	start := 0
	for {
		page := Page(entries, "", start, 2)
		walked = append(walked, page.Entries...)
		if page.NextStart == nil {
			break
		}
		start = *page.NextStart
	}
	require.Equal(t, entries, walked)
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
}

func TestNormalizeSearchSelectionCoversDescendantsInternal(t *testing.T) {
	eligible := BuildEntries([]string{"match/a.txt", "match/nested/b.txt", "other.txt"}, "", true)
	selected, err := NormalizeSelection(backuptypes.RestoreSelection{SelectAll: true, Search: "match"}, eligible)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "match", Name: "match", IsDirectory: true}}, selected)
}
