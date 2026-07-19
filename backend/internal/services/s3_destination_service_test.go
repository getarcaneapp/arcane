package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
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
		&models.S3Destination{},
		&models.SettingVariable{},
	))
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
		Name:            "Offsite",
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
	require.True(t, created.SecretConfigured)
	require.Equal(t, "production", created.Prefix)

	var stored models.S3Destination
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
		Name:           "Primary offsite",
		Endpoint:       created.Endpoint,
		Bucket:         created.Bucket,
		Region:         created.Region,
		AccessKeyID:    created.AccessKeyID,
		Prefix:         created.Prefix,
		UseSSL:         created.UseSSL,
		ForcePathStyle: created.ForcePathStyle,
	})
	require.NoError(t, err)
	require.Equal(t, "Primary offsite", updated.Name)

	var updatedStored models.S3Destination
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
	require.NoError(t, gormDB.Create(&models.SettingVariable{
		Key:   "backupS3DestinationId",
		Value: destination.ID,
	}).Error)

	err = service.DeleteS3Destination(ctx, destination.ID)
	require.ErrorIs(t, err, ErrS3DestinationInUse)
}
