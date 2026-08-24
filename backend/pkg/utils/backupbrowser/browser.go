// Package backupbrowser provides deterministic backup-tree construction,
// paging, and restore-selection validation.
package backupbrowser

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"emperror.dev/errors"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
)

// NormalizePath validates and normalizes a path relative to a backup root.
func NormalizePath(value string, allowEmpty bool) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if trimmed == "" {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("backup path is required")
	}
	if path.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || slices.Contains(strings.Split(trimmed, "/"), "..") {
		return "", errors.New("backup path must stay within the restore root")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("backup path is required")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("backup path must stay within the restore root")
	}
	return cleaned, nil
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
			if candidate == root {
				continue
			}
			prefix := root + "/"
			if !strings.HasPrefix(candidate, prefix) {
				continue
			}
			relative = strings.TrimPrefix(candidate, prefix)
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
	SortEntries(result)
	return result
}

// SortEntries orders directories first and then uses case-insensitive name and path ordering.
func SortEntries(entries []backuptypes.BackupFileEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.IsDirectory != right.IsDirectory {
			return left.IsDirectory
		}
		leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
		if leftName != rightName {
			return leftName < rightName
		}
		leftPath, rightPath := strings.ToLower(left.Path), strings.ToLower(right.Path)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return left.Path < right.Path
	})
}

// Page filters entries by full relative path and returns one cursor-like page.
func Page(entries []backuptypes.BackupFileEntry, search string, start, limit int) backuptypes.BackupFilePage {
	query := strings.ToLower(strings.TrimSpace(search))
	filtered := entries
	if query != "" {
		filtered = make([]backuptypes.BackupFileEntry, 0, len(entries))
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Path), query) {
				filtered = append(filtered, entry)
			}
		}
	}
	if start < 0 {
		start = 0
	}
	if start >= len(filtered) || limit <= 0 {
		return backuptypes.BackupFilePage{Entries: []backuptypes.BackupFileEntry{}}
	}
	end := min(start+limit, len(filtered))
	page := backuptypes.BackupFilePage{Entries: slices.Clone(filtered[start:end])}
	if end < len(filtered) {
		page.NextStart = &end
	}
	return page
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
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" || slices.Contains(strings.Split(trimmed, "/"), "..") {
		return "", false
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
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
