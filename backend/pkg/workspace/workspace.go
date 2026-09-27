package workspace

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"emperror.dev/errors"
	kit "go.getarcane.app/kit/pkg"
)

const DefaultMaxFileSizeMB = 10

type UploadReference struct {
	Operation     string
	UploadIndex   *int
	BaselineIndex *int
}

func EffectiveMaxFileSizeMB(configured int) int {
	return kit.Ternary(configured <= 0, DefaultMaxFileSizeMB, configured)
}

func MaxFileSizeBytes(configuredMB int) int64 {
	return int64(EffectiveMaxFileSizeMB(configuredMB)) * 1024 * 1024
}

func ValidateUpdateManifest(fileTreeRevision string, fileChangeCount, maxFileChanges int) error {
	if strings.TrimSpace(fileTreeRevision) == "" {
		return errors.New("workspace revision is required")
	}
	if fileChangeCount == 0 || fileChangeCount > maxFileChanges {
		return errors.Errorf("fileChanges must contain between 1 and %d changes", maxFileChanges)
	}
	return nil
}

// ValidateContentSize enforces the per-file workspace upload limit. Content
// may be any bytes: text and binary files are both allowed.
func ValidateContentSize(content []byte, maxBytes int64) error {
	if int64(len(content)) > maxBytes {
		return errors.Errorf("file exceeds %d MiB limit", maxBytes/(1024*1024))
	}
	return nil
}

func IsTextContent(content []byte) bool {
	return utf8.Valid(content) && bytes.IndexByte(content, 0) < 0
}

func ValidateUploadIndices(changes []UploadReference, uploadCount int, createFileOperation, updateFileOperation string) error {
	used := make(map[int]struct{}, uploadCount)
	for _, change := range changes {
		requiresUpload := change.Operation == createFileOperation || change.Operation == updateFileOperation
		if requiresUpload && change.UploadIndex == nil {
			return errors.Errorf("uploadIndex is required for %s", change.Operation)
		}
		if !requiresUpload && change.UploadIndex != nil {
			return errors.Errorf("uploadIndex is not allowed for %s", change.Operation)
		}
		if change.UploadIndex == nil {
			continue
		}
		index := *change.UploadIndex
		if index < 0 || index >= uploadCount {
			return errors.Errorf("uploadIndex %d is out of range", index)
		}
		if _, exists := used[index]; exists {
			return errors.Errorf("uploadIndex %d is duplicated", index)
		}
		used[index] = struct{}{}
	}
	for _, change := range changes {
		if change.BaselineIndex == nil {
			continue
		}
		if change.Operation != updateFileOperation {
			return errors.Errorf("baselineIndex is not allowed for %s", change.Operation)
		}
		index := *change.BaselineIndex
		if index < 0 || index >= uploadCount {
			return errors.Errorf("baselineIndex %d is out of range", index)
		}
		if _, exists := used[index]; exists {
			return errors.Errorf("baselineIndex %d is duplicated", index)
		}
		used[index] = struct{}{}
	}
	if len(used) != uploadCount {
		return errors.New("one or more uploaded files are unused")
	}
	return nil
}
