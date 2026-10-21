package s3

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/normalization"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/s3"
)

var (
	ErrS3DestinationNotFound = common.Classify(common.ErrNotFound, errors.New("S3 destination not found"))
	ErrS3DestinationInUse    = errors.New("S3 destination is in use")
	// errS3SecretRequired enforces the fresh-secret rule: a stored
	// secret is never combined with changed connection or trust settings.
	errS3SecretRequired = errors.New("re-enter the secret access key when changing the endpoint, bucket, region, access key ID, SSL mode, or path style")
)

type S3DestinationService struct {
	db *database.DB
	// checkRemoteReferences rejects a deletion while any managed environment
	// still references the destination (or cannot be checked conclusively).
	// Nil outside the manager's module wiring.
	checkRemoteReferences func(ctx context.Context, destinationID string) error
}

func NewS3DestinationService(db *database.DB, checkRemoteReferences func(context.Context, string) error) *S3DestinationService {
	return &S3DestinationService{db: db, checkRemoteReferences: checkRemoteReferences}
}

func destinationConfiguration(id string, input types.S3Destination) s3.Configuration {
	return s3.Configuration{
		ID:              id,
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

func (s *S3DestinationService) ListS3Destinations(ctx context.Context, params pagination.QueryParams) ([]types.S3Destination, pagination.Response, error) {
	var destinations []S3Destination
	query := s.db.WithContext(ctx).Model(&S3Destination{})
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR endpoint LIKE ? OR bucket LIKE ? OR region LIKE ? OR prefix LIKE ?", pattern, pattern, pattern, pattern, pattern)
	}
	response, err := pagination.PaginateAndSortDB(params, query, &destinations)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list S3 destinations: %w", err)
	}
	result := make([]types.S3Destination, len(destinations))
	for i := range destinations {
		result[i] = destinations[i].ToDTO()
	}
	return result, response, nil
}

func (s *S3DestinationService) ListAllS3Destinations(ctx context.Context) ([]types.S3Destination, error) {
	var destinations []S3Destination
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&destinations).Error; err != nil {
		return nil, fmt.Errorf("failed to list S3 destinations: %w", err)
	}
	result := make([]types.S3Destination, len(destinations))
	for i := range destinations {
		result[i] = destinations[i].ToDTO()
	}
	return result, nil
}

// DestinationsByID indexes destinations to decorate backup records, such as with their names. Decoration never fails
// its caller, so a failed listing yields an empty index.
func (s *S3DestinationService) DestinationsByID(ctx context.Context) map[string]types.S3Destination {
	destinations, err := s.ListAllS3Destinations(ctx)
	if err != nil {
		slog.WarnContext(ctx, "could not list S3 destinations", "error", err)
		return nil
	}
	indexed := make(map[string]types.S3Destination, len(destinations))
	for _, destination := range destinations {
		indexed[destination.ID] = destination
	}
	return indexed
}

func (s *S3DestinationService) getS3DestinationModel(ctx context.Context, id string) (*S3Destination, error) {
	var destination S3Destination
	if err := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).First(&destination).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrS3DestinationNotFound
		}
		return nil, fmt.Errorf("failed to load S3 destination: %w", err)
	}
	return &destination, nil
}

func (s *S3DestinationService) GetS3Destination(ctx context.Context, id string) (*types.S3Destination, error) {
	destination, err := s.getS3DestinationModel(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := destination.ToDTO()
	return &dto, nil
}

func (s *S3DestinationService) CreateS3Destination(ctx context.Context, input types.S3Destination) (*types.S3Destination, error) {
	if err := normalization.Normalize(&input); err != nil {
		return nil, err
	}
	configuration := destinationConfiguration("", input)
	if err := configuration.Validate(true); err != nil {
		return nil, err
	}
	encryptedSecret, err := crypto.Encrypt(configuration.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt S3 secret access key: %w", err)
	}
	destination := &S3Destination{
		Name:            configuration.Name,
		Endpoint:        configuration.Endpoint,
		Bucket:          configuration.Bucket,
		Region:          configuration.Region,
		AccessKeyID:     configuration.AccessKeyID,
		SecretAccessKey: encryptedSecret,
		Prefix:          configuration.Prefix,
		UseSSL:          configuration.UseSSL,
		ForcePathStyle:  configuration.ForcePathStyle,
	}
	if createDestinationErr := s.db.WithContext(ctx).Create(destination).Error; createDestinationErr != nil {
		return nil, fmt.Errorf("failed to create S3 destination: %w", createDestinationErr)
	}
	dto := destination.ToDTO()
	return &dto, nil
}

func (s *S3DestinationService) MergeS3Destination(ctx context.Context, id string, update types.S3Destination, submitted map[string]bool) (types.S3Destination, error) {
	destination, err := s.getS3DestinationModel(ctx, id)
	if err != nil {
		return types.S3Destination{}, err
	}
	return types.S3Destination{
		Name:            kit.Ternary(submitted["name"], update.Name, destination.Name),
		Endpoint:        kit.Ternary(submitted["endpoint"], update.Endpoint, destination.Endpoint),
		Bucket:          kit.Ternary(submitted["bucket"], update.Bucket, destination.Bucket),
		Region:          kit.Ternary(submitted["region"], update.Region, destination.Region),
		AccessKeyID:     kit.Ternary(submitted["accessKeyId"], update.AccessKeyID, destination.AccessKeyID),
		SecretAccessKey: kit.Ternary(submitted["secretAccessKey"], update.SecretAccessKey, ""),
		Prefix:          kit.Ternary(submitted["prefix"], update.Prefix, destination.Prefix),
		UseSSL:          kit.Ternary(submitted["useSsl"], update.UseSSL, destination.UseSSL),
		ForcePathStyle:  kit.Ternary(submitted["forcePathStyle"], update.ForcePathStyle, destination.ForcePathStyle),
	}, nil
}

func (s *S3DestinationService) UpdateS3Destination(ctx context.Context, id string, input types.S3Destination) (*types.S3Destination, error) {
	if err := normalization.Normalize(&input); err != nil {
		return nil, err
	}
	configuration := destinationConfiguration("", input)
	if err := configuration.Validate(false); err != nil {
		return nil, err
	}
	destination, err := s.getS3DestinationModel(ctx, id)
	if err != nil {
		return nil, err
	}
	// The persisted configuration without the
	// decrypted secret, for comparing connection fields against an update.
	stored := s3.Configuration{
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
	if configuration.SecretAccessKey == "" && !s3.ConnectionFieldsEqual(stored, configuration) {
		return nil, errS3SecretRequired
	}
	if stored.Endpoint != configuration.Endpoint || stored.Bucket != configuration.Bucket || stored.Region != configuration.Region || stored.Prefix != configuration.Prefix {
		if ensureUnusedErr := s.ensureUnused(ctx, id); ensureUnusedErr != nil {
			return nil, fmt.Errorf("the bucket, prefix, endpoint or region cannot change while backups use the destination: %w", ensureUnusedErr)
		}
	}
	encryptedSecret, err := func() (string, error) {
		if configuration.SecretAccessKey == "" {
			return destination.SecretAccessKey, nil
		}
		return crypto.Encrypt(configuration.SecretAccessKey)
	}()
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt S3 secret access key: %w", err)
	}
	*destination = S3Destination{ //exhaustruct:enforce
		BaseModel:       destination.BaseModel,
		Name:            configuration.Name,
		Endpoint:        configuration.Endpoint,
		Bucket:          configuration.Bucket,
		Region:          configuration.Region,
		AccessKeyID:     configuration.AccessKeyID,
		SecretAccessKey: encryptedSecret,
		Prefix:          configuration.Prefix,
		UseSSL:          configuration.UseSSL,
		ForcePathStyle:  configuration.ForcePathStyle,
	}
	if updateDestinationErr := s.db.WithContext(ctx).Save(destination).Error; updateDestinationErr != nil {
		return nil, fmt.Errorf("failed to update S3 destination: %w", updateDestinationErr)
	}
	dto := destination.ToDTO()
	return &dto, nil
}

func (s *S3DestinationService) DeleteS3Destination(ctx context.Context, id string) error {
	destination, err := s.getS3DestinationModel(ctx, id)
	if err != nil {
		return err
	}
	if ensureUnusedErr := s.ensureUnused(ctx, id); ensureUnusedErr != nil {
		return ensureUnusedErr
	}
	if deleteDestinationErr := s.db.WithContext(ctx).Delete(destination).Error; deleteDestinationErr != nil {
		return fmt.Errorf("failed to delete S3 destination: %w", deleteDestinationErr)
	}
	return nil
}

func (s *S3DestinationService) ensureUnused(ctx context.Context, id string) error {
	inUse, err := s.DestinationInUse(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("%w: remove it from backup configurations and retained backups first", ErrS3DestinationInUse)
	}
	if checkRemoteReferencesErr := s.checkRemoteReferences(ctx, id); checkRemoteReferencesErr != nil {
		return fmt.Errorf("%w: %s", ErrS3DestinationInUse, checkRemoteReferencesErr.Error())
	}
	return nil
}

// DestinationInUse reports whether any local backup record or policy
// references the destination.
func (s *S3DestinationService) DestinationInUse(ctx context.Context, id string) (bool, error) {
	db := s.db.WithContext(ctx)
	counts := []*gorm.DB{
		db.Table("system_backup_runs").Where("s3_destination_id = ? AND remote_snapshot_id <> ''", id),
		db.Table("system_backup_policies").Where("s3_destination_id = ? AND s3_enabled = ?", id, true),
		db.Table("volume_backups").Where("s3_destination_id = ? AND remote_snapshot_id <> ''", id),
		db.Table("volume_backup_policies").Where("s3_destination_id = ? AND s3_enabled = ?", id, true),
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
	return s.db.WithContext(ctx).Model(&S3Destination{}).Where("id = ?", id).Count(&count).Error == nil && count == 1
}

// SyncS3Destinations replaces an agent's destination cache with the manager-owned destinations.
func (s *S3DestinationService) SyncS3Destinations(ctx context.Context, destinations []types.S3Destination) error {
	if err := normalization.Normalize(&destinations); err != nil {
		return err
	}
	var existing []S3Destination
	if err := s.db.WithContext(ctx).Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to load existing S3 destinations: %w", err)
	}
	existingByID := make(map[string]S3Destination, len(existing))
	for _, destination := range existing {
		existingByID[destination.ID] = destination
	}

	syncedIDs := make(map[string]struct{}, len(destinations))
	for _, item := range destinations {
		configuration := destinationConfiguration(item.ID, item)
		if configuration.ID == "" {
			return errors.New("S3 destination ID is required")
		}
		if err := configuration.Validate(true); err != nil {
			return fmt.Errorf("invalid S3 destination %s: %w", configuration.ID, err)
		}
		current, exists := existingByID[configuration.ID]
		// The manager resends the full set every health tick; unchanged rows
		// keep their ciphertext and skip the write.
		encryptedSecret := current.SecretAccessKey
		secretChanged, err := utils.ApplyEncrypted(&encryptedSecret, configuration.SecretAccessKey)
		if err != nil {
			return fmt.Errorf("failed to apply S3 secret access key for %s: %w", configuration.ID, err)
		}
		changed := !exists || secretChanged ||
			current.Name != configuration.Name ||
			current.Endpoint != configuration.Endpoint ||
			current.Bucket != configuration.Bucket ||
			current.Region != configuration.Region ||
			current.AccessKeyID != configuration.AccessKeyID ||
			current.Prefix != configuration.Prefix ||
			current.UseSSL != configuration.UseSSL ||
			current.ForcePathStyle != configuration.ForcePathStyle ||
			(!item.CreatedAt.IsZero() && !current.CreatedAt.Equal(item.CreatedAt)) ||
			(item.UpdatedAt != nil && !kit.FromPtr(current.UpdatedAt).Equal(*item.UpdatedAt))
		if changed {
			if syncDestinationErr := s.db.WithContext(ctx).Save(&S3Destination{ //exhaustruct:enforce
				BaseModel: database.BaseModel{
					ID:        configuration.ID,
					CreatedAt: kit.Ternary(item.CreatedAt.IsZero(), current.CreatedAt, item.CreatedAt),
					UpdatedAt: kit.Ternary(item.UpdatedAt != nil, item.UpdatedAt, current.UpdatedAt),
				},
				Name:            configuration.Name,
				Endpoint:        configuration.Endpoint,
				Bucket:          configuration.Bucket,
				Region:          configuration.Region,
				AccessKeyID:     configuration.AccessKeyID,
				SecretAccessKey: encryptedSecret,
				Prefix:          configuration.Prefix,
				UseSSL:          configuration.UseSSL,
				ForcePathStyle:  configuration.ForcePathStyle,
			}).Error; syncDestinationErr != nil {
				return fmt.Errorf("failed to sync S3 destination %s: %w", configuration.ID, syncDestinationErr)
			}
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
func (s *S3DestinationService) Configuration(ctx context.Context, id string) (s3.Configuration, error) {
	destination, err := s.getS3DestinationModel(ctx, id)
	if err != nil {
		return s3.Configuration{}, err
	}
	secret, err := crypto.Decrypt(destination.SecretAccessKey)
	if err != nil {
		return s3.Configuration{}, fmt.Errorf("failed to decrypt S3 secret access key: %w", err)
	}
	return s3.Configuration{
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

func (s *S3DestinationService) TestS3Destination(ctx context.Context, id string, input *types.S3Destination) error {
	configuration, err := s.Configuration(ctx, id)
	if err != nil {
		return err
	}
	if input == nil {
		return s3.TestConnection(ctx, configuration)
	}
	if normalizeErr := normalization.Normalize(input); normalizeErr != nil {
		return normalizeErr
	}
	updated := destinationConfiguration(configuration.ID, *input)
	// The stored secret may only sign requests against the stored
	// connection settings; any change requires a fresh secret.
	if updated.SecretAccessKey == "" && !s3.ConnectionFieldsEqual(configuration, updated) {
		return errS3SecretRequired
	}
	return s3.TestConnection(ctx, s3.Configuration{
		ID:              updated.ID,
		Name:            updated.Name,
		Endpoint:        updated.Endpoint,
		Bucket:          updated.Bucket,
		Region:          updated.Region,
		AccessKeyID:     updated.AccessKeyID,
		SecretAccessKey: cmp.Or(updated.SecretAccessKey, configuration.SecretAccessKey),
		Prefix:          updated.Prefix,
		UseSSL:          updated.UseSSL,
		ForcePathStyle:  updated.ForcePathStyle,
	})
}

func (s *S3DestinationService) TestS3DestinationConfiguration(ctx context.Context, input types.S3Destination) error {
	if err := normalization.Normalize(&input); err != nil {
		return err
	}
	return s3.TestConnection(ctx, destinationConfiguration("", input))
}
