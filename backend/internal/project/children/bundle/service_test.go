package bundle

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

// stoppedEnvironment reports the project stopped until startAfter project reads, as if something started it mid-export.
type stoppedEnvironment struct {
	Environment

	uploads    *upload.UploadService
	reads      atomic.Int32
	startAfter int32
}

func (e *stoppedEnvironment) Project(context.Context, string) (types.Project, error) {
	running := 0
	if e.reads.Add(1) > e.startAfter {
		running = 1
	}
	return types.Project{ID: "p1", Name: "demo", RunningCount: running}, nil
}

func (e *stoppedEnvironment) ExportProject(ctx context.Context, _ string) (types.UploadSession, error) {
	session, err := e.uploads.IngestSession(ctx, types.UploadKindProjectArchive, "demo.tar.gz", 5, strings.NewReader("files"))
	return *session, err
}

func (e *stoppedEnvironment) ExportVolume(ctx context.Context, _ string) (types.UploadSession, error) {
	session, err := e.uploads.IngestSession(ctx, types.UploadKindVolumeBackup, "data.tar.gz", 4, strings.NewReader("data"))
	return *session, err
}

func TestExportFailsWhenTheProjectStartsDuringTheCopy(t *testing.T) {
	archive, err := projects.ProjectBundleVolumeArchive("data")
	require.NoError(t, err)
	manifest := types.ProjectManifest{
		Project:       types.ProjectManifestProject{ID: "p1", Name: "demo", ComposeName: "demo"},
		Volumes:       []types.ProjectVolume{{Key: "data", Name: "demo_data", Kind: types.ProjectVolumeNamed, Archive: archive, Included: true}},
		FormatVersion: types.ProjectManifestFormatVersion,
	}
	opts := ExportOptions{ProjectID: "p1", Request: types.ExportProjectArchive{Volumes: []string{"data"}, StopProject: true}}
	for _, test := range []struct {
		name       string
		startAfter int32
		conflict   bool
	}{
		{name: "a project that stays stopped is exported", startAfter: 2},
		// A volume backup that had stopped the project finishes and starts it while the volume is copied.
		{name: "a project started during the copy fails the export", startAfter: 1, conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			uploads := upload.NewUploadService(nil)
			service := &Service{deps: Dependencies{Uploads: uploads, Environment: &stoppedEnvironment{uploads: uploads, startAfter: test.startAfter}}}
			writeErr := service.writeBundle(t.Context(), io.Discard, opts, manifest, false)
			if test.conflict {
				require.ErrorIs(t, writeErr, common.ErrConflict)
				require.ErrorContains(t, writeErr, "started while its volumes were copied")
				return
			}
			require.NoError(t, writeErr)
		})
	}
}
