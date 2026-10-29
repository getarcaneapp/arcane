package workspace

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveMaxFileSizeMB(t *testing.T) {
	require.Equal(t, DefaultMaxFileSizeMB, EffectiveMaxFileSizeMB(0))
	require.Equal(t, DefaultMaxFileSizeMB, EffectiveMaxFileSizeMB(-1))
	require.Equal(t, 24, EffectiveMaxFileSizeMB(24))
	require.Equal(t, int64(24*1024*1024), MaxFileSizeBytes(24))
}

func TestValidateContentSize(t *testing.T) {
	limit := int64(4)
	require.NoError(t, ValidateContentSize([]byte("text"), limit))
	// Any bytes are allowed: binary content is valid as long as it fits.
	require.NoError(t, ValidateContentSize([]byte{0xff, 0, 0x1f}, limit))
	require.ErrorContains(t, ValidateContentSize([]byte("large"), limit), "file exceeds")
}

func TestIsTextContent(t *testing.T) {
	require.True(t, IsTextContent([]byte("hello\n")))
	require.False(t, IsTextContent([]byte{0xff}))
	require.False(t, IsTextContent([]byte("hello\x00world")))
}

func TestValidateContentSizeUsesDynamicMiBLimit(t *testing.T) {
	limit := int64(2 * 1024 * 1024)
	err := ValidateContentSize([]byte(strings.Repeat("a", int(limit+1))), limit)
	require.ErrorContains(t, err, "2 MiB")
}

func TestValidateUploadIndices(t *testing.T) {
	valid := []UploadReference{
		{Operation: "create_file", UploadIndex: new(1)},
		{Operation: "create_folder"},
		{Operation: "update_file", UploadIndex: new(0)},
	}
	require.NoError(t, ValidateUploadIndices(valid, 2, "create_file", "update_file"))

	tests := []struct {
		name        string
		message     string
		changes     []UploadReference
		uploadCount int
	}{
		{name: "missing", changes: []UploadReference{{Operation: "create_file"}}, uploadCount: 0, message: "required"},
		{name: "unexpected", changes: []UploadReference{{Operation: "delete", UploadIndex: new(0)}}, uploadCount: 1, message: "not allowed"},
		{name: "negative", changes: []UploadReference{{Operation: "create_file", UploadIndex: new(-1)}}, uploadCount: 1, message: "out of range"},
		{name: "out of range", changes: []UploadReference{{Operation: "create_file", UploadIndex: new(1)}}, uploadCount: 1, message: "out of range"},
		{
			name: "duplicate",
			changes: []UploadReference{
				{
					Operation:   "create_file",
					UploadIndex: new(0),
				},
				{
					Operation:   "update_file",
					UploadIndex: new(0),
				},
			},
			uploadCount: 1,
			message:     "duplicated",
		},
		{name: "unused", changes: []UploadReference{{Operation: "create_file", UploadIndex: new(0)}}, uploadCount: 2, message: "unused"},
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
