package workspace

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func intPointer(value int) *int { return &value }

func TestEffectiveMaxFileSizeMB(t *testing.T) {
	require.Equal(t, DefaultMaxFileSizeMB, EffectiveMaxFileSizeMB(0))
	require.Equal(t, DefaultMaxFileSizeMB, EffectiveMaxFileSizeMB(-1))
	require.Equal(t, 24, EffectiveMaxFileSizeMB(24))
	require.Equal(t, int64(24*1024*1024), MaxFileSizeBytes(24))
}

func TestValidateTextContent(t *testing.T) {
	limit := int64(4)
	require.NoError(t, ValidateTextContent([]byte("text"), limit))
	require.ErrorContains(t, ValidateTextContent([]byte("large"), limit), "file exceeds")
	require.ErrorContains(t, ValidateTextContent([]byte{0xff}, limit), "UTF-8")
	require.ErrorContains(t, ValidateTextContent([]byte{'a', 0}, limit), "NUL")
}

func TestIsTextContent(t *testing.T) {
	require.True(t, IsTextContent([]byte("hello\n")))
	require.False(t, IsTextContent([]byte{0xff}))
	require.False(t, IsTextContent([]byte("hello\x00world")))
}

func TestValidateTextContentUsesDynamicMiBLimit(t *testing.T) {
	limit := int64(2 * 1024 * 1024)
	err := ValidateTextContent([]byte(strings.Repeat("a", int(limit+1))), limit)
	require.ErrorContains(t, err, "2 MiB")
}

func TestValidateUploadIndices(t *testing.T) {
	valid := []UploadReference{
		{Operation: "create_file", UploadIndex: intPointer(1)},
		{Operation: "create_folder"},
		{Operation: "update_file", UploadIndex: intPointer(0)},
	}
	require.NoError(t, ValidateUploadIndices(valid, 2, "create_file", "update_file"))

	tests := []struct {
		name        string
		changes     []UploadReference
		uploadCount int
		message     string
	}{
		{name: "missing", changes: []UploadReference{{Operation: "create_file"}}, uploadCount: 0, message: "required"},
		{name: "unexpected", changes: []UploadReference{{Operation: "delete", UploadIndex: intPointer(0)}}, uploadCount: 1, message: "not allowed"},
		{name: "negative", changes: []UploadReference{{Operation: "create_file", UploadIndex: intPointer(-1)}}, uploadCount: 1, message: "out of range"},
		{name: "out of range", changes: []UploadReference{{Operation: "create_file", UploadIndex: intPointer(1)}}, uploadCount: 1, message: "out of range"},
		{name: "duplicate", changes: []UploadReference{{Operation: "create_file", UploadIndex: intPointer(0)}, {Operation: "update_file", UploadIndex: intPointer(0)}}, uploadCount: 1, message: "duplicated"},
		{name: "unused", changes: []UploadReference{{Operation: "create_file", UploadIndex: intPointer(0)}}, uploadCount: 2, message: "unused"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateUploadIndices(test.changes, test.uploadCount, "create_file", "update_file")
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestValidateUpdateManifest(t *testing.T) {
	require.NoError(t, ValidateUpdateManifest("revision", 1, 500))
	require.ErrorContains(t, ValidateUpdateManifest(" ", 1, 500), "revision")
	require.ErrorContains(t, ValidateUpdateManifest("revision", 0, 500), "between 1 and 500")
	require.ErrorContains(t, ValidateUpdateManifest("revision", 501, 500), "between 1 and 500")
}
