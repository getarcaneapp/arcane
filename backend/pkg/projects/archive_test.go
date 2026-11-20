package projects

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

type archiveTestEntry struct {
	header tar.Header
	body   string
}

func buildTestArchive(t *testing.T, entries []archiveTestEntry) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := entry.header
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		require.NoError(t, tarWriter.WriteHeader(&header))
		_, err := tarWriter.Write([]byte(entry.body))
		require.NoError(t, err)
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return &buffer
}

func TestProjectDirectoryArchive(t *testing.T) {
	source := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600))

	files := map[string]os.FileMode{
		"compose.yaml":                          0o600,
		"scripts/run.sh":                        0o755,
		"nested/settings.json":                  0o644,
		"nested/sub/settings.json":              0o644,
		".project-update-backup-1/compose.yaml": 0o644,
		GitSourceEnvFileName:                    0o600,
	}
	for name, mode := range files {
		fullPath := filepath.Join(source, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte("content of "+name), mode))
		require.NoError(t, os.Chmod(fullPath, mode))
	}
	require.NoError(t, os.WriteFile(filepath.Join(source, "trailing.txt "), []byte("content of trailing.txt "), 0o644))
	require.NoError(t, os.Symlink("nested/sub", filepath.Join(source, "alias")))
	require.NoError(t, os.Symlink("alias/../settings.json", filepath.Join(source, "alias-parent")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(source, "outside-link")))

	var archive bytes.Buffer
	require.NoError(t, PackProjectDirectory(t.Context(), source, &archive))
	destination := t.TempDir()
	require.NoError(t, UnpackProjectDirectory(t.Context(), destination, &archive))

	unpacked := []struct {
		name    string
		content string
		link    string
		mode    os.FileMode
		absent  bool
	}{
		{name: "compose.yaml", mode: 0o600, content: "content of compose.yaml"},
		{name: "scripts/run.sh", mode: 0o755, content: "content of scripts/run.sh"},
		{name: "trailing.txt ", mode: 0o644, content: "content of trailing.txt "},
		// The kernel applies ".." after following alias, so this is nested/settings.json.
		{name: "alias-parent", link: "alias/../settings.json", content: "content of nested/settings.json"},
		{name: "outside-link", absent: true},
		{name: ".project-update-backup-1", absent: true},
		{name: GitSourceEnvFileName, absent: true},
	}
	for _, want := range unpacked {
		fullPath := filepath.Join(destination, want.name)
		info, err := os.Lstat(fullPath)
		if want.absent {
			require.ErrorIs(t, err, os.ErrNotExist, want.name)
			continue
		}
		require.NoError(t, err, want.name)
		if want.link != "" {
			target, readlinkErr := os.Readlink(fullPath)
			require.NoError(t, readlinkErr)
			assert.Equal(t, want.link, target)
		} else {
			assert.Equal(t, want.mode, info.Mode(), want.name)
		}
		content, readErr := os.ReadFile(fullPath)
		require.NoError(t, readErr)
		assert.Equal(t, want.content, string(content), want.name)
	}

	file := func(name string) archiveTestEntry {
		return archiveTestEntry{header: tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644}, body: "x"}
	}
	symlink := func(name, target string) archiveTestEntry {
		return archiveTestEntry{header: tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: target, Mode: 0o777}}
	}
	sub := archiveTestEntry{header: tar.Header{Typeflag: tar.TypeDir, Name: "sub/", Mode: 0o755}}
	tooMany := make([]archiveTestEntry, maxArchiveEntries+1)
	for i := range tooMany {
		tooMany[i] = sub
	}
	unsafe := []struct {
		name    string
		wantErr string
		entries []archiveTestEntry
	}{
		{name: "parent traversal", entries: []archiveTestEntry{file("a/../../x")}, wantErr: "path traversal"},
		{name: "absolute path", entries: []archiveTestEntry{file("/etc/x")}, wantErr: "absolute paths"},
		{name: "hard link", entries: []archiveTestEntry{
			file("a"),
			{header: tar.Header{Typeflag: tar.TypeLink, Name: "b", Linkname: "a"}},
		}, wantErr: "hard links"},
		{name: "symlink escaping through a later symlink", entries: []archiveTestEntry{
			sub,
			symlink("escape", "sub/up/../outside"),
			symlink("sub/up", ".."),
		}, wantErr: "outside"},
		{name: "too many entries", entries: tooMany, wantErr: "more than"},
	}
	for _, test := range unsafe {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			project := filepath.Join(parent, "project")
			require.NoError(t, os.Mkdir(project, 0o755))

			err := UnpackProjectDirectory(t.Context(), project, buildTestArchive(t, test.entries))
			require.ErrorContains(t, err, test.wantErr)
			_, statErr := os.Lstat(filepath.Join(parent, "outside"))
			assert.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func testProjectManifest(volumes ...types.ProjectVolume) types.ProjectManifest {
	return types.ProjectManifest{
		Kind:          types.ProjectManifestKindExport,
		Project:       types.ProjectManifestProject{ID: "p1", Name: "web", ComposeName: "web"},
		Volumes:       volumes,
		FormatVersion: types.ProjectManifestFormatVersion,
	}
}

func writeTestBundle(t *testing.T, manifest types.ProjectManifest, entries map[string]string, order ...string) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := NewProjectBundleWriter(&buffer, manifest)
	require.NoError(t, err)
	for _, name := range order {
		require.NoError(t, writer.Add(name, int64(len(entries[name])), strings.NewReader(entries[name])))
	}
	require.NoError(t, writer.Close())
	return &buffer
}

func TestProjectBundleRoundTrip(t *testing.T) {
	dataArchive, err := ProjectBundleVolumeArchive("data")
	require.NoError(t, err)
	require.Equal(t, "volumes/data.tar.gz", dataArchive)
	manifest := testProjectManifest(
		types.ProjectVolume{Key: "data", Name: "web_data", Kind: types.ProjectVolumeNamed, Archive: dataArchive, Included: true},
		types.ProjectVolume{Key: "cache", Name: "web_cache", Kind: types.ProjectVolumeNamed},
		types.ProjectVolume{
			Key: "shared", Name: "shared-data", Kind: types.ProjectVolumeExternal, Archive: "volumes/other.tar.gz", Included: true,
			DockerLabels: map[string]string{"owner": "ops"},
		},
		types.ProjectVolume{Key: "other", Name: "shared-data", Kind: types.ProjectVolumeExternal, Archive: "volumes/other.tar.gz", Included: true},
	)
	entries := map[string]string{ProjectBundleProject: "project files", dataArchive: "volume data", "volumes/other.tar.gz": "shared data"}
	bundle := writeTestBundle(t, manifest, entries, ProjectBundleProject, dataArchive, "volumes/other.tar.gz")

	gzipReader, err := gzip.NewReader(bytes.NewReader(bundle.Bytes()))
	require.NoError(t, err)
	first, err := tar.NewReader(gzipReader).Next()
	require.NoError(t, err)
	assert.Equal(t, ProjectBundleManifest, first.Name)

	var visited []string
	got, ok, err := ReadProjectBundle(bytes.NewReader(bundle.Bytes()), func(name string, size int64, body io.Reader) error {
		content, readErr := io.ReadAll(body)
		require.NoError(t, readErr)
		assert.Equal(t, entries[name], string(content))
		assert.Equal(t, int64(len(content)), size)
		visited = append(visited, name)
		return nil
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, manifest.Volumes, got.Volumes)
	assert.Equal(t, []string{ProjectBundleProject, dataArchive, "volumes/other.tar.gz"}, visited)

	writer, err := NewProjectBundleWriter(io.Discard, manifest)
	require.NoError(t, err)
	for _, name := range []string{"../escape.tar.gz", "volumes/../x.tar.gz", "/abs.tar.gz", "volumes/a/b.tar.gz", "notes.txt"} {
		require.Error(t, writer.Add(name, 0, strings.NewReader("")), name)
	}
}

func TestReadProjectBundleTreatsPlainArchivesAsLegacy(t *testing.T) {
	plain := buildTestArchive(t, []archiveTestEntry{{header: tar.Header{Typeflag: tar.TypeReg, Name: "compose.yaml", Mode: 0o644}, body: "services: {}\n"}})
	_, ok, err := ReadProjectBundle(plain, nil)
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = ReadProjectBundle(strings.NewReader("not gzip"), nil)
	require.NoError(t, err)
	assert.False(t, ok)

	// A project's own file named like the manifest leaves its archive plain.
	for _, body := range []string{`{"name":"mine"}`, strings.Repeat(" ", maxProjectManifestBytes+1)} {
		own := buildTestArchive(t, []archiveTestEntry{
			{header: tar.Header{Typeflag: tar.TypeReg, Name: ProjectBundleManifest, Mode: 0o644}, body: body},
			{header: tar.Header{Typeflag: tar.TypeReg, Name: "compose.yaml", Mode: 0o644}, body: "services: {}\n"},
		})
		_, ok, err = ReadProjectBundle(own, nil)
		require.NoError(t, err)
		assert.False(t, ok)
	}
}

func TestReadProjectBundleRejectsInvalidBundles(t *testing.T) {
	dataArchive, err := ProjectBundleVolumeArchive("data")
	require.NoError(t, err)
	included := types.ProjectVolume{Key: "data", Name: "web_data", Kind: types.ProjectVolumeNamed, Archive: dataArchive, Included: true}
	manifestJSON := func(manifest types.ProjectManifest) string {
		content, marshalErr := json.Marshal(manifest)
		require.NoError(t, marshalErr)
		return string(content)
	}
	regular := func(name, body string) archiveTestEntry {
		return archiveTestEntry{header: tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o600}, body: body}
	}

	for _, tt := range []struct {
		name    string
		wantErr string
		entries []archiveTestEntry
	}{
		{
			name:    "missing included volume archive",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest(included))), regular(ProjectBundleProject, "p")},
			wantErr: "no volumes/data.tar.gz",
		},
		{
			name:    "missing project archive",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest()))},
			wantErr: "no project.tar.gz",
		},
		{
			name:    "path traversal entry",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest())), regular(ProjectBundleProject, "p"), regular("volumes/../../etc/passwd", "x")},
			wantErr: "path traversal",
		},
		{
			name:    "absolute entry",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest())), regular("/project.tar.gz", "p")},
			wantErr: "absolute paths",
		},
		{
			name:    "entry the manifest does not list",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest())), regular(ProjectBundleProject, "p"), regular(dataArchive, "x")},
			wantErr: "unexpected bundle entry",
		},
		{
			name: "manifest archive path outside volumes",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest(types.ProjectVolume{
				Key: "data", Name: "web_data", Archive: "../data.tar.gz", Included: true,
			}))), regular(ProjectBundleProject, "p")},
			wantErr: "must be archived at volumes/data.tar.gz",
		},
		{
			name: "archive shared with a differently named volume",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest(included, types.ProjectVolume{
				Key: "cache", Name: "web_cache", Archive: dataArchive, Included: true,
			}))), regular(ProjectBundleProject, "p"), regular(dataArchive, "x")},
			wantErr: "must be archived at volumes/cache.tar.gz",
		},
		{
			name: "docker labels on a named volume",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest(types.ProjectVolume{
				Key: "data", Name: "web_data", Kind: types.ProjectVolumeNamed, Archive: dataArchive, Included: true, DockerLabels: map[string]string{"a": "b"},
			}))), regular(ProjectBundleProject, "p"), regular(dataArchive, "x")},
			wantErr: "not an included external volume",
		},
		{
			name:    "docker label that is not a string",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, `{"kind":"export","formatVersion":1,"volumes":[{"key":"data","dockerLabels":{"a":1}}]}`), regular(ProjectBundleProject, "p")},
			wantErr: "dockerLabels",
		},
		{
			name: "oversized docker labels",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest(types.ProjectVolume{
				Key: "data", Name: "shared-data", Kind: types.ProjectVolumeExternal, Archive: dataArchive, Included: true,
				DockerLabels: map[string]string{"a": strings.Repeat("x", maxVolumeLabelBytes)},
			}))), regular(ProjectBundleProject, "p"), regular(dataArchive, "x")},
			wantErr: "labels exceed",
		},
		{
			name: "manifest key escaping volumes",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, manifestJSON(testProjectManifest(types.ProjectVolume{
				Key: "../data", Name: "web_data", Archive: "volumes/../data.tar.gz", Included: true,
			}))), regular(ProjectBundleProject, "p")},
			wantErr: "cannot name a bundle entry",
		},
		{
			name:    "unsupported format version",
			entries: []archiveTestEntry{regular(ProjectBundleManifest, `{"kind":"export","formatVersion":2}`), regular(ProjectBundleProject, "p")},
			wantErr: "unsupported project bundle format version 2",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, readErr := ReadProjectBundle(buildTestArchive(t, tt.entries), nil)
			assert.True(t, ok)
			require.ErrorContains(t, readErr, tt.wantErr)
		})
	}
}
