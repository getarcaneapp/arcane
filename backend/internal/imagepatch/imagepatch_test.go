package imagepatch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

func TestResolvePatchedRef(t *testing.T) {
	tests := []struct {
		name       string
		imageRef   string
		patchedTag string
		suffix     string
		want       string
	}{
		{
			name:     "default suffix",
			imageRef: "nginx:1.25",
			suffix:   "patched",
			want:     "docker.io/library/nginx:1.25-patched",
		},
		{
			name:       "explicit tag override with registry port",
			imageRef:   "registry.local:5000/app:2.0",
			patchedTag: "2.0-hardened",
			suffix:     "patched",
			want:       "registry.local:5000/app:2.0-hardened",
		},
		{
			name:     "custom suffix",
			imageRef: "ghcr.io/acme/api:v3",
			suffix:   "fixed",
			want:     "ghcr.io/acme/api:v3-fixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePatchedRef(tt.imageRef, tt.patchedTag, tt.suffix)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestListPatchTargets_ExcludesUntaggedImages(t *testing.T) {
	ctx := context.Background()
	dsn := fmt.Sprintf("file:image-patch-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&vulnerability.VulnerabilityScanRecord{}, &vulnerability.VulnerabilityReportRecord{}, &ImagePatchRecord{}))
	db := &database.DB{DB: gdb}

	svc := &ImagePatchService{
		db:            db,
		dockerService: docker.NewDockerClientService(ctx, nil, &config.Config{DockerHost: "unix:///nonexistent-arcane-test.sock"}, nil),
	}

	fixable := 3
	records := []vulnerability.VulnerabilityScanRecord{
		{ID: "sha256:aaa", ImageName: "nginx:latest", Status: vulnerability.ScanStatusCompleted, ScanTime: time.Now(), FixableCount: &fixable},
		{ID: "sha256:bbb", ImageName: "sha256:bbb", Status: vulnerability.ScanStatusCompleted, ScanTime: time.Now(), FixableCount: &fixable},
		{ID: "sha256:ccc", ImageName: "<none>:<none>", Status: vulnerability.ScanStatusCompleted, ScanTime: time.Now(), FixableCount: &fixable},
	}
	for i := range records {
		require.NoError(t, db.Create(&records[i]).Error)
		require.NoError(t, db.Create(&vulnerability.VulnerabilityReportRecord{ImageID: records[i].ID, Data: "{}"}).Error)
	}

	targets, _, err := svc.ListPatchTargets(ctx, "0", pagination.QueryParams{Limit: 20})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "nginx:latest", targets[0].ImageRef)
}
