package s3

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	s3config "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/s3"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

var (
	ErrS3DestinationNotFound = errors.New("S3 destination not found")
	ErrS3DestinationInUse    = errors.New("S3 destination is in use")
	// errS3SecretRequiredInternal enforces the fresh-secret rule: a stored
	// secret is never combined with changed connection or trust settings.
	errS3SecretRequiredInternal = errors.New("re-enter the secret access key when changing the endpoint, bucket, region, access key ID, SSL mode, or path style")
)

type S3DestinationService struct {
	db *database.DB
	// checkRemoteReferences rejects a deletion while any managed environment
	// still references the destination (or cannot be checked conclusively).
	// Nil outside the manager's module wiring.
	checkRemoteReferences func(ctx context.Context, destinationID string) error
}

func NewS3DestinationService(db *database.DB) *S3DestinationService {
	return &S3DestinationService{db: db}
}

func fromCreateDestinationInternal(input backuptypes.CreateS3Destination) s3config.Configuration {
	return s3config.Configuration{
		Name:            input.Name,
		Endpoint:        input.Endpoint,
		Bucket:          input.Bucket,
		Region:          input.Region,
		AccessKeyID:     input.AccessKeyID,
		SecretAccessKey: input.SecretAccessKey,
		Prefix:          input.Prefix,
		UseSSL:          input.UseSSL,
		ForcePathStyle:  input.ForcePathStyle,
	}.Normalized()
}

func fromUpdateDestinationInternal(input backuptypes.UpdateS3Destination) s3config.Configuration {
	return s3config.Configuration{
		Name:            input.Name,
		Endpoint:        input.Endpoint,
		Bucket:          input.Bucket,
		Region:          input.Region,
		AccessKeyID:     input.AccessKeyID,
		SecretAccessKey: input.SecretAccessKey,
		Prefix:          input.Prefix,
		UseSSL:          input.UseSSL,
		ForcePathStyle:  input.ForcePathStyle,
	}.Normalized()
}

func fromSyncDestinationInternal(input backuptypes.S3DestinationSync) s3config.Configuration {
	return s3config.Configuration{
		ID:              input.ID,
		Name:            input.Name,
		Endpoint:        input.Endpoint,
		Bucket:          input.Bucket,
		Region:          input.Region,
		AccessKeyID:     input.AccessKeyID,
		SecretAccessKey: input.SecretAccessKey,
		Prefix:          input.Prefix,
		UseSSL:          input.UseSSL,
		ForcePathStyle:  input.ForcePathStyle,
	}.Normalized()
}

func s3DestinationsToDTOsInternal(destinations []models.S3Destination) []backuptypes.S3Destination {
	result := make([]backuptypes.S3Destination, len(destinations))
	for i := range destinations {
		result[i] = destinations[i].ToDTO()
	}
	return result
}

func (s *S3DestinationService) ListS3Destinations(ctx context.Context, params pagination.QueryParams) ([]backuptypes.S3Destination, pagination.Response, error) {
	var destinations []models.S3Destination
	query := s.db.WithContext(ctx).Model(&models.S3Destination{})
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR endpoint LIKE ? OR bucket LIKE ? OR region LIKE ? OR prefix LIKE ?", pattern, pattern, pattern, pattern, pattern)
	}
	response, err := pagination.PaginateAndSortDB(params, query, &destinations)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list S3 destinations: %w", err)
	}
	return s3DestinationsToDTOsInternal(destinations), response, nil
}

func (s *S3DestinationService) ListAllS3Destinations(ctx context.Context) ([]backuptypes.S3Destination, error) {
	var destinations []models.S3Destination
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&destinations).Error; err != nil {
		return nil, fmt.Errorf("failed to list S3 destinations: %w", err)
	}
	return s3DestinationsToDTOsInternal(destinations), nil
}

func (s *S3DestinationService) getS3DestinationModelInternal(ctx context.Context, id string) (*models.S3Destination, error) {
	var destination models.S3Destination
	if err := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).First(&destination).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrS3DestinationNotFound
		}
		return nil, fmt.Errorf("failed to load S3 destination: %w", err)
	}
	return &destination, nil
}

func (s *S3DestinationService) GetS3Destination(ctx context.Context, id string) (*backuptypes.S3Destination, error) {
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := destination.ToDTO()
	return &dto, nil
}

func applyS3ConfigurationInternal(destination *models.S3Destination, configuration s3config.Configuration, encryptedSecret string) {
	destination.Name = configuration.Name
	destination.Endpoint = configuration.Endpoint
	destination.Bucket = configuration.Bucket
	destination.Region = configuration.Region
	destination.AccessKeyID = configuration.AccessKeyID
	destination.SecretAccessKey = encryptedSecret
	destination.Prefix = configuration.Prefix
	destination.UseSSL = configuration.UseSSL
	destination.ForcePathStyle = configuration.ForcePathStyle
}

func (s *S3DestinationService) CreateS3Destination(ctx context.Context, input backuptypes.CreateS3Destination) (*backuptypes.S3Destination, error) {
	configuration := fromCreateDestinationInternal(input)
	if err := configuration.Validate(true); err != nil {
		return nil, err
	}
	encryptedSecret, err := crypto.Encrypt(configuration.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt S3 secret access key: %w", err)
	}
	destination := &models.S3Destination{}
	applyS3ConfigurationInternal(destination, configuration, encryptedSecret)
	if err := s.db.WithContext(ctx).Create(destination).Error; err != nil {
		return nil, fmt.Errorf("failed to create S3 destination: %w", err)
	}
	dto := destination.ToDTO()
	return &dto, nil
}

// storedConfigurationInternal returns the persisted configuration without the
// decrypted secret, for comparing connection fields against an update.
func storedConfigurationInternal(destination *models.S3Destination) s3config.Configuration {
	return s3config.Configuration{
		ID:             destination.ID,
		Name:           destination.Name,
		Endpoint:       destination.Endpoint,
		Bucket:         destination.Bucket,
		Region:         destination.Region,
		AccessKeyID:    destination.AccessKeyID,
		Prefix:         destination.Prefix,
		UseSSL:         destination.UseSSL,
		ForcePathStyle: destination.ForcePathStyle,
	}.Normalized()
}

func (s *S3DestinationService) UpdateS3Destination(ctx context.Context, id string, input backuptypes.UpdateS3Destination) (*backuptypes.S3Destination, error) {
	configuration := fromUpdateDestinationInternal(input)
	if err := configuration.Validate(false); err != nil {
		return nil, err
	}
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	if configuration.SecretAccessKey == "" && !s3config.ConnectionFieldsEqual(storedConfigurationInternal(destination), configuration) {
		return nil, errS3SecretRequiredInternal
	}
	encryptedSecret := destination.SecretAccessKey
	if configuration.SecretAccessKey != "" {
		encryptedSecret, err = crypto.Encrypt(configuration.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt S3 secret access key: %w", err)
		}
	}
	applyS3ConfigurationInternal(destination, configuration, encryptedSecret)
	if err := s.db.WithContext(ctx).Save(destination).Error; err != nil {
		return nil, fmt.Errorf("failed to update S3 destination: %w", err)
	}
	dto := destination.ToDTO()
	return &dto, nil
}

func (s *S3DestinationService) DeleteS3Destination(ctx context.Context, id string) error {
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return err
	}
	inUse, err := s.DestinationInUse(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("%w: remove it from backup configurations and retained backups first", ErrS3DestinationInUse)
	}
	if s.checkRemoteReferences != nil {
		if err := s.checkRemoteReferences(ctx, id); err != nil {
			return fmt.Errorf("%w: %s", ErrS3DestinationInUse, err.Error())
		}
	}
	if err := s.db.WithContext(ctx).Delete(destination).Error; err != nil {
		return fmt.Errorf("failed to delete S3 destination: %w", err)
	}
	return nil
}

// DestinationInUse reports whether any local backup record, policy, or setting
// references the destination.
func (s *S3DestinationService) DestinationInUse(ctx context.Context, id string) (bool, error) {
	db := s.db.WithContext(ctx)
	counts := []*gorm.DB{
		db.Model(&models.SystemBackupRun{}).Where("s3_destination_id = ? AND remote_snapshot_id <> ''", id),
		db.Model(&models.SystemBackupPolicy{}).Where("s3_destination_id = ? AND s3_enabled = ?", id, true),
		db.Model(&models.VolumeBackup{}).Where("s3_destination_id = ? AND remote_snapshot_id <> ''", id),
		db.Model(&models.VolumeBackupPolicy{}).Where("s3_destination_id = ? AND s3_enabled = ?", id, true),
		db.Model(&models.SettingVariable{}).Where("key = ? AND value = ?", "backupS3DestinationId", id),
	}
	for _, query := range counts {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return false, fmt.Errorf("failed to check S3 destination references: %w", err)
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *S3DestinationService) S3DestinationExists(ctx context.Context, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	var count int64
	return s.db.WithContext(ctx).Model(&models.S3Destination{}).Where("id = ?", id).Count(&count).Error == nil && count == 1
}

// SyncS3Destinations replaces an agent's destination cache with the manager-owned destinations.
func (s *S3DestinationService) SyncS3Destinations(ctx context.Context, destinations []backuptypes.S3DestinationSync) error {
	var existing []models.S3Destination
	if err := s.db.WithContext(ctx).Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to load existing S3 destinations: %w", err)
	}
	existingByID := make(map[string]*models.S3Destination, len(existing))
	for i := range existing {
		existingByID[existing[i].ID] = &existing[i]
	}

	syncedIDs := make(map[string]struct{}, len(destinations))
	for _, item := range destinations {
		configuration := fromSyncDestinationInternal(item)
		if configuration.ID == "" {
			return errors.New("S3 destination ID is required")
		}
		if err := configuration.Validate(true); err != nil {
			return fmt.Errorf("invalid S3 destination %s: %w", configuration.ID, err)
		}
		encryptedSecret, err := crypto.Encrypt(configuration.SecretAccessKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt S3 secret access key for %s: %w", configuration.ID, err)
		}
		destination, exists := existingByID[configuration.ID]
		if !exists {
			destination = &models.S3Destination{BaseModel: models.BaseModel{ID: configuration.ID}}
		}
		applyS3ConfigurationInternal(destination, configuration, encryptedSecret)
		if !item.CreatedAt.IsZero() {
			destination.CreatedAt = item.CreatedAt
		}
		if item.UpdatedAt != nil {
			destination.UpdatedAt = item.UpdatedAt
		}
		if err := s.db.WithContext(ctx).Save(destination).Error; err != nil {
			return fmt.Errorf("failed to sync S3 destination %s: %w", configuration.ID, err)
		}
		syncedIDs[configuration.ID] = struct{}{}
	}

	// One undeletable destination (still referenced locally) must not abort
	// the rest of the reconciliation; report every failure after the sweep.
	var removeErr error
	for i := range existing {
		if _, ok := syncedIDs[existing[i].ID]; ok {
			continue
		}
		if err := s.DeleteS3Destination(ctx, existing[i].ID); err != nil {
			removeErr = errors.Join(removeErr, fmt.Errorf("failed to remove unsynced S3 destination %s: %w", existing[i].ID, err))
		}
	}
	return removeErr
}

// Configuration returns a destination's decrypted runtime configuration for backup domains.
func (s *S3DestinationService) Configuration(ctx context.Context, id string) (s3config.Configuration, error) {
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return s3config.Configuration{}, err
	}
	secret, err := crypto.Decrypt(destination.SecretAccessKey)
	if err != nil {
		return s3config.Configuration{}, fmt.Errorf("failed to decrypt S3 secret access key: %w", err)
	}
	return s3config.Configuration{
		ID:              destination.ID,
		Name:            destination.Name,
		Endpoint:        destination.Endpoint,
		Bucket:          destination.Bucket,
		Region:          destination.Region,
		AccessKeyID:     destination.AccessKeyID,
		SecretAccessKey: secret,
		Prefix:          destination.Prefix,
		UseSSL:          destination.UseSSL,
		ForcePathStyle:  destination.ForcePathStyle,
	}.Normalized(), nil
}

func (s *S3DestinationService) TestS3Destination(ctx context.Context, id string, input *backuptypes.UpdateS3Destination) error {
	configuration, err := s.Configuration(ctx, id)
	if err != nil {
		return err
	}
	if input != nil {
		updated := fromUpdateDestinationInternal(*input)
		updated.ID = configuration.ID
		if updated.SecretAccessKey == "" {
			// The stored secret may only sign requests against the stored
			// connection settings; any change requires a fresh secret.
			if !s3config.ConnectionFieldsEqual(configuration, updated) {
				return errS3SecretRequiredInternal
			}
			updated.SecretAccessKey = configuration.SecretAccessKey
		}
		configuration = updated
	}
	return s3config.TestConnection(ctx, configuration)
}

func (s *S3DestinationService) TestS3DestinationConfiguration(ctx context.Context, input backuptypes.CreateS3Destination) error {
	return s3config.TestConnection(ctx, fromCreateDestinationInternal(input))
}
