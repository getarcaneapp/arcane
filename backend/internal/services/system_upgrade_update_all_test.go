package services

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
)

func TestUpdateAllResolveResumeAction(t *testing.T) {
	now := time.Now()

	newJob := func(createdAt time.Time, versionAtStart, digestAtStart string) *models.EnvironmentUpdateJob {
		job := &models.EnvironmentUpdateJob{
			ManagerVersionAtStart: versionAtStart,
			ManagerDigestAtStart:  digestAtStart,
		}
		job.CreatedAt = createdAt
		return job
	}

	tests := []struct {
		name           string
		job            *models.EnvironmentUpdateJob
		currentVersion string
		currentDigest  string
		wantStale      bool
		wantManagerOK  bool
	}{
		{
			name:           "stale job is failed regardless of version",
			job:            newJob(now.Add(-2*time.Hour), "1.0.0", "sha256:a"),
			currentVersion: "1.1.0",
			currentDigest:  "sha256:b",
			wantStale:      true,
		},
		{
			name:           "version changed means manager upgraded",
			job:            newJob(now.Add(-5*time.Minute), "1.0.0", "sha256:a"),
			currentVersion: "1.1.0",
			currentDigest:  "sha256:a",
			wantManagerOK:  true,
		},
		{
			name:           "digest changed means manager upgraded (digest-pinned install)",
			job:            newJob(now.Add(-5*time.Minute), "latest", "sha256:a"),
			currentVersion: "latest",
			currentDigest:  "sha256:b",
			wantManagerOK:  true,
		},
		{
			name:           "nothing changed means manager upgrade did not take",
			job:            newJob(now.Add(-5*time.Minute), "1.0.0", "sha256:a"),
			currentVersion: "1.0.0",
			currentDigest:  "sha256:a",
			wantManagerOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveResumeActionInternal(tt.job, tt.currentVersion, tt.currentDigest, now)
			if got.markStale != tt.wantStale {
				t.Fatalf("markStale = %v, want %v", got.markStale, tt.wantStale)
			}
			if !tt.wantStale && got.managerSucceeded != tt.wantManagerOK {
				t.Fatalf("managerSucceeded = %v, want %v", got.managerSucceeded, tt.wantManagerOK)
			}
		})
	}
}
