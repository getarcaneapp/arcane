package s3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

func setupS3DestinationServiceTestInternal(t *testing.T) (*S3DestinationService, *gorm.DB) {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(
		&S3Destination{},
		&settings.SettingVariable{},
	))
	require.NoError(t, gormDB.Exec("CREATE TABLE system_backup_runs (s3_destination_id text, remote_snapshot_id text)").Error)
	require.NoError(t, gormDB.Exec("CREATE TABLE system_backup_policies (s3_destination_id text, s3_enabled numeric)").Error)
	require.NoError(t, gormDB.Exec("CREATE TABLE volume_backups (s3_destination_id text, remote_snapshot_id text)").Error)
	require.NoError(t, gormDB.Exec("CREATE TABLE volume_backup_policies (s3_destination_id text, s3_enabled numeric)").Error)
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-s3-destination-32bytes",
		Environment:   "test",
	})
	return NewS3DestinationService(&database.DB{DB: gormDB}), gormDB
}

func TestS3DestinationService_CRUD(t *testing.T) {
	service, gormDB := setupS3DestinationServiceTestInternal(t)
	ctx := context.Background()

	created, err := service.CreateS3Destination(ctx, backuptypes.CreateS3Destination{
		Name:            "  Cafe\u0301 offsite  ",
		Endpoint:        "https://s3.example.com",
		Bucket:          "arcane-backups",
		Region:          "eu-central-1",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "/production/",
		UseSSL:          true,
		ForcePathStyle:  true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "Café offsite", created.Name)
	require.True(t, created.SecretConfigured)
	require.Equal(t, "production", created.Prefix)

	var stored S3Destination
	require.NoError(t, gormDB.First(&stored, "id = ?", created.ID).Error)
	require.NotEqual(t, "secret-key", stored.SecretAccessKey)

	listed, page, err := service.ListS3Destinations(ctx, pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: "arcane-backups"},
		Params:      pagination.Params{Limit: 20},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, listed, 1)

	updated, err := service.UpdateS3Destination(ctx, created.ID, backuptypes.UpdateS3Destination{
		Name:           "  Primary cafe\u0301 offsite  ",
		Endpoint:       created.Endpoint,
		Bucket:         created.Bucket,
		Region:         created.Region,
		AccessKeyID:    created.AccessKeyID,
		Prefix:         created.Prefix,
		UseSSL:         created.UseSSL,
		ForcePathStyle: created.ForcePathStyle,
	})
	require.NoError(t, err)
	require.Equal(t, "Primary café offsite", updated.Name)

	var updatedStored S3Destination
	require.NoError(t, gormDB.First(&updatedStored, "id = ?", created.ID).Error)
	require.Equal(t, stored.SecretAccessKey, updatedStored.SecretAccessKey)

	require.NoError(t, service.DeleteS3Destination(ctx, created.ID))
	_, err = service.GetS3Destination(ctx, created.ID)
	require.ErrorIs(t, err, ErrS3DestinationNotFound)
}

func TestS3DestinationService_DeleteRejectsConfiguredDestination(t *testing.T) {
	service, gormDB := setupS3DestinationServiceTestInternal(t)
	ctx := context.Background()
	destination, err := service.CreateS3Destination(ctx, backuptypes.CreateS3Destination{
		Name:            "In use",
		Bucket:          "arcane-backups",
		Region:          "us-east-1",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		UseSSL:          true,
	})
	require.NoError(t, err)
	require.NoError(t, gormDB.Exec("INSERT INTO volume_backup_policies (s3_destination_id, s3_enabled) VALUES (?, 1)", destination.ID).Error)

	err = service.DeleteS3Destination(ctx, destination.ID)
	require.ErrorIs(t, err, ErrS3DestinationInUse)
}

func TestS3DestinationService_SyncS3Destinations(t *testing.T) {
	service, gormDB := setupS3DestinationServiceTestInternal(t)
	ctx := context.Background()

	legacy, err := service.CreateS3Destination(ctx, backuptypes.CreateS3Destination{
		Name:            "Legacy",
		Bucket:          "legacy-bucket",
		Region:          "us-east-1",
		AccessKeyID:     "legacy-access-key",
		SecretAccessKey: "legacy-secret",
		UseSSL:          true,
	})
	require.NoError(t, err)

	createdAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, service.SyncS3Destinations(ctx, []backuptypes.S3DestinationSync{
		{
			ID:              "destination-1",
			Name:            "Remote primary",
			Endpoint:        "https://s3.example.com",
			Bucket:          "volume-backups",
			Region:          "eu-central-1",
			AccessKeyID:     "remote-access-key",
			SecretAccessKey: "remote-secret",
			Prefix:          "/agents/",
			UseSSL:          true,
			ForcePathStyle:  true,
			CreatedAt:       createdAt,
		},
	}))

	var synced S3Destination
	require.NoError(t, gormDB.First(&synced, "id = ?", "destination-1").Error)
	require.Equal(t, "Remote primary", synced.Name)
	require.Equal(t, "agents", synced.Prefix)
	require.NotEqual(t, "remote-secret", synced.SecretAccessKey)
	secret, err := crypto.Decrypt(synced.SecretAccessKey)
	require.NoError(t, err)
	require.Equal(t, "remote-secret", secret)

	_, err = service.GetS3Destination(ctx, legacy.ID)
	require.ErrorIs(t, err, ErrS3DestinationNotFound)

	require.NoError(t, service.SyncS3Destinations(ctx, []backuptypes.S3DestinationSync{
		{
			ID:              "destination-1",
			Name:            "Remote primary updated",
			Bucket:          "updated-volume-backups",
			Region:          "eu-west-1",
			AccessKeyID:     "updated-access-key",
			SecretAccessKey: "updated-secret",
			UseSSL:          true,
		},
	}))

	require.NoError(t, gormDB.First(&synced, "id = ?", "destination-1").Error)
	require.Equal(t, "Remote primary updated", synced.Name)
	require.Equal(t, "updated-volume-backups", synced.Bucket)
	secret, err = crypto.Decrypt(synced.SecretAccessKey)
	require.NoError(t, err)
	require.Equal(t, "updated-secret", secret)
}

func TestS3DestinationService_TestS3DestinationRoundTrip(t *testing.T) {
	var object []byte
	var requestMethods []string
	expectedPathPrefix := "/arcane-backups/production/.arcane-connection-test-"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, r.Header.Get("Authorization"))
		require.True(t, strings.HasPrefix(r.URL.Path, expectedPathPrefix))
		requestMethods = append(requestMethods, r.Method)
		switch r.Method {
		case http.MethodPut:
			var err error
			object, err = io.ReadAll(r.Body)
			require.NoError(t, err)
			w.Header().Set("ETag", `"test-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Length", strconv.Itoa(len(object)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(object)
		case http.MethodDelete:
			object = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	service, _ := setupS3DestinationServiceTestInternal(t)
	require.NoError(t, service.TestS3DestinationConfiguration(context.Background(), backuptypes.CreateS3Destination{
		Name:            "Unsaved destination",
		Endpoint:        server.URL,
		Bucket:          "arcane-backups",
		Region:          "",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "production",
		UseSSL:          false,
		ForcePathStyle:  true,
	}))
	destination, err := service.CreateS3Destination(context.Background(), backuptypes.CreateS3Destination{
		Name:            "Test destination",
		Endpoint:        server.URL,
		Bucket:          "arcane-backups",
		Region:          "",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "production",
		UseSSL:          false,
		ForcePathStyle:  true,
	})
	require.NoError(t, err)

	require.NoError(t, service.TestS3Destination(context.Background(), destination.ID, nil))

	// Changing a connection field without re-supplying the secret is rejected
	// before any outbound request: the stored secret must never sign requests
	// against caller-modified connection settings.
	requestsBeforeRejection := len(requestMethods)
	err = service.TestS3Destination(context.Background(), destination.ID, &backuptypes.UpdateS3Destination{
		Name:           destination.Name,
		Endpoint:       server.URL,
		Bucket:         "edited-bucket",
		Region:         "",
		AccessKeyID:    destination.AccessKeyID,
		Prefix:         "edited",
		UseSSL:         false,
		ForcePathStyle: true,
	})
	require.ErrorContains(t, err, "re-enter the secret access key")
	require.Len(t, requestMethods, requestsBeforeRejection)

	expectedPathPrefix = "/edited-bucket/edited/.arcane-connection-test-"
	require.NoError(t, service.TestS3Destination(context.Background(), destination.ID, &backuptypes.UpdateS3Destination{
		Name:            destination.Name,
		Endpoint:        server.URL,
		Bucket:          "edited-bucket",
		Region:          "",
		AccessKeyID:     destination.AccessKeyID,
		SecretAccessKey: "secret-key",
		Prefix:          "edited",
		UseSSL:          false,
		ForcePathStyle:  true,
	}))

	require.Equal(t, []string{
		http.MethodPut, http.MethodGet, http.MethodDelete,
		http.MethodPut, http.MethodGet, http.MethodDelete,
		http.MethodPut, http.MethodGet, http.MethodDelete,
	}, requestMethods)
	require.Empty(t, object)
	persisted, err := service.GetS3Destination(context.Background(), destination.ID)
	require.NoError(t, err)
	require.Equal(t, "arcane-backups", persisted.Bucket)
	require.Equal(t, "production", persisted.Prefix)
	require.Empty(t, persisted.Region)
}

func TestListS3DestinationsByIDInternal(t *testing.T) {
	service, db := setupS3DestinationServiceTestInternal(t)
	indexed, err := service.ListS3DestinationsByID(t.Context())
	require.NoError(t, err)
	require.Empty(t, indexed)
	destination := S3Destination{Name: "Offsite", Bucket: "backups", SecretAccessKey: "encrypted"}
	require.NoError(t, db.Create(&destination).Error)
	indexed, err = service.ListS3DestinationsByID(t.Context())
	require.NoError(t, err)
	require.Len(t, indexed, 1)
	require.Equal(t, "Offsite", indexed[destination.ID].Name)
	require.Equal(t, "backups", indexed[destination.ID].Bucket)
	require.True(t, indexed[destination.ID].SecretConfigured)
	require.Empty(t, indexed["missing"].Name)
	require.NoError(t, db.Migrator().DropTable(&S3Destination{}))
	indexed, err = service.ListS3DestinationsByID(t.Context())
	require.ErrorContains(t, err, "failed to list S3 destinations")
	require.Nil(t, indexed)
}
