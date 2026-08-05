package workspace

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"emperror.dev/errors"
)

const DefaultMaxFileSizeMB = 10

type UploadReference struct {
	Operation   string
	UploadIndex *int
}

func EffectiveMaxFileSizeMB(configured int) int {
	if configured <= 0 {
		return DefaultMaxFileSizeMB
	}
	return configured
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

func ValidateTextContent(content []byte, maxBytes int64) error {
	if int64(len(content)) > maxBytes {
		return errors.Errorf("file exceeds %d MiB limit", maxBytes/(1024*1024))
	}
	if !utf8.Valid(content) {
		return errors.New("file must contain valid UTF-8 text")
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return errors.New("file must not contain NUL bytes")
	}
	return nil
}

func IsTextContent(content []byte) bool {
	return ValidateTextContent(content, int64(len(content))) == nil
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
	if len(used) != uploadCount {
		return errors.New("one or more uploaded files are unused")
	}
	return nil
}
