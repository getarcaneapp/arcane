package podman

import (
	"archive/tar"
	"errors"
	"io"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func readTar(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	files := map[string]string{}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files
		}
		require.NoError(t, err)
		assert.Equal(t, int64(0o644), header.Mode)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		files[header.Name] = string(data)
	}
}

func TestBuildTar(t *testing.T) {
	archive, err := buildTar([]types.QuadletFile{
		{Name: "web.kube", Content: "[Kube]\nYaml=web.yaml\n"},
		{Name: "web.yaml", Content: "apiVersion: v1\nkind: Pod\n"},
	})
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"web.kube": "[Kube]\nYaml=web.yaml\n",
		"web.yaml": "apiVersion: v1\nkind: Pod\n",
	}, readTar(t, archive))
}

func TestBuildTarRejectsInvalidNames(t *testing.T) {
	tests := []struct {
		name  string
		files []types.QuadletFile
	}{
		{name: "empty", files: []types.QuadletFile{{Name: ""}}},
		{name: "parent", files: []types.QuadletFile{{Name: ".."}}},
		{name: "nested path", files: []types.QuadletFile{{Name: "../web.container"}}},
		{name: "absolute path", files: []types.QuadletFile{{Name: "/etc/web.container"}}},
		{name: "duplicate", files: []types.QuadletFile{{Name: "web.container"}, {Name: "web.container"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildTar(tt.files)
			require.ErrorIs(t, err, common.ErrBadRequest)
		})
	}
}
