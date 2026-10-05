package snapshots

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/recovery"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestSnapshotSourceFilesSkipLiveAndProjectFiles(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, ".arcane-snapshot-stage")
	require.NoError(t, os.Mkdir(stage, 0o700))
	for _, name := range []string{RecoveryRequestName, "settings.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("data"), 0o600))
	}
	require.NoError(t, os.Symlink("settings.txt", filepath.Join(root, "link")))
	db, err := sql.Open("sqlite", filepath.Join(root, "arcane.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), "PRAGMA journal_mode=WAL; CREATE TABLE evidence (id TEXT); INSERT INTO evidence VALUES ('committed')")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(root, "arcane.db"), 0o640))
	projects := filepath.Join(root, "projects")
	require.NoError(t, os.Mkdir(projects, 0o700))
	composePath := filepath.Join(projects, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte("services: {}"), 0o600))
	layout := backupSourceLayout{
		dataDirectory:     root,
		databaseName:      "arcane.db",
		projectsDirectory: projects,
		projectsPath:      snapshotDataPath + "/projects",
		excludes:          []string{".arcane-snapshot-*", RecoveryRequestName, "arcane.db-wal", "arcane.db-shm", "arcane.db-journal"},
	}
	type attributes struct {
		size    int64
		mode    os.FileMode
		modTime time.Time
	}
	walk := func() map[string]attributes {
		files, walkErr := snapshotSourceFiles(t.Context(), layout)
		require.NoError(t, walkErr)
		result := make(map[string]attributes, len(files))
		for filePath, info := range files {
			result[filePath] = attributes{size: info.Size(), mode: info.Mode(), modTime: info.ModTime()}
		}
		return result
	}
	before := walk()
	require.NotContains(t, before, projects)
	require.NotContains(t, before, filepath.Join(root, "arcane.db"))
	require.NotContains(t, before, filepath.Join(root, "arcane.db-wal"))
	require.NotContains(t, before, stage)
	require.Contains(t, before, filepath.Join(root, "settings.txt"))
	require.NoError(t, os.WriteFile(composePath, []byte("services: {app: {image: nginx}}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projects, "app.log"), []byte("live"), 0o600))
	require.NoError(t, stageDatabase(t.Context(), db, filepath.Join(root, "arcane.db"), filepath.Join(stage, "arcane.db")))
	require.Equal(t, before, walk())
	require.NoError(t, os.WriteFile(filepath.Join(root, "settings.txt"), []byte("changed"), 0o600))
	require.NotEqual(t, before, walk())
	info, err := os.Stat(filepath.Join(stage, "arcane.db"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	original, err := os.Stat(filepath.Join(root, "arcane.db"))
	require.NoError(t, err)
	require.Equal(t, original.Sys().(*syscall.Stat_t).Uid, info.Sys().(*syscall.Stat_t).Uid)
	require.Equal(t, original.Sys().(*syscall.Stat_t).Gid, info.Sys().(*syscall.Stat_t).Gid)
	staged, err := sql.Open("sqlite", filepath.Join(stage, "arcane.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = staged.Close() })
	var id string
	require.NoError(t, staged.QueryRowContext(t.Context(), "SELECT id FROM evidence").Scan(&id))
	require.Equal(t, "committed", id)
}

func TestProjectFilesFromSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		layout   snapshotLayout
		expected []string
	}{
		{
			name: "version 1 snapshot root",
			files: []string{
				"/.arcane-recovery.json", "/arcane.db", "/arcane.db-wal", "/templates/demo.yaml",
				"/projects/demo/docker-compose.yaml", "/projects/demo/.env", "/projects/demo/data/", "/projects/demo/.env",
			},
			layout:   snapshotLayout{dataPath: "/", projectsPath: "/projects", databaseName: "arcane.db"},
			expected: []string{"demo/.env", "demo/docker-compose.yaml"},
		},
		{
			name: "legacy snapshot and custom projects root",
			files: []string{
				"/app/data/.arcane-recovery.json", "/app/data/arcane.db", "/app/data/custom/projects/nested/app/compose.yaml",
				"/app/data/projects/ignored/compose.yaml",
			},
			layout:   snapshotLayout{dataPath: "/app/data", projectsPath: "/app/data/custom/projects", databaseName: "arcane.db"},
			expected: []string{"nested/app/compose.yaml"},
		},
		{
			name: "projects directory is data root",
			files: []string{
				"/data/.arcane-recovery.json", "/data/.arcane-recovery-request.json", "/data/custom.db", "/data/custom.db-shm", "/data/demo/config.yaml",
			},
			layout:   snapshotLayout{dataPath: "/data", projectsPath: "/data", databaseName: "custom.db"},
			expected: []string{"demo/config.yaml"},
		},
		{
			name: "external projects keep database-like names",
			files: []string{
				"/data/.arcane-recovery.json", "/data/arcane.db", "/projects/arcane.db", "/projects/demo/compose.yaml",
			},
			layout:   snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"},
			expected: []string{"arcane.db", "demo/compose.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := projectEntriesFromSnapshot(test.files, test.layout, "", true)
			actual := make([]string, 0, len(entries))
			for _, entry := range entries {
				if !entry.IsDirectory {
					actual = append(actual, entry.Path)
				}
			}
			slices.Sort(actual)
			require.Equal(t, test.expected, actual)
		})
	}
}

func TestProjectsRelativePathFromManifest(t *testing.T) {
	tests := []struct {
		name              string
		databaseURL       string
		projectsDirectory string
		expected          string
		errorContains     string
	}{
		{name: "absolute database path", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/app/data/historical", expected: "historical"},
		{name: "relative default database path", databaseURL: "file:data/arcane.db", projectsDirectory: "/app/data/historical", expected: "historical"},
		{name: "projects mapping", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/app/data/historical:/host/projects", expected: "historical"},
		{name: "data root", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/app/data", expected: ""},
		{name: "outside data", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/srv/projects", errorContains: errProjectsOutsideData.Error()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := recovery.Manifest{Environment: map[string]string{
				"DATABASE_URL":       test.databaseURL,
				"PROJECTS_DIRECTORY": test.projectsDirectory,
			}}
			relative, err := projectsRelativePathFromManifest(manifest)
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, relative)
		})
	}
}

func TestProjectEntriesFromSnapshotSynthesizesFoldersAndExcludesProtectedData(t *testing.T) {
	entries := projectEntriesFromSnapshot([]string{
		"/.arcane-recovery.json",
		"/.arcane-recovery-request.json",
		"/arcane.db",
		"/arcane.db-wal",
		"/arcane.db-shm",
		"/arcane.db-journal",
		"/demo/nested/compose.yaml",
		"/z.txt",
	}, snapshotLayout{dataPath: "/", projectsPath: "/", databaseName: "arcane.db"}, "", false)
	require.Equal(t, []backup.BackupFileEntry{
		{Path: "demo", Name: "demo", IsDirectory: true},
		{Path: "z.txt", Name: "z.txt"},
	}, entries)
}

func TestProjectEntriesFromSnapshotNestedBrowse(t *testing.T) {
	entries := projectEntriesFromSnapshot([]string{
		"/app/data/custom/projects/demo/nested/",
		"/app/data/custom/projects/demo/compose.yaml",
	}, snapshotLayout{dataPath: "/app/data", projectsPath: "/app/data/custom/projects", databaseName: "arcane.db"}, "demo", false)
	require.Equal(t, []backup.BackupFileEntry{
		{Path: "demo/nested", Name: "nested", IsDirectory: true},
		{Path: "demo/compose.yaml", Name: "compose.yaml"},
	}, entries)
}

func TestNormalizeSystemBackupSelection(t *testing.T) {
	snapshot := systemBackupSnapshot{
		layout: snapshotLayout{dataPath: "/data", projectsPath: "/data/historical", databaseName: "arcane.db"},
		entries: []backup.BackupFileEntry{
			{Path: "demo", Name: "demo", IsDirectory: true},
			{Path: "demo/compose.yaml", Name: "compose.yaml"},
		},
	}
	selected, err := normalizeSystemBackupSelection(backup.RestoreSelection{SelectAll: true}, snapshot)
	require.NoError(t, err)
	require.Equal(t, []backup.BackupFileEntry{{Path: "", Name: "historical", IsDirectory: true}}, selected)

	_, err = normalizeSystemBackupSelection(
		backup.RestoreSelection{SelectAll: true, Paths: []string{"demo"}},
		snapshot,
	)
	require.ErrorContains(t, err, "cannot be combined")

	selected, err = normalizeSystemBackupSelection(
		backup.RestoreSelection{Paths: []string{"demo/compose.yaml", "demo"}},
		snapshot,
	)
	require.NoError(t, err)
	require.Equal(t, []backup.BackupFileEntry{{Path: "demo", Name: "demo", IsDirectory: true}}, selected)

	snapshot.layout.projectsPath = snapshot.layout.dataPath
	selected, err = normalizeSystemBackupSelection(backup.RestoreSelection{SelectAll: true}, snapshot)
	require.NoError(t, err)
	require.Equal(t, []backup.BackupFileEntry{{Path: "demo", Name: "demo", IsDirectory: true}}, selected)
}

func TestSnapshotLayoutFromManifest(t *testing.T) {
	legacy := map[string]string{"DATABASE_URL": "file:/app/data/arcane.db", "PROJECTS_DIRECTORY": "/app/data/projects"}
	tests := []struct {
		name          string
		manifest      recovery.Manifest
		root          string
		expected      snapshotLayout
		errorContains string
	}{
		{
			name: "version 1 projects inside data", manifest: recovery.Manifest{FormatVersion: 1, Environment: legacy}, root: "/",
			expected: snapshotLayout{dataPath: "/", projectsPath: "/projects", databaseName: "arcane.db"},
		},
		{
			name:     "version 1 projects outside data are omitted",
			manifest: recovery.Manifest{FormatVersion: 1, Environment: map[string]string{"DATABASE_URL": "file:/app/data/arcane.db", "PROJECTS_DIRECTORY": "/srv/projects"}},
			root:     "/app/data", expected: snapshotLayout{dataPath: "/app/data", databaseName: "arcane.db"},
		},
		{
			name: "version 2 external projects", manifest: recovery.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/projects", DatabasePath: "arcane.db"}, root: "/data",
			expected: snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"},
		},
		{
			name: "version 2 mismatched root", manifest: recovery.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/projects", DatabasePath: "arcane.db"}, root: "/",
			errorContains: "records data path /data but was found at /",
		},
		{
			name: "version 2 traversal", manifest: recovery.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/../etc", DatabasePath: "arcane.db"}, root: "/data",
			errorContains: "invalid projects path",
		},
		{
			name: "version 2 malformed database path", manifest: recovery.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/data", DatabasePath: ""}, root: "/data",
			errorContains: "invalid database path",
		},
		{name: "unsupported version", manifest: recovery.Manifest{FormatVersion: 3}, root: "/data", errorContains: "unsupported format 3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout, err := snapshotLayoutFromManifest(test.manifest, test.root)
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, layout)
		})
	}
}

func TestSnapshotLayoutProtectsDataFiles(t *testing.T) {
	overlapping := snapshotLayout{dataPath: "/data", projectsPath: "/data", databaseName: "arcane.db"}
	require.True(t, overlapping.protected("arcane.db-wal"))
	require.True(t, overlapping.protected(".arcane-recovery.json"))
	require.False(t, overlapping.protected("demo/arcane.db"))
	nested := snapshotLayout{dataPath: "/data", projectsPath: "/data/projects", databaseName: "arcane.db"}
	require.False(t, nested.protected("arcane.db"))
	external := snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
	require.False(t, external.protected(".arcane-recovery.json"))
	require.False(t, snapshotLayout{dataPath: "/data"}.projectsIncluded())
}

func TestProjectsSnapshotPath(t *testing.T) {
	tests := []struct {
		name, data, projects, expected string
		external                       bool
		errorContains                  string
	}{
		{name: "inside data", data: "/app/data", projects: "/app/data/projects", expected: "/data/projects"},
		{name: "nested deeper", data: "/app/data", projects: "/app/data/custom/projects/", expected: "/data/custom/projects"},
		{name: "equal to data", data: "/app/data", projects: "/app/data", expected: "/data"},
		{name: "external bind", data: "/app/data", projects: "/host/path/to/projects", expected: "/projects", external: true},
		{name: "sibling with shared prefix", data: "/app/data", projects: "/app/datasets", expected: "/projects", external: true},
		{name: "ancestor of data", data: "/app/data", projects: "/app", errorContains: "contains Arcane's data directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectsPath, external, err := projectsSnapshotPath(test.data, test.projects)
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, projectsPath)
			require.Equal(t, test.external, external)
		})
	}
}

func TestSourceMounts(t *testing.T) {
	mounts := []container.MountPoint{
		{Type: mount.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mount.TypeBind, Source: "/host/projects", Destination: "/app/data/projects", RW: true},
		{Type: mount.TypeBind, Source: "/host/external", Destination: "/srv/projects", RW: true},
	}
	data, err := sourceMounts(mounts, true, "/app/data", "/data")
	require.NoError(t, err)
	require.Equal(t, []mount.Mount{
		{Type: mount.TypeVolume, Source: "arcane-data", Target: "/data", ReadOnly: true},
		{Type: mount.TypeBind, Source: "/host/projects", Target: "/data/projects", ReadOnly: true},
	}, data)

	external, err := sourceMounts(mounts, true, "/srv/projects", "/projects")
	require.NoError(t, err)
	require.Equal(t, []mount.Mount{{Type: mount.TypeBind, Source: "/host/external", Target: "/projects", ReadOnly: true}}, external)

	_, err = sourceMounts(mounts, true, "/opt/projects", "/projects")
	require.ErrorContains(t, err, "/opt/projects must be mounted into the Arcane container")

	host, err := sourceMounts(nil, false, "/tmp/data", "/data")
	require.NoError(t, err)
	require.Equal(t, []mount.Mount{{Type: mount.TypeBind, Source: "/tmp/data", Target: "/data", ReadOnly: true}}, host)
}

func TestNestedDataPath(t *testing.T) {
	tests := []struct {
		name          string
		mounts        []container.MountPoint
		expected      string
		nested        bool
		errorContains string
	}{
		{
			name: "data directory inside projects bind on the host",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/volume4/docker/dsm/arcane/data", Destination: "/app/data", RW: true},
				{Type: mount.TypeBind, Source: "/volume4/docker/dsm", Destination: "/volume4/docker/dsm", RW: true},
			},
			expected: "arcane/data",
			nested:   true,
		},
		{
			name: "same host directory",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/shared", Destination: "/app/data", RW: true},
				{Type: mount.TypeBind, Source: "/host/shared/", Destination: "/volume4/docker/dsm", RW: true},
			},
			nested:        true,
			errorContains: "same host directory",
		},
		{
			name: "sibling with shared prefix",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/projects-data", Destination: "/app/data", RW: true},
				{Type: mount.TypeBind, Source: "/host/projects", Destination: "/volume4/docker/dsm", RW: true},
			},
		},
		{
			name: "unrelated binds",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/data", Destination: "/app/data", RW: true},
				{Type: mount.TypeBind, Source: "/host/projects", Destination: "/volume4/docker/dsm", RW: true},
			},
		},
		{
			name: "volume data mount beside bind projects",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
				{Type: mount.TypeBind, Source: "/host/projects", Destination: "/volume4/docker/dsm", RW: true},
			},
		},
		{
			name: "data subpath inside the projects volume",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "shared", Destination: "/volume4/docker/dsm", RW: true},
				{Type: mount.TypeVolume, Name: "shared", Destination: "/app/data", RW: true},
			},
			nested:        true,
			errorContains: "same host directory",
		},
		{
			name: "different volumes",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
				{Type: mount.TypeVolume, Name: "projects", Destination: "/volume4/docker/dsm", RW: true},
			},
		},
		{name: "host development without mounts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			relative, nested, err := nestedDataPath(test.mounts, "/app/data", "/volume4/docker/dsm")
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.nested, nested)
			require.Equal(t, test.expected, relative)
		})
	}
}

func TestRestoreTarget(t *testing.T) {
	mounts := []container.MountPoint{
		{Type: mount.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mount.TypeBind, Source: "/host/projects", Destination: "/app/data/projects", RW: true},
		{Type: mount.TypeBind, Source: "/host/templates", Destination: "/app/data/templates", RW: true},
		{Type: mount.TypeBind, Source: "/host/ro", Destination: "/mnt/ro", RW: false},
	}
	target, err := restoreTarget(mounts, "/app/data/custom/projects", "/restore-projects", "")
	require.NoError(t, err)
	require.Equal(t, "/restore-projects/custom/projects", target.Path)
	require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: "arcane-data", Target: "/restore-projects"}}, target.Mounts)

	data, err := restoreTarget(mounts, "/app/data", "/restore", "/app/data/projects")
	require.NoError(t, err)
	require.Equal(t, "/restore", data.Path)
	require.Equal(t, []mount.Mount{
		{Type: mount.TypeVolume, Source: "arcane-data", Target: "/restore"},
		{Type: mount.TypeBind, Source: "/host/templates", Target: "/restore/templates"},
	}, data.Mounts)

	_, err = restoreTarget(mounts, "/mnt/ro/projects", "/restore-projects", "")
	require.ErrorContains(t, err, "mounted read-only")
	_, err = restoreTarget(mounts, "/opt/projects", "/restore-projects", "")
	require.ErrorContains(t, err, "must be mounted into the Arcane container")
}

func TestRestoreStages(t *testing.T) {
	mounts := []container.MountPoint{
		{Type: mount.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mount.TypeBind, Source: "/host/nested", Destination: "/app/data/projects", RW: true},
		{Type: mount.TypeBind, Source: "/host/external", Destination: "/srv/projects", RW: true},
	}
	repository := recovery.RestoreRepository{Environment: []string{"RUSTIC_REPOSITORY=/repository"}}
	dataVolume := mount.Mount{Type: mount.TypeVolume, Source: "arcane-data", Target: "/restore"}
	nested := mount.Mount{Type: mount.TypeBind, Source: "/host/nested", Target: "/restore/projects"}

	t.Run("projects covered by data restore", func(t *testing.T) {
		layout := snapshotLayout{dataPath: "/data", projectsPath: "/data/projects", databaseName: "arcane.db"}
		stages, err := restoreStages(mounts, "/app/data", "/app/data/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Equal(
			t,
			[]recovery.RestoreStage{
				{
					Repository: repository,
					SnapshotID: "snap",
					SourcePath: "/data",
					Target: recovery.RestoreTarget{
						Mounts: []mount.Mount{
							dataVolume,
							nested,
						},
						Path: "/restore",
					},
				},
			},
			stages,
		)
	})
	t.Run("external projects restore separately", func(t *testing.T) {
		layout := snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		stages, err := restoreStages(mounts, "/app/data", "/srv/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 2)
		require.Equal(t, "/data", stages[0].SourcePath)
		require.Equal(t, []mount.Mount{dataVolume, nested}, stages[0].Target.Mounts)
		require.Equal(t, "/projects", stages[1].SourcePath)
		require.Equal(
			t,
			recovery.RestoreTarget{
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeBind,
						Source: "/host/external",
						Target: "/restore-projects",
					},
				},
				Path: "/restore-projects",
			},
			stages[1].Target,
		)
	})
	t.Run("changed projects directory excludes the nested mount from the data stage", func(t *testing.T) {
		layout := snapshotLayout{dataPath: "/", projectsPath: "/projects", databaseName: "arcane.db"}
		stages, err := restoreStages(mounts, "/app/data", "/app/data/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 1)
		require.Equal(t, []mount.Mount{dataVolume, nested}, stages[0].Target.Mounts)

		external := snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		stages, err = restoreStages(mounts, "/app/data", "/app/data/projects", repository, "snap", external)
		require.NoError(t, err)
		require.Len(t, stages, 2)
		require.Equal(t, []mount.Mount{dataVolume}, stages[0].Target.Mounts)
		require.Equal(
			t,
			recovery.RestoreTarget{
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeBind,
						Source: "/host/nested",
						Target: "/restore-projects",
					},
				},
				Path: "/restore-projects",
			},
			stages[1].Target,
		)

		stages, err = restoreStages(mounts, "/app/data", "/app/data/custom/projects", repository, "snap", external)
		require.NoError(t, err)
		require.Len(t, stages, 2)
		require.Equal(t, []mount.Mount{dataVolume, nested}, stages[0].Target.Mounts)
		require.Equal(
			t,
			recovery.RestoreTarget{
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeVolume,
						Source: "arcane-data",
						Target: "/restore-projects",
					},
				},
				Path: "/restore-projects/custom/projects",
			},
			stages[1].Target,
		)
	})
	t.Run("version 1 backup without projects restores data only", func(t *testing.T) {
		layout := snapshotLayout{dataPath: "/app/data", databaseName: "arcane.db"}
		stages, err := restoreStages(mounts, "/app/data", "/srv/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 1)
		require.Equal(t, "/app/data", stages[0].SourcePath)
	})
	t.Run("projects at data root cannot move", func(t *testing.T) {
		layout := snapshotLayout{dataPath: "/data", projectsPath: "/data", databaseName: "arcane.db"}
		_, err := restoreStages(mounts, "/app/data", "/srv/projects", repository, "snap", layout)
		require.ErrorContains(t, err, "keeps projects in Arcane's data directory")
		stages, err := restoreStages(mounts, "/app/data", "/app/data", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 1)
	})
	t.Run("unmounted destination fails", func(t *testing.T) {
		layout := snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		_, err := restoreStages(mounts, "/app/data", "/opt/projects", repository, "snap", layout)
		require.ErrorContains(t, err, "/opt/projects must be mounted")
	})
	t.Run("data directory inside projects on the host fails", func(t *testing.T) {
		hostNested := []container.MountPoint{
			{Type: mount.TypeBind, Source: "/volume4/docker/dsm/arcane/data", Destination: "/app/data", RW: true},
			{Type: mount.TypeBind, Source: "/volume4/docker/dsm", Destination: "/volume4/docker/dsm", RW: true},
		}
		layout := snapshotLayout{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		_, err := restoreStages(hostNested, "/app/data", "/volume4/docker/dsm", repository, "snap", layout)
		require.ErrorContains(t, err, "contains Arcane's data directory")
	})
}

func TestSafetySnapshotContainsPath(t *testing.T) {
	safety := systemBackupSafetySnapshot{paths: map[string]struct{}{"demo": {}, "demo/compose.yaml": {}}}
	require.True(t, safetySnapshotContainsPath(safety, ""))
	require.True(t, safetySnapshotContainsPath(safety, "demo"))
	require.True(t, safetySnapshotContainsPath(safety, "demo/compose.yaml"))
	require.False(t, safetySnapshotContainsPath(safety, "other"))
}

func TestLegacyLayoutOmittedProjectsIsActionable(t *testing.T) {
	require.Contains(t, errProjectsNotInBackup.Error(), "create a new system backup")
}
