package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

var (
	ErrS3DestinationNotFound = errors.New("S3 destination not found")
	ErrS3DestinationInUse    = errors.New("S3 destination is in use")
)

type S3DestinationService struct {
	db *database.DB
}

type s3DestinationConfiguration struct {
	S3DestinationID   string
	S3Endpoint        string
	S3Bucket          string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Prefix          string
	S3UseSSL          bool
	S3ForcePathStyle  bool
}

func NewS3DestinationService(db *database.DB) *S3DestinationService {
	return &S3DestinationService{db: db}
}

func validateS3DestinationInputInternal(name, bucket, region, accessKeyID, secretAccessKey string, requireSecret bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return errors.New("bucket is required")
	}
	if strings.TrimSpace(region) == "" {
		return errors.New("region is required")
	}
	if strings.TrimSpace(accessKeyID) == "" {
		return errors.New("access key ID is required")
	}
	if requireSecret && strings.TrimSpace(secretAccessKey) == "" {
		return errors.New("secret access key is required")
	}
	return nil
}

func s3DestinationToDTOInternal(destination *models.S3Destination) backuptypes.S3Destination {
	return backuptypes.S3Destination{
		ID:               destination.ID,
		Name:             destination.Name,
		Endpoint:         destination.Endpoint,
		Bucket:           destination.Bucket,
		Region:           destination.Region,
		AccessKeyID:      destination.AccessKeyID,
		Prefix:           destination.Prefix,
		UseSSL:           destination.UseSSL,
		ForcePathStyle:   destination.ForcePathStyle,
		SecretConfigured: strings.TrimSpace(destination.SecretAccessKey) != "",
		CreatedAt:        destination.CreatedAt,
		UpdatedAt:        destination.UpdatedAt,
	}
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
	result := make([]backuptypes.S3Destination, len(destinations))
	for i := range destinations {
		result[i] = s3DestinationToDTOInternal(&destinations[i])
	}
	return result, response, nil
}

func (s *S3DestinationService) ListAllS3Destinations(ctx context.Context) ([]backuptypes.S3Destination, error) {
	var destinations []models.S3Destination
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&destinations).Error; err != nil {
		return nil, fmt.Errorf("failed to list S3 destinations: %w", err)
	}
	result := make([]backuptypes.S3Destination, len(destinations))
	for i := range destinations {
		result[i] = s3DestinationToDTOInternal(&destinations[i])
	}
	return result, nil
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
	dto := s3DestinationToDTOInternal(destination)
	return &dto, nil
}

func (s *S3DestinationService) CreateS3Destination(ctx context.Context, input backuptypes.CreateS3Destination) (*backuptypes.S3Destination, error) {
	if err := validateS3DestinationInputInternal(input.Name, input.Bucket, input.Region, input.AccessKeyID, input.SecretAccessKey, true); err != nil {
		return nil, err
	}
	encryptedSecret, err := crypto.Encrypt(strings.TrimSpace(input.SecretAccessKey))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt S3 secret access key: %w", err)
	}
	destination := &models.S3Destination{
		Name:            strings.TrimSpace(input.Name),
		Endpoint:        strings.TrimSpace(input.Endpoint),
		Bucket:          strings.TrimSpace(input.Bucket),
		Region:          strings.TrimSpace(input.Region),
		AccessKeyID:     strings.TrimSpace(input.AccessKeyID),
		SecretAccessKey: encryptedSecret,
		Prefix:          strings.Trim(strings.TrimSpace(input.Prefix), "/"),
		UseSSL:          input.UseSSL,
		ForcePathStyle:  input.ForcePathStyle,
	}
	if err := s.db.WithContext(ctx).Create(destination).Error; err != nil {
		return nil, fmt.Errorf("failed to create S3 destination: %w", err)
	}
	dto := s3DestinationToDTOInternal(destination)
	return &dto, nil
}

func (s *S3DestinationService) UpdateS3Destination(ctx context.Context, id string, input backuptypes.UpdateS3Destination) (*backuptypes.S3Destination, error) {
	if err := validateS3DestinationInputInternal(input.Name, input.Bucket, input.Region, input.AccessKeyID, input.SecretAccessKey, false); err != nil {
		return nil, err
	}
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	destination.Name = strings.TrimSpace(input.Name)
	destination.Endpoint = strings.TrimSpace(input.Endpoint)
	destination.Bucket = strings.TrimSpace(input.Bucket)
	destination.Region = strings.TrimSpace(input.Region)
	destination.AccessKeyID = strings.TrimSpace(input.AccessKeyID)
	destination.Prefix = strings.Trim(strings.TrimSpace(input.Prefix), "/")
	destination.UseSSL = input.UseSSL
	destination.ForcePathStyle = input.ForcePathStyle
	if strings.TrimSpace(input.SecretAccessKey) != "" {
		destination.SecretAccessKey, err = crypto.Encrypt(strings.TrimSpace(input.SecretAccessKey))
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt S3 secret access key: %w", err)
		}
	}
	if err := s.db.WithContext(ctx).Save(destination).Error; err != nil {
		return nil, fmt.Errorf("failed to update S3 destination: %w", err)
	}
	dto := s3DestinationToDTOInternal(destination)
	return &dto, nil
}

func (s *S3DestinationService) DeleteS3Destination(ctx context.Context, id string) error {
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return err
	}
	var references int64
	checks := []struct {
		table           string
		requiredColumns []string
		query           string
		args            []any
	}{
		{table: "system_backup_runs", requiredColumns: []string{"s3_destination_id", "remote_key"}, query: "s3_destination_id = ? AND remote_key <> ''", args: []any{id}},
		{table: "volume_backups", requiredColumns: []string{"s3_destination_id", "remote_key"}, query: "s3_destination_id = ? AND remote_key <> ''", args: []any{id}},
		{table: "volume_backup_policies", requiredColumns: []string{"s3_destination_id", "s3_enabled"}, query: "s3_destination_id = ? AND s3_enabled = ?", args: []any{id, true}},
		{table: "settings", requiredColumns: []string{"key", "value"}, query: "key = ? AND value = ?", args: []any{"backupS3DestinationId", id}},
	}
	for _, check := range checks {
		migrator := s.db.WithContext(ctx).Migrator()
		if !migrator.HasTable(check.table) {
			continue
		}
		missingColumn := false
		for _, column := range check.requiredColumns {
			if !migrator.HasColumn(check.table, column) {
				missingColumn = true
				break
			}
		}
		if missingColumn {
			continue
		}
		var count int64
		if err := s.db.WithContext(ctx).Table(check.table).Where(check.query, check.args...).Count(&count).Error; err != nil {
			return fmt.Errorf("failed to check S3 destination references: %w", err)
		}
		references += count
	}
	if references > 0 {
		return fmt.Errorf("%w: remove it from backup configurations and retained backups first", ErrS3DestinationInUse)
	}
	if err := s.db.WithContext(ctx).Delete(destination).Error; err != nil {
		return fmt.Errorf("failed to delete S3 destination: %w", err)
	}
	return nil
}

func (s *S3DestinationService) S3DestinationExists(ctx context.Context, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	var count int64
	return s.db.WithContext(ctx).Model(&models.S3Destination{}).Where("id = ?", id).Count(&count).Error == nil && count == 1
}

func (s *S3DestinationService) configurationInternal(ctx context.Context, id string) (s3DestinationConfiguration, error) {
	destination, err := s.getS3DestinationModelInternal(ctx, id)
	if err != nil {
		return s3DestinationConfiguration{}, err
	}
	secret, err := crypto.Decrypt(destination.SecretAccessKey)
	if err != nil {
		return s3DestinationConfiguration{}, fmt.Errorf("failed to decrypt S3 secret access key: %w", err)
	}
	return s3DestinationConfiguration{
		S3DestinationID:   destination.ID,
		S3Endpoint:        destination.Endpoint,
		S3Bucket:          destination.Bucket,
		S3Region:          destination.Region,
		S3AccessKeyID:     destination.AccessKeyID,
		S3SecretAccessKey: secret,
		S3Prefix:          destination.Prefix,
		S3UseSSL:          destination.UseSSL,
		S3ForcePathStyle:  destination.ForcePathStyle,
	}, nil
}
