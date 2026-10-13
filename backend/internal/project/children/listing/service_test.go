package listing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/updater/labels"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
)

func TestCalculateProjectStatus(t *testing.T) {
	tests := []struct {
		name     string
		services []project.RuntimeService
		want     string
	}{
		{
			name:     "empty",
			services: []project.RuntimeService{},
			want:     project.StatusUnknown,
		},
		{
			name: "all running",
			services: []project.RuntimeService{
				{Status: "running"},
				{Status: "up"},
			},
			want: project.StatusRunning,
		},
		{
			name: "all stopped",
			services: []project.RuntimeService{
				{Status: "exited"},
				{Status: "stopped"},
			},
			want: project.StatusStopped,
		},
		{
			name: "partial",
			services: []project.RuntimeService{
				{Status: "running"},
				{Status: "exited"},
			},
			want: project.StatusPartiallyRunning,
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

	counts := svc.StatusCounts(t.Context(), []project.Record{
		{Status: project.StatusRunning},
		{Status: project.StatusStopped},
		{Status: project.StatusUnknown},
		{Status: project.StatusRunning, IsArchived: true},
	})

	assert.Equal(t, project.StatusCounts{TotalProjects: 4, ArchivedProjects: 1, RunningProjects: 1, StoppedProjects: 1}, counts)
}

func TestMapProjectToDto_SetsRedeployDisabledFromRuntimeServices(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "arcane")
	now := time.Now()
	proj := project.Record{
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
			}, currentContainerID: tt.currentContainerID, currentContainerErr: tt.currentErr})

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
			p := project.Record{ID: tt.name, Name: tt.name, Path: t.TempDir(), UpdatedAt: &now, BuildImageRefsJSON: tt.buildImageRefsJSON}
			assert.Equal(t, tt.want, row(projectsDirectory, iconcatalog.DefaultCatalog, p, Snapshot{}).HasBuildDirective)
		})
	}
}

func TestServiceCounts(t *testing.T) {
	tests := []struct {
		name        string
		services    []project.RuntimeService
		wantTotal   int
		wantRunning int
	}{
		{
			name: "mixed status",
			services: []project.RuntimeService{
				{Name: "s1", Status: "running"},
				{Name: "s2", Status: "exited"},
				{Name: "s3", Status: "up"},
			},
			wantTotal:   3,
			wantRunning: 2,
		},
		{
			name: "all stopped",
			services: []project.RuntimeService{
				{Name: "s1", Status: "exited"},
			},
			wantTotal:   1,
			wantRunning: 0,
		},
		{
			name:        "empty",
			services:    []project.RuntimeService{},
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

func TestApplyPresentation_ProjectLinks(t *testing.T) {
	links := []project.Link{{URL: "https://example.com"}, {URL: "https://example.com/docs", Label: "Documentation"}}
	details := []project.Details{{URLs: []string{"https://stale.example.com"}}}
	ApplyPresentation(t.Context(), t.TempDir(), iconcatalog.DefaultCatalog,
		[]project.Record{{Path: t.TempDir()}}, details,
		[]projects.ArcaneComposeMetadata{{ProjectLinks: links}})
	assert.Equal(t, links, details[0].Links)
	assert.Equal(t, []string{"https://example.com", "https://example.com/docs"}, details[0].URLs) //nolint:staticcheck // Preserve the deprecated URLs field for v2 clients.
}
