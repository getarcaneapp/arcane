package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ref "go.podman.io/image/v5/docker/reference"
	"gorm.io/gorm"
)

// TestParseImageReference tests the parseImageReference function with various image formats
// This is used for digest-based update checking
func TestImageUpdateService_ParseImageReference(t *testing.T) {
	tests := []struct {
		name           string
		imageRef       string
		wantRegistry   string
		wantRepository string
		wantTag        string
	}{
		{
			name:           "Docker Hub official image with tag",
			imageRef:       "redis:latest",
			wantRegistry:   "docker.io",
			wantRepository: "library/redis",
			wantTag:        "latest",
		},
		{
			name:           "Docker Hub official image without tag",
			imageRef:       "nginx",
			wantRegistry:   "docker.io",
			wantRepository: "library/nginx",
			wantTag:        "latest",
		},
		{
			name:           "Docker Hub user image",
			imageRef:       "traefik/traefik:v2.10",
			wantRegistry:   "docker.io",
			wantRepository: "traefik/traefik",
			wantTag:        "v2.10",
		},
		{
			name:           "Custom registry with port",
			imageRef:       "localhost:5000/myapp:v1.0",
			wantRegistry:   "localhost:5000",
			wantRepository: "myapp",
			wantTag:        "v1.0",
		},
		{
			name:           "Custom registry with subdomain",
			imageRef:       "docker.getoutline.com/outlinewiki/outline:latest",
			wantRegistry:   "docker.getoutline.com",
			wantRepository: "outlinewiki/outline",
			wantTag:        "latest",
		},
		{
			name:           "GCR image",
			imageRef:       "gcr.io/google-containers/nginx:1.21",
			wantRegistry:   "gcr.io",
			wantRepository: "google-containers/nginx",
			wantTag:        "1.21",
		},
		{
			name:           "GHCR image",
			imageRef:       "ghcr.io/owner/repo:main",
			wantRegistry:   "ghcr.io",
			wantRepository: "owner/repo",
			wantTag:        "main",
		},
		{
			name:           "Multi-path repository",
			imageRef:       "registry.example.com/team/project/app:v2.0.0",
			wantRegistry:   "registry.example.com",
			wantRepository: "team/project/app",
			wantTag:        "v2.0.0",
		},
		{
			name:           "Image with digest",
			imageRef:       "alpine@sha256:1234567890abcdef",
			wantRegistry:   "docker.io",
			wantRepository: "library/alpine",
			wantTag:        "latest",
		},
		{
			name:           "Custom registry image with digest",
			imageRef:       "registry.io/app/service@sha256:abcdef123456",
			wantRegistry:   "registry.io",
			wantRepository: "app/service",
			wantTag:        "latest",
		},
	}

	svc := &ImageUpdateService{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := svc.parseImageReference(tt.imageRef)
			require.NotNil(t, parts, "parseImageReference returned nil")

			assert.Equal(t, tt.wantRegistry, parts.Registry, "registry mismatch")
			assert.Equal(t, tt.wantRepository, parts.Repository, "repository mismatch")
			assert.Equal(t, tt.wantTag, parts.Tag, "tag mismatch")
		})
	}
}

// TestParseImageReference_Fallback tests edge cases that might trigger fallback parsing
func TestImageUpdateService_ParseImageReference_Fallback(t *testing.T) {
	svc := &ImageUpdateService{}

	// Test malformed references that should still be parsed by fallback
	tests := []struct {
		name     string
		imageRef string
		wantNil  bool
	}{
		{
			name:     "Empty string",
			imageRef: "",
			wantNil:  false, // Fallback should handle it
		},
		{
			name:     "Valid reference",
			imageRef: "nginx:latest",
			wantNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := svc.parseImageReference(tt.imageRef)
			if tt.wantNil {
				assert.Nil(t, parts)
			} else {
				assert.NotNil(t, parts)
			}
		})
	}
}

// TestNormalizeRepository tests repository normalization
func TestImageUpdateService_NormalizeRepository(t *testing.T) {
	tests := []struct {
		name       string
		regHost    string
		repo       string
		wantNormal string
	}{
		{
			name:       "Docker Hub single name adds library",
			regHost:    "docker.io",
			repo:       "redis",
			wantNormal: "library/redis",
		},
		{
			name:       "Docker Hub with slash unchanged",
			regHost:    "docker.io",
			repo:       "traefik/traefik",
			wantNormal: "traefik/traefik",
		},
		{
			name:       "Custom registry unchanged",
			regHost:    "gcr.io",
			repo:       "project/app",
			wantNormal: "project/app",
		},
		{
			name:       "Custom registry single name unchanged",
			regHost:    "gcr.io",
			repo:       "nginx",
			wantNormal: "nginx",
		},
	}

	svc := &ImageUpdateService{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.normalizeRepository(tt.regHost, tt.repo)
			assert.Equal(t, tt.wantNormal, result, "repository normalization mismatch")
		})
	}
}

// TestGetLocalImageDigestWithAll_ExtractsAllDigests tests that all digests are collected
func TestImageUpdateService_GetLocalImageDigestWithAll_Logic(t *testing.T) {
	// This is a unit test for the digest extraction logic
	// In a real scenario, you'd need to mock Docker client
	t.Run("Multiple digests in RepoDigests", func(t *testing.T) {
		// This test demonstrates the expected behavior
		// In practice, you'd use a mock Docker client
		repoDigests := []string{
			"docker.io/library/redis@sha256:abc123",
			"redis@sha256:def456",
		}

		var allDigests []string
		for _, repoDigest := range repoDigests {
			parts := splitRepoDigest(repoDigest)
			if parts != nil {
				allDigests = append(allDigests, parts.digest)
			}
		}

		assert.Len(t, allDigests, 2)
		assert.Contains(t, allDigests, "sha256:abc123")
		assert.Contains(t, allDigests, "sha256:def456")
	})
}

// Helper function to test digest splitting
type repoDigestParts struct {
	repo   string
	digest string
}

func splitRepoDigest(repoDigest string) *repoDigestParts {
	parts := splitString(repoDigest, "@")
	if len(parts) == 2 {
		return &repoDigestParts{
			repo:   parts[0],
			digest: parts[1],
		}
	}
	return nil
}

func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// TestDockerReferenceCompatibility ensures our parser is compatible with Docker's reference package
func TestImageUpdateService_DockerReferenceCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
	}{
		{"Docker Hub official", "nginx:latest"},
		{"Docker Hub user", "traefik/traefik:v2.0"},
		{"Custom registry", "gcr.io/project/app:v1"},
		{"With port", "localhost:5000/app:tag"},
		{"Multi-path", "registry.io/team/project/app:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that official parser can handle it
			named, err := ref.ParseNormalizedNamed(tt.imageRef)
			require.NoError(t, err, "official parser failed")

			// Test our parser
			svc := &ImageUpdateService{}
			parts := svc.parseImageReference(tt.imageRef)
			require.NotNil(t, parts, "our parser returned nil")

			// Verify they produce the same results
			assert.Equal(t, ref.Domain(named), parts.Registry)
			assert.Equal(t, ref.Path(named), parts.Repository)
		})
	}
}

// TestStringPtrToString tests the helper function used for pointer comparison fix
func TestStringPtrToString(t *testing.T) {
	tests := []struct {
		name string
		ptr  *string
		want string
	}{
		{
			name: "nil pointer returns empty string",
			ptr:  nil,
			want: "",
		},
		{
			name: "valid pointer returns value",
			ptr:  stringToPtr("test-value"),
			want: "test-value",
		},
		{
			name: "empty string pointer returns empty string",
			ptr:  stringToPtr(""),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringPtrToString(tt.ptr)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestStringToPtr tests the helper function for creating string pointers
func TestStringToPtr(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{
			name:    "empty string returns nil",
			input:   "",
			wantNil: true,
		},
		{
			name:    "non-empty string returns pointer",
			input:   "test",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToPtr(tt.input)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.input, *result)
			}
		})
	}
}

// setupImageUpdateTestDB creates an in-memory SQLite database for testing
func setupImageUpdateTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:image-update-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(glsqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ImageUpdateRecord{}))
	return &database.DB{DB: db}
}

// TestNotificationSentLogic tests the notification_sent flag behavior
func TestImageUpdateService_NotificationSentLogic(t *testing.T) {
	db := setupImageUpdateTestDB(t)

	imageID := "sha256:test123"
	repo := "docker.io/library/nginx"
	tag := "latest"

	t.Run("new record starts with notification_sent=false", func(t *testing.T) {
		result := &imageupdate.Response{
			HasUpdate:      true,
			UpdateType:     "digest",
			CurrentVersion: "1.0",
			LatestVersion:  "2.0",
			CurrentDigest:  "sha256:old",
			LatestDigest:   "sha256:new",
			CheckTime:      time.Now(),
			ResponseTimeMs: 100,
		}

		updateRecord := buildImageUpdateRecord(imageID, repo, tag, result)

		// New record should have NotificationSent = false
		assert.False(t, updateRecord.NotificationSent)

		err := db.Create(updateRecord).Error
		require.NoError(t, err)

		// Verify it was saved correctly
		var saved models.ImageUpdateRecord
		err = db.First(&saved, "id = ?", imageID).Error
		require.NoError(t, err)
		assert.False(t, saved.NotificationSent)
	})
}

// TestNotificationSentReset tests that notification_sent resets when update state changes
func TestImageUpdateService_NotificationSentReset(t *testing.T) {
	db := setupImageUpdateTestDB(t)

	imageID := "sha256:test456"
	repo := "docker.io/library/redis"
	tag := "alpine"

	tests := []struct {
		name             string
		existingRecord   *models.ImageUpdateRecord
		newResult        *imageupdate.Response
		expectNotifReset bool
		reason           string
	}{
		{
			name: "digest changed - should reset",
			existingRecord: &models.ImageUpdateRecord{
				ID:               imageID,
				Repository:       repo,
				Tag:              tag,
				HasUpdate:        true,
				UpdateType:       "digest",
				CurrentVersion:   "7.0",
				LatestDigest:     stringToPtr("sha256:old"),
				NotificationSent: true,
			},
			newResult: &imageupdate.Response{
				HasUpdate:      true,
				UpdateType:     "digest",
				CurrentVersion: "7.0",
				LatestDigest:   "sha256:new",
				CheckTime:      time.Now(),
				ResponseTimeMs: 50,
			},
			expectNotifReset: true,
			reason:           "digest changed from old to new",
		},
		{
			name: "version changed - should reset",
			existingRecord: &models.ImageUpdateRecord{
				ID:               imageID,
				Repository:       repo,
				Tag:              tag,
				HasUpdate:        true,
				UpdateType:       "tag",
				CurrentVersion:   "7.0",
				LatestVersion:    stringToPtr("7.0.1"),
				NotificationSent: true,
			},
			newResult: &imageupdate.Response{
				HasUpdate:      true,
				UpdateType:     "tag",
				CurrentVersion: "7.0",
				LatestVersion:  "7.0.2",
				CheckTime:      time.Now(),
				ResponseTimeMs: 50,
			},
			expectNotifReset: true,
			reason:           "version changed from 7.0.1 to 7.0.2",
		},
		{
			name: "update state changed - should reset",
			existingRecord: &models.ImageUpdateRecord{
				ID:               imageID,
				Repository:       repo,
				Tag:              tag,
				HasUpdate:        false,
				UpdateType:       "digest",
				CurrentVersion:   "7.0",
				NotificationSent: true,
			},
			newResult: &imageupdate.Response{
				HasUpdate:      true,
				UpdateType:     "digest",
				CurrentVersion: "7.0",
				CheckTime:      time.Now(),
				ResponseTimeMs: 50,
			},
			expectNotifReset: true,
			reason:           "HasUpdate changed from false to true",
		},
		{
			name: "nothing changed - should keep flag",
			existingRecord: &models.ImageUpdateRecord{
				ID:               imageID,
				Repository:       repo,
				Tag:              tag,
				HasUpdate:        true,
				UpdateType:       "digest",
				CurrentVersion:   "7.0",
				LatestDigest:     stringToPtr("sha256:same"),
				LatestVersion:    stringToPtr("7.0.1"),
				NotificationSent: true,
			},
			newResult: &imageupdate.Response{
				HasUpdate:      true,
				UpdateType:     "digest",
				CurrentVersion: "7.0",
				LatestDigest:   "sha256:same",
				LatestVersion:  "7.0.1",
				CheckTime:      time.Now(),
				ResponseTimeMs: 50,
			},
			expectNotifReset: false,
			reason:           "nothing changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing record
			db.Exec("DELETE FROM image_updates WHERE id = ?", imageID)

			// Insert existing record
			err := db.Create(tt.existingRecord).Error
			require.NoError(t, err)

			// Verify it was marked as notified
			var check models.ImageUpdateRecord
			err = db.First(&check, "id = ?", imageID).Error
			require.NoError(t, err)
			assert.True(t, check.NotificationSent, "existing record should be marked as notified")

			// Simulate comparison logic from saveUpdateResultByID
			updateRecord := buildImageUpdateRecord(imageID, repo, tag, tt.newResult)

			var existingRecord models.ImageUpdateRecord
			err = db.Where("id = ?", imageID).First(&existingRecord).Error
			require.NoError(t, err)

			// This is the logic we're testing - comparing string values not pointers
			stateChanged := existingRecord.HasUpdate != updateRecord.HasUpdate
			digestChanged := stringPtrToString(existingRecord.LatestDigest) != stringPtrToString(updateRecord.LatestDigest)
			versionChanged := stringPtrToString(existingRecord.LatestVersion) != stringPtrToString(updateRecord.LatestVersion)

			if stateChanged || (updateRecord.HasUpdate && (digestChanged || versionChanged)) {
				updateRecord.NotificationSent = false
			} else {
				updateRecord.NotificationSent = existingRecord.NotificationSent
			}

			// Save the updated record
			err = db.Save(updateRecord).Error
			require.NoError(t, err)

			// Verify the result
			var updated models.ImageUpdateRecord
			err = db.First(&updated, "id = ?", imageID).Error
			require.NoError(t, err)

			if tt.expectNotifReset {
				assert.False(t, updated.NotificationSent, "notification_sent should be reset because: %s", tt.reason)
			} else {
				assert.True(t, updated.NotificationSent, "notification_sent should be preserved because: %s", tt.reason)
			}
		})
	}
}

// TestGetUnnotifiedUpdates tests retrieving updates that haven't been notified
func TestImageUpdateService_GetUnnotifiedUpdates(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)
	svc := &ImageUpdateService{db: db}

	// Create test records
	records := []models.ImageUpdateRecord{
		{
			ID:               "sha256:img1",
			Repository:       "nginx",
			Tag:              "latest",
			HasUpdate:        true,
			NotificationSent: false,
		},
		{
			ID:               "sha256:img2",
			Repository:       "redis",
			Tag:              "alpine",
			HasUpdate:        true,
			NotificationSent: true, // Already notified
		},
		{
			ID:               "sha256:img3",
			Repository:       "postgres",
			Tag:              "14",
			HasUpdate:        false, // No update
			NotificationSent: false,
		},
		{
			ID:               "sha256:img4",
			Repository:       "traefik",
			Tag:              "latest",
			HasUpdate:        true,
			NotificationSent: false,
		},
	}

	for _, rec := range records {
		err := db.Create(&rec).Error
		require.NoError(t, err)
	}

	// Get unnotified updates
	unnotified, err := svc.GetUnnotifiedUpdates(ctx)
	require.NoError(t, err)

	// Should only return img1 and img4 (has_update=true AND notification_sent=false)
	assert.Len(t, unnotified, 2, "should return 2 unnotified updates")
	assert.Contains(t, unnotified, "sha256:img1")
	assert.Contains(t, unnotified, "sha256:img4")
	assert.NotContains(t, unnotified, "sha256:img2", "img2 already notified")
	assert.NotContains(t, unnotified, "sha256:img3", "img3 has no update")
}

// TestMarkUpdatesAsNotified tests marking images as notified
func TestImageUpdateService_MarkUpdatesAsNotified(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)
	svc := &ImageUpdateService{db: db}

	// Create test records
	imageIDs := []string{"sha256:img1", "sha256:img2", "sha256:img3"}
	for _, id := range imageIDs {
		rec := models.ImageUpdateRecord{
			ID:               id,
			Repository:       "test/repo",
			Tag:              "latest",
			HasUpdate:        true,
			NotificationSent: false,
		}
		err := db.Create(&rec).Error
		require.NoError(t, err)
	}

	// Mark img1 and img2 as notified
	err := svc.MarkUpdatesAsNotified(ctx, []string{"sha256:img1", "sha256:img2"})
	require.NoError(t, err)

	// Verify img1 and img2 are marked
	var img1 models.ImageUpdateRecord
	err = db.First(&img1, "id = ?", "sha256:img1").Error
	require.NoError(t, err)
	assert.True(t, img1.NotificationSent)

	var img2 models.ImageUpdateRecord
	err = db.First(&img2, "id = ?", "sha256:img2").Error
	require.NoError(t, err)
	assert.True(t, img2.NotificationSent)

	// Verify img3 is still false
	var img3 models.ImageUpdateRecord
	err = db.First(&img3, "id = ?", "sha256:img3").Error
	require.NoError(t, err)
	assert.False(t, img3.NotificationSent)
}

// TestMarkUpdatesAsNotified_EmptyList tests handling of empty ID list
func TestImageUpdateService_MarkUpdatesAsNotified_EmptyList(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)
	svc := &ImageUpdateService{db: db}

	// Should not error on empty list
	err := svc.MarkUpdatesAsNotified(ctx, []string{})
	require.NoError(t, err)

	err = svc.MarkUpdatesAsNotified(ctx, nil)
	require.NoError(t, err)
}

func TestImageUpdateService_GetUpdateSummaryForImageIDs_FiltersToLiveImages(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)
	svc := &ImageUpdateService{db: db}
	now := time.Now()

	records := []models.ImageUpdateRecord{
		{
			ID:             "sha256:live-1",
			Repository:     "docker.io/library/nginx",
			Tag:            "latest",
			HasUpdate:      true,
			UpdateType:     "digest",
			CurrentVersion: "latest",
			CheckTime:      now,
		},
		{
			ID:             "sha256:live-2",
			Repository:     "docker.io/library/redis",
			Tag:            "latest",
			HasUpdate:      false,
			UpdateType:     "digest",
			CurrentVersion: "latest",
			LastError:      stringToPtr("rate limited"),
			CheckTime:      now,
		},
		{
			ID:             "sha256:stale-1",
			Repository:     "docker.io/library/postgres",
			Tag:            "latest",
			HasUpdate:      true,
			UpdateType:     "digest",
			CurrentVersion: "latest",
			LastError:      stringToPtr("stale failure"),
			CheckTime:      now,
		},
	}
	for i := range records {
		err := db.Create(&records[i]).Error
		require.NoError(t, err)
	}

	summary, err := svc.getUpdateSummaryForImageIDsInternal(ctx, []string{"sha256:live-1", "sha256:live-2"})
	require.NoError(t, err)

	assert.Equal(t, 2, summary.TotalImages)
	assert.Equal(t, 1, summary.ImagesWithUpdates)
	assert.Equal(t, 1, summary.DigestUpdates)
	assert.Equal(t, 1, summary.ErrorsCount)
}

func TestImageUpdateService_GetUpdateSummaryForImageIDs_EmptyLiveSet(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)
	svc := &ImageUpdateService{db: db}

	summary, err := svc.getUpdateSummaryForImageIDsInternal(ctx, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, summary.TotalImages)
	assert.Equal(t, 0, summary.ImagesWithUpdates)
	assert.Equal(t, 0, summary.DigestUpdates)
	assert.Equal(t, 0, summary.ErrorsCount)
}
