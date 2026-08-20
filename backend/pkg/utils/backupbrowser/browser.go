// Package backupbrowser provides deterministic backup-tree construction,
// paging, and restore-selection validation.
package backupbrowser

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
)

// NormalizePath validates and normalizes a path relative to a backup root.
func NormalizePath(value string, allowEmpty bool) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if trimmed == "" || path.Clean(trimmed) == "." {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("backup path is required")
	}
	// Reject ".." segments before cleaning: "a/../b" must not sneak through as "b".
	if slices.Contains(strings.Split(trimmed, "/"), "..") {
		return "", errors.New("backup path must stay within the restore root")
	}
	cleaned, err := utils.NormalizeRelativePath(trimmed)
	if err != nil {
		return "", errors.New("backup path must stay within the restore root")
	}
	return cleaned, nil
}

// ListScope resolves the snapshot listing scope for one browse request: the
// whole tree when searching, otherwise just the requested folder.
func ListScope(requestedPath string, params pagination.QueryParams) (listPath string, recursive bool, err error) {
	browsePath, err := NormalizePath(requestedPath, true)
	if err != nil || params.Start < 0 {
		return "", false, errors.New("invalid browse path or start")
	}
	if strings.TrimSpace(params.Search) != "" {
		return "", true, nil
	}
	return browsePath, false, nil
}

// BuildEntries converts archive or Rustic path output into a synthesized tree.
// Paths are returned relative to the supplied logical root.
func BuildEntries(paths []string, browsePath string, recursive bool) []backuptypes.BackupFileEntry {
	root, err := NormalizePath(browsePath, true)
	if err != nil {
		return nil
	}
	entries := make(map[string]backuptypes.BackupFileEntry)
	for _, raw := range paths {
		directory := strings.HasSuffix(strings.TrimSpace(raw), "/")
		candidate, ok := normalizeListedPathInternal(raw)
		if !ok {
			continue
		}
		relative := candidate
		if root != "" {
			if candidate == root || !utils.FilePathMatches(candidate, root) {
				continue
			}
			relative = strings.TrimPrefix(candidate, root+"/")
		}
		segments := strings.Split(relative, "/")
		if len(segments) == 0 || segments[0] == "" {
			continue
		}
		if !recursive && len(segments) > 1 {
			addEntryInternal(entries, path.Join(root, segments[0]), true)
			continue
		}
		if recursive {
			for index := 1; index < len(segments); index++ {
				addEntryInternal(entries, path.Join(root, path.Join(segments[:index]...)), true)
			}
		}
		addEntryInternal(entries, candidate, directory)
	}
	result := make([]backuptypes.BackupFileEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	slices.SortStableFunc(result, compareEntriesInternal)
	return result
}

func compareEntriesInternal(left, right backuptypes.BackupFileEntry) int {
	if left.IsDirectory != right.IsDirectory {
		if left.IsDirectory {
			return -1
		}
		return 1
	}
	if result := cmp.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)); result != 0 {
		return result
	}
	if result := cmp.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path)); result != 0 {
		return result
	}
	return cmp.Compare(left.Path, right.Path)
}

// Browse searches, orders, and paginates backup entries using the standard pagination contract.
func Browse(entries []backuptypes.BackupFileEntry, params pagination.QueryParams) ([]backuptypes.BackupFileEntry, pagination.Response) {
	config := pagination.Config[backuptypes.BackupFileEntry]{
		SearchAccessors: []pagination.SearchAccessor[backuptypes.BackupFileEntry]{
			func(entry backuptypes.BackupFileEntry) (string, error) { return entry.Path, nil },
		},
		SortBindings: []pagination.SortBinding[backuptypes.BackupFileEntry]{
			{Key: "path", Fn: compareEntriesInternal},
		},
	}
	result := config.SearchOrderAndPaginate(entries, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)
}

// NormalizeSelection validates explicit selections against eligible entries,
// deduplicates them, and removes descendants covered by selected directories.
func NormalizeSelection(selection backuptypes.RestoreSelection, eligible []backuptypes.BackupFileEntry) ([]backuptypes.BackupFileEntry, error) {
	if selection.SelectAll && len(selection.Paths) > 0 {
		return nil, errors.New("selectAll cannot be combined with explicit paths")
	}
	if !selection.SelectAll && len(selection.Paths) == 0 {
		return nil, errors.New("paths are required")
	}
	candidates, err := selectionCandidatesInternal(selection, eligible)
	if err != nil {
		return nil, err
	}
	return collapseSelectionCandidatesInternal(candidates), nil
}

func selectionCandidatesInternal(selection backuptypes.RestoreSelection, eligible []backuptypes.BackupFileEntry) ([]backuptypes.BackupFileEntry, error) {
	if selection.SelectAll {
		query := strings.ToLower(strings.TrimSpace(selection.Search))
		candidates := make([]backuptypes.BackupFileEntry, 0, len(eligible))
		for _, entry := range eligible {
			if query == "" || strings.Contains(strings.ToLower(entry.Path), query) {
				candidates = append(candidates, entry)
			}
		}
		if len(candidates) == 0 {
			return nil, errors.New("no backup paths match the selection")
		}
		return candidates, nil
	}
	return explicitSelectionCandidatesInternal(selection.Paths, eligible)
}

func explicitSelectionCandidatesInternal(requestedPaths []string, eligible []backuptypes.BackupFileEntry) ([]backuptypes.BackupFileEntry, error) {
	byPath := make(map[string]backuptypes.BackupFileEntry, len(eligible))
	for _, entry := range eligible {
		byPath[entry.Path] = entry
	}
	candidates := make([]backuptypes.BackupFileEntry, 0, len(requestedPaths))
	seen := make(map[string]struct{}, len(requestedPaths))
	for _, requested := range requestedPaths {
		expectsDirectory := strings.HasSuffix(strings.TrimSpace(strings.ReplaceAll(requested, `\`, "/")), "/")
		normalized, err := NormalizePath(requested, false)
		if err != nil {
			return nil, err
		}
		entry, exists := byPath[normalized]
		if !exists {
			return nil, fmt.Errorf("%s does not exist in this backup", normalized)
		}
		if expectsDirectory && !entry.IsDirectory {
			return nil, fmt.Errorf("%s is a file, not a folder", normalized)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, entry)
	}
	return candidates, nil
}

func collapseSelectionCandidatesInternal(candidates []backuptypes.BackupFileEntry) []backuptypes.BackupFileEntry {
	sort.SliceStable(candidates, func(i, j int) bool {
		leftDepth := strings.Count(candidates[i].Path, "/")
		rightDepth := strings.Count(candidates[j].Path, "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		leftPath := strings.ToLower(candidates[i].Path)
		rightPath := strings.ToLower(candidates[j].Path)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return candidates[i].Path < candidates[j].Path
	})
	result := make([]backuptypes.BackupFileEntry, 0, len(candidates))
	selectedDirectories := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		covered := false
		for _, directory := range selectedDirectories {
			if strings.HasPrefix(candidate.Path, directory+"/") {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		result = append(result, candidate)
		if candidate.IsDirectory {
			selectedDirectories = append(selectedDirectories, candidate.Path)
		}
	}
	return result
}

func normalizeListedPathInternal(raw string) (string, bool) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
	cleaned, err := NormalizePath(strings.TrimPrefix(strings.TrimSuffix(trimmed, "/"), "/"), false)
	if err != nil {
		return "", false
	}
	return cleaned, true
}

func addEntryInternal(entries map[string]backuptypes.BackupFileEntry, entryPath string, directory bool) {
	if entryPath == "" || entryPath == "." {
		return
	}
	current, exists := entries[entryPath]
	if exists && (current.IsDirectory || !directory) {
		return
	}
	entries[entryPath] = backuptypes.BackupFileEntry{
		Path:        entryPath,
		Name:        path.Base(entryPath),
		IsDirectory: directory,
	}
}
