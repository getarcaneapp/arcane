package listing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/updater/labels"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
)

func TestCalculateProjectStatus(t *testing.T) {
	tests := []struct {
		name     string
		services []types.ProjectRuntimeService
		want     string
	}{
		{
			name:     "empty",
			services: []types.ProjectRuntimeService{},
			want:     types.ProjectStatusUnknown,
		},
		{
			name: "all running",
			services: []types.ProjectRuntimeService{
				{Status: "running"},
				{Status: "up"},
			},
			want: types.ProjectStatusRunning,
		},
		{
			name: "all stopped",
			services: []types.ProjectRuntimeService{
				{Status: "exited"},
				{Status: "stopped"},
			},
			want: types.ProjectStatusStopped,
		},
		{
			name: "partial",
			services: []types.ProjectRuntimeService{
				{Status: "running"},
				{Status: "exited"},
			},
			want: types.ProjectStatusPartiallyRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectStatus(tt.services)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatusCounts_FallsBackToStoredStatus(t *testing.T) {
	svc := New(func(context.Context) ([]container.Summary, error) { return nil, errors.New("docker unavailable") }, nil, nil)

	counts := svc.StatusCounts(t.Context(), []types.ProjectRecord{
		{Status: types.ProjectStatusRunning},
		{Status: types.ProjectStatusStopped},
		{Status: types.ProjectStatusUnknown},
		{Status: types.ProjectStatusRunning, IsArchived: true},
	})

	assert.Equal(t, types.ProjectStatusCounts{TotalProjects: 4, ArchivedProjects: 1, RunningProjects: 1, StoppedProjects: 1}, counts)
}

func TestMapProjectToDto_SetsRedeployDisabledFromRuntimeServices(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "arcane")
	now := time.Now()
	proj := types.ProjectRecord{
		Name:         "arcane-directory",
		Path:         projectPath,
		ServiceCount: 1,
		ID:           "project-arcane",
		CreatedAt:    now,
		UpdatedAt:    &now,
	}

	tests := []struct {
		name               string
		containerID        string
		currentContainerID string
		currentErr         error
		labels             map[string]string
		wantProject        bool
		wantService        bool
	}{
		{
			name:               "current Arcane server container disables project redeploy",
			containerID:        "arcane1234567890",
			currentContainerID: "arcane1234567890",
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "server",
				labels.LabelArcane:           "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:               "current legacy Arcane server container disables project redeploy",
			containerID:        "arcane1234567890",
			currentContainerID: "arcane1234567890",
			labels: map[string]string{
				"com.docker.compose.project":   "arcane",
				"com.docker.compose.service":   "server",
				labels.LabelArcaneLegacyServer: "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:        "Arcane server container fails closed when current container is unavailable",
			containerID: "arcane1234567890",
			currentErr:  errors.New("not running in docker"),
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "server",
				labels.LabelArcane:           "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:               "current Arcane agent container disables project redeploy",
			containerID:        "agent1234567890",
			currentContainerID: "agent1234567890",
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "agent",
				labels.LabelArcane:           "true",
				labels.LabelArcaneAgent:      "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:               "non Arcane container stays redeployable",
			containerID:        "regular1234567890",
			currentContainerID: "regular1234567890",
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "postgres",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.labels[api.WorkingDirLabel] = projectPath
			details := row(filepath.Dir(projectPath), iconcatalog.DefaultCatalog, proj, Snapshot{byProject: map[string][]container.Summary{
				"arcane": {
					{
						ID:     tt.containerID,
						Image:  "ghcr.io/getarcaneapp/arcane:latest",
						State:  "running",
						Status: "Up",
						Names:  []string{"/arcane-server"},
						Labels: tt.labels,
					},
					{
						ID: "unrelated-container",
						Labels: map[string]string{
							api.ProjectLabel:    "arcane",
							api.WorkingDirLabel: filepath.Join(filepath.Dir(projectPath), "unrelated"),
							api.ServiceLabel:    "other",
						},
					},
				},
			}, currentContainerID: tt.currentContainerID, currentContainerErr: tt.currentErr}, nil)

			require.Equal(t, tt.wantProject, details.RedeployDisabled)
			require.Len(t, details.RuntimeServices, 1)
			require.Equal(t, tt.containerID, details.RuntimeServices[0].ContainerID)
			require.Equal(t, tt.wantService, details.RuntimeServices[0].RedeployDisabled)
		})
	}
}

func TestProjectListRow_SeedsHasBuildDirectiveFromPersistedRefs(t *testing.T) {
	projectsDirectory := t.TempDir()

	now := time.Now()
	tests := []struct {
		name               string
		buildImageRefsJSON *string
		want               bool
	}{
		{name: "build refs persisted", buildImageRefsJSON: new(`["demo-worker"]`), want: true},
		{name: "no build services", buildImageRefsJSON: new(`[]`), want: false},
		{name: "refs never resolved", buildImageRefsJSON: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := types.ProjectRecord{ID: tt.name, Name: tt.name, Path: t.TempDir(), UpdatedAt: &now, BuildImageRefsJSON: tt.buildImageRefsJSON}
			assert.Equal(t, tt.want, row(projectsDirectory, iconcatalog.DefaultCatalog, p, Snapshot{}, nil).HasBuildDirective)
		})
	}
}

func TestServiceCounts(t *testing.T) {
	tests := []struct {
		name        string
		services    []types.ProjectRuntimeService
		wantTotal   int
		wantRunning int
	}{
		{
			name: "mixed status",
			services: []types.ProjectRuntimeService{
				{Name: "s1", Status: "running"},
				{Name: "s2", Status: "exited"},
				{Name: "s3", Status: "up"},
			},
			wantTotal:   3,
			wantRunning: 2,
		},
		{
			name: "all stopped",
			services: []types.ProjectRuntimeService{
				{Name: "s1", Status: "exited"},
			},
			wantTotal:   1,
			wantRunning: 0,
		},
		{
			name:        "empty",
			services:    []types.ProjectRuntimeService{},
			wantTotal:   0,
			wantRunning: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, running := ServiceCounts(tt.services)
			assert.Equal(t, tt.wantTotal, total)
			assert.Equal(t, tt.wantRunning, running)
		})
	}
}
