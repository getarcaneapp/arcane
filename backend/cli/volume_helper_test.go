package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunInternalVolumeHelperProbeInternal_ReportsVolumeFilesystem(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte("hello"), 0o600))

	var output bytes.Buffer
	err := runInternalVolumeHelperProbeInternal(context.Background(), dir, &output)

	require.NoError(t, err)
	var got internalVolumeHelperProbeOutput
	require.NoError(t, json.Unmarshal(output.Bytes(), &got))
	require.Equal(t, filepath.Clean(dir), got.Path)
	require.NotZero(t, got.AllocatedBytes)
	require.NotZero(t, got.AvailableBytes)
}

func TestAllocatedBytesForPathInternal_DoesNotDoubleCountHardLinks(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(source, []byte("hello"), 0o600))

	first, err := allocatedBytesForPathInternal(context.Background(), dir)
	require.NoError(t, err)
	require.NoError(t, os.Link(source, filepath.Join(dir, "linked.txt")))

	second, err := allocatedBytesForPathInternal(context.Background(), dir)

	require.NoError(t, err)
	require.LessOrEqual(t, second, first+4096)
}
