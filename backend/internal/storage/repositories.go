package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	pkgstorage "github.com/getarcaneapp/arcane/backend/pkg/storage"
)

type Repositories struct {
	Settings SettingsRepository
	KV       KVRepository
	User     UserRepository
	APIKey   APIKeyRepository
}

type SettingsRepository interface {
	List(ctx context.Context) ([]models.SettingVariable, error)
	Get(ctx context.Context, key string) (*models.SettingVariable, error)
	Upsert(ctx context.Context, setting models.SettingVariable) error
	UpsertMany(ctx context.Context, settings []models.SettingVariable) error
	EnsureDefaults(ctx context.Context, defaults []models.SettingVariable) error
	DeleteUnknown(ctx context.Context, allowed map[string]struct{}) (int64, error)
	RenameKeys(ctx context.Context, mappings map[string]string) error
	Delete(ctx context.Context, key string) error
}

type KVRepository interface {
	Get(ctx context.Context, key string) (*models.KVEntry, error)
	Set(ctx context.Context, key, value string) error
}

type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	Upsert(ctx context.Context, user *models.User) error
	Delete(ctx context.Context, id string) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByOidcSubjectID(ctx context.Context, subjectID string) (*models.User, error)
	List(ctx context.Context) ([]models.User, error)
}

type APIKeyRepository interface {
	Create(ctx context.Context, apiKey *models.ApiKey) error
	Upsert(ctx context.Context, apiKey *models.ApiKey) error
	Delete(ctx context.Context, id string) error
	DeleteMany(ctx context.Context, ids []string) error
	GetByID(ctx context.Context, id string) (*models.ApiKey, error)
	List(ctx context.Context) ([]models.ApiKey, error)
	ListByKeyPrefix(ctx context.Context, keyPrefix string) ([]models.ApiKey, error)
	ListManagedByUser(ctx context.Context, userID, managedBy string) ([]models.ApiKey, error)
}

func newSQLRepositories(db *database.DB) *Repositories {
	return &Repositories{
		Settings: &sqlSettingsRepository{db: db},
		KV:       &sqlKVRepository{db: db},
		User:     &sqlUserRepository{db: db},
		APIKey:   &sqlAPIKeyRepository{db: db},
	}
}

func newEtcdRepositories(store pkgstorage.Store) *Repositories {
	return &Repositories{
		Settings: &etcdSettingsRepository{store: store},
		KV:       &etcdKVRepository{store: store},
		User:     &etcdUserRepository{store: store},
		APIKey:   &etcdAPIKeyRepository{store: store},
	}
}

type sqlSettingsRepository struct {
	db *database.DB
}

func (r *sqlSettingsRepository) List(ctx context.Context) ([]models.SettingVariable, error) {
	var settings []models.SettingVariable
	if err := r.db.WithContext(ctx).Order("key asc").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	return settings, nil
}

func (r *sqlSettingsRepository) Get(ctx context.Context, key string) (*models.SettingVariable, error) {
	var setting models.SettingVariable
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get setting %s: %w", key, err)
	}
	return &setting, nil
}

func (r *sqlSettingsRepository) Upsert(ctx context.Context, setting models.SettingVariable) error {
	if err := r.db.WithContext(ctx).Save(&setting).Error; err != nil {
		return fmt.Errorf("failed to upsert setting %s: %w", setting.Key, err)
	}
	return nil
}

func (r *sqlSettingsRepository) UpsertMany(ctx context.Context, settings []models.SettingVariable) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range settings {
			if err := tx.Save(&setting).Error; err != nil {
				return fmt.Errorf("failed to upsert setting %s: %w", setting.Key, err)
			}
		}
		return nil
	})
}

func (r *sqlSettingsRepository) EnsureDefaults(ctx context.Context, defaults []models.SettingVariable) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range defaults {
			var existing models.SettingVariable
			err := tx.Where("key = ?", setting.Key).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if err := tx.Create(&setting).Error; err != nil {
					return fmt.Errorf("failed to create default setting %s: %w", setting.Key, err)
				}
			case err != nil:
				return fmt.Errorf("failed to load setting %s: %w", setting.Key, err)
			}
		}
		return nil
	})
}

func (r *sqlSettingsRepository) DeleteUnknown(ctx context.Context, allowed map[string]struct{}) (int64, error) {
	keys := make([]string, 0, len(allowed))
	for key := range allowed {
		keys = append(keys, key)
	}
	result := r.db.WithContext(ctx).Where("key NOT IN ?", keys).Delete(&models.SettingVariable{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete unknown settings: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (r *sqlSettingsRepository) RenameKeys(ctx context.Context, mappings map[string]string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for oldKey, newKey := range mappings {
			var oldSetting models.SettingVariable
			if err := tx.Where("key = ?", oldKey).First(&oldSetting).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return fmt.Errorf("failed to load setting %s: %w", oldKey, err)
			}

			var newSetting models.SettingVariable
			if err := tx.Where("key = ?", newKey).First(&newSetting).Error; err == nil {
				if err := tx.Delete(&oldSetting).Error; err != nil {
					return fmt.Errorf("failed to delete duplicate legacy setting %s: %w", oldKey, err)
				}
				continue
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("failed to load setting %s: %w", newKey, err)
			}

			if err := tx.Model(&oldSetting).Update("key", newKey).Error; err != nil {
				return fmt.Errorf("failed to rename setting %s to %s: %w", oldKey, newKey, err)
			}
		}
		return nil
	})
}

func (r *sqlSettingsRepository) Delete(ctx context.Context, key string) error {
	if err := r.db.WithContext(ctx).Where("key = ?", key).Delete(&models.SettingVariable{}).Error; err != nil {
		return fmt.Errorf("failed to delete setting %s: %w", key, err)
	}
	return nil
}

type sqlKVRepository struct {
	db *database.DB
}

func (r *sqlKVRepository) Get(ctx context.Context, key string) (*models.KVEntry, error) {
	var entry models.KVEntry
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get kv entry %s: %w", key, err)
	}
	return &entry, nil
}

func (r *sqlKVRepository) Set(ctx context.Context, key, value string) error {
	entry := models.KVEntry{Key: key, Value: value}
	if err := r.db.WithContext(ctx).Save(&entry).Error; err != nil {
		return fmt.Errorf("failed to set kv entry %s: %w", key, err)
	}
	return nil
}

type etcdSettingsRepository struct {
	store pkgstorage.Store
}

type etcdSettingRecord struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (r *etcdSettingsRepository) List(ctx context.Context) ([]models.SettingVariable, error) {
	records, err := r.store.ListRecords(ctx, "settings")
	if err != nil {
		return nil, err
	}
	out := make([]models.SettingVariable, 0, len(records))
	for _, record := range records {
		var setting etcdSettingRecord
		if err := json.Unmarshal(record.Value, &setting); err != nil {
			return nil, fmt.Errorf("failed to decode setting %s: %w", record.Key, err)
		}
		out = append(out, models.SettingVariable{Key: setting.Key, Value: setting.Value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (r *etcdSettingsRepository) Get(ctx context.Context, key string) (*models.SettingVariable, error) {
	record, err := r.store.GetRecord(ctx, "settings", key)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	var setting etcdSettingRecord
	if err := json.Unmarshal(record.Value, &setting); err != nil {
		return nil, fmt.Errorf("failed to decode setting %s: %w", key, err)
	}
	return &models.SettingVariable{Key: setting.Key, Value: setting.Value}, nil
}

func (r *etcdSettingsRepository) Upsert(ctx context.Context, setting models.SettingVariable) error {
	data, err := json.Marshal(etcdSettingRecord{Key: setting.Key, Value: setting.Value})
	if err != nil {
		return fmt.Errorf("failed to encode setting %s: %w", setting.Key, err)
	}
	return r.store.PutRecord(ctx, "settings", setting.Key, data)
}

func (r *etcdSettingsRepository) UpsertMany(ctx context.Context, settings []models.SettingVariable) error {
	for _, setting := range settings {
		if err := r.Upsert(ctx, setting); err != nil {
			return err
		}
	}
	return nil
}

func (r *etcdSettingsRepository) EnsureDefaults(ctx context.Context, defaults []models.SettingVariable) error {
	for _, setting := range defaults {
		existing, err := r.Get(ctx, setting.Key)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if err := r.Upsert(ctx, setting); err != nil {
			return err
		}
	}
	return nil
}

func (r *etcdSettingsRepository) DeleteUnknown(ctx context.Context, allowed map[string]struct{}) (int64, error) {
	settings, err := r.List(ctx)
	if err != nil {
		return 0, err
	}
	var deleted int64
	for _, setting := range settings {
		if _, ok := allowed[setting.Key]; ok {
			continue
		}
		if err := r.Delete(ctx, setting.Key); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (r *etcdSettingsRepository) RenameKeys(ctx context.Context, mappings map[string]string) error {
	for oldKey, newKey := range mappings {
		oldSetting, err := r.Get(ctx, oldKey)
		if err != nil {
			return err
		}
		if oldSetting == nil {
			continue
		}

		newSetting, err := r.Get(ctx, newKey)
		if err != nil {
			return err
		}
		if newSetting != nil {
			if err := r.Delete(ctx, oldKey); err != nil {
				return err
			}
			continue
		}

		oldSetting.Key = newKey
		if err := r.Upsert(ctx, *oldSetting); err != nil {
			return err
		}
		if err := r.Delete(ctx, oldKey); err != nil {
			return err
		}
	}
	return nil
}

func (r *etcdSettingsRepository) Delete(ctx context.Context, key string) error {
	return r.store.DeleteRecord(ctx, "settings", key)
}

type etcdKVRepository struct {
	store pkgstorage.Store
}

type etcdKVRecord struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (r *etcdKVRepository) Get(ctx context.Context, key string) (*models.KVEntry, error) {
	record, err := r.store.GetRecord(ctx, "kv", key)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	var entry etcdKVRecord
	if err := json.Unmarshal(record.Value, &entry); err != nil {
		return nil, fmt.Errorf("failed to decode kv entry %s: %w", key, err)
	}
	return &models.KVEntry{Key: entry.Key, Value: entry.Value}, nil
}

func (r *etcdKVRepository) Set(ctx context.Context, key, value string) error {
	data, err := json.Marshal(etcdKVRecord{Key: key, Value: value})
	if err != nil {
		return fmt.Errorf("failed to encode kv entry %s: %w", key, err)
	}
	return r.store.PutRecord(ctx, "kv", key, data)
}

func NewSQLSettingsRepository(db *database.DB) SettingsRepository {
	return &sqlSettingsRepository{db: db}
}

func NewSQLKVRepository(db *database.DB) KVRepository {
	return &sqlKVRepository{db: db}
}

type sqlUserRepository struct {
	db *database.DB
}

func (r *sqlUserRepository) Create(ctx context.Context, user *models.User) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("failed to create user %s: %w", user.Username, err)
	}
	return nil
}

func (r *sqlUserRepository) Upsert(ctx context.Context, user *models.User) error {
	if err := r.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("failed to save user %s: %w", user.ID, err)
	}
	return nil
}

func (r *sqlUserRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&models.User{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete user %s: %w", id, err)
	}
	return nil
}

func (r *sqlUserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	return r.getOne(ctx, "id = ?", id)
}

func (r *sqlUserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	return r.getOne(ctx, "username = ?", username)
}

func (r *sqlUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	return r.getOne(ctx, "email = ?", email)
}

func (r *sqlUserRepository) GetByOidcSubjectID(ctx context.Context, subjectID string) (*models.User, error) {
	return r.getOne(ctx, "oidc_subject_id = ?", subjectID)
}

func (r *sqlUserRepository) List(ctx context.Context) ([]models.User, error) {
	var users []models.User
	if err := r.db.WithContext(ctx).Order("created_at asc, id asc").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	return users, nil
}

func (r *sqlUserRepository) getOne(ctx context.Context, query string, args ...any) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Where(query, args...).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load user: %w", err)
	}
	return &user, nil
}

type etcdUserRepository struct {
	store pkgstorage.Store
}

func (r *etcdUserRepository) Create(ctx context.Context, user *models.User) error {
	return r.Upsert(ctx, user)
}

func (r *etcdUserRepository) Upsert(ctx context.Context, user *models.User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to encode user %s: %w", user.ID, err)
	}
	return r.store.PutRecord(ctx, "users", user.ID, data)
}

func (r *etcdUserRepository) Delete(ctx context.Context, id string) error {
	return r.store.DeleteRecord(ctx, "users", id)
}

func (r *etcdUserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	record, err := r.store.GetRecord(ctx, "users", id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	var user models.User
	if err := json.Unmarshal(record.Value, &user); err != nil {
		return nil, fmt.Errorf("failed to decode user %s: %w", id, err)
	}
	return &user, nil
}

func (r *etcdUserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	return r.findOne(ctx, func(user models.User) bool {
		return user.Username == username
	})
}

func (r *etcdUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	return r.findOne(ctx, func(user models.User) bool {
		return user.Email != nil && *user.Email == email
	})
}

func (r *etcdUserRepository) GetByOidcSubjectID(ctx context.Context, subjectID string) (*models.User, error) {
	return r.findOne(ctx, func(user models.User) bool {
		return user.OidcSubjectId != nil && *user.OidcSubjectId == subjectID
	})
}

func (r *etcdUserRepository) List(ctx context.Context) ([]models.User, error) {
	records, err := r.store.ListRecords(ctx, "users")
	if err != nil {
		return nil, err
	}
	out := make([]models.User, 0, len(records))
	for _, record := range records {
		var user models.User
		if err := json.Unmarshal(record.Value, &user); err != nil {
			return nil, fmt.Errorf("failed to decode user %s: %w", record.Key, err)
		}
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *etcdUserRepository) findOne(ctx context.Context, match func(models.User) bool) (*models.User, error) {
	users, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if match(user) {
			userCopy := user
			return &userCopy, nil
		}
	}
	return nil, nil
}

func NewSQLUserRepository(db *database.DB) UserRepository {
	return &sqlUserRepository{db: db}
}

type sqlAPIKeyRepository struct {
	db *database.DB
}

func (r *sqlAPIKeyRepository) Create(ctx context.Context, apiKey *models.ApiKey) error {
	if err := r.db.WithContext(ctx).Create(apiKey).Error; err != nil {
		return fmt.Errorf("failed to create api key %s: %w", apiKey.ID, err)
	}
	return nil
}

func (r *sqlAPIKeyRepository) Upsert(ctx context.Context, apiKey *models.ApiKey) error {
	if err := r.db.WithContext(ctx).Save(apiKey).Error; err != nil {
		return fmt.Errorf("failed to save api key %s: %w", apiKey.ID, err)
	}
	return nil
}

func (r *sqlAPIKeyRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&models.ApiKey{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete api key %s: %w", id, err)
	}
	return nil
}

func (r *sqlAPIKeyRepository) DeleteMany(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Delete(&models.ApiKey{}, "id IN ?", ids).Error; err != nil {
		return fmt.Errorf("failed to delete api keys: %w", err)
	}
	return nil
}

func (r *sqlAPIKeyRepository) GetByID(ctx context.Context, id string) (*models.ApiKey, error) {
	var apiKey models.ApiKey
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load api key %s: %w", id, err)
	}
	return &apiKey, nil
}

func (r *sqlAPIKeyRepository) List(ctx context.Context) ([]models.ApiKey, error) {
	var apiKeys []models.ApiKey
	if err := r.db.WithContext(ctx).Order("created_at asc, id asc").Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to list api keys: %w", err)
	}
	return apiKeys, nil
}

func (r *sqlAPIKeyRepository) ListByKeyPrefix(ctx context.Context, keyPrefix string) ([]models.ApiKey, error) {
	var apiKeys []models.ApiKey
	if err := r.db.WithContext(ctx).Where("key_prefix = ?", keyPrefix).Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to list api keys by prefix: %w", err)
	}
	return apiKeys, nil
}

func (r *sqlAPIKeyRepository) ListManagedByUser(ctx context.Context, userID, managedBy string) ([]models.ApiKey, error) {
	var apiKeys []models.ApiKey
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND managed_by = ?", userID, managedBy).
		Order("created_at asc, id asc").
		Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to list managed api keys: %w", err)
	}
	return apiKeys, nil
}

type etcdAPIKeyRepository struct {
	store pkgstorage.Store
}

func (r *etcdAPIKeyRepository) Create(ctx context.Context, apiKey *models.ApiKey) error {
	return r.Upsert(ctx, apiKey)
}

func (r *etcdAPIKeyRepository) Upsert(ctx context.Context, apiKey *models.ApiKey) error {
	data, err := json.Marshal(apiKey)
	if err != nil {
		return fmt.Errorf("failed to encode api key %s: %w", apiKey.ID, err)
	}
	return r.store.PutRecord(ctx, "api_keys", apiKey.ID, data)
}

func (r *etcdAPIKeyRepository) Delete(ctx context.Context, id string) error {
	return r.store.DeleteRecord(ctx, "api_keys", id)
}

func (r *etcdAPIKeyRepository) DeleteMany(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := r.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *etcdAPIKeyRepository) GetByID(ctx context.Context, id string) (*models.ApiKey, error) {
	record, err := r.store.GetRecord(ctx, "api_keys", id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	var apiKey models.ApiKey
	if err := json.Unmarshal(record.Value, &apiKey); err != nil {
		return nil, fmt.Errorf("failed to decode api key %s: %w", id, err)
	}
	return &apiKey, nil
}

func (r *etcdAPIKeyRepository) List(ctx context.Context) ([]models.ApiKey, error) {
	records, err := r.store.ListRecords(ctx, "api_keys")
	if err != nil {
		return nil, err
	}
	out := make([]models.ApiKey, 0, len(records))
	for _, record := range records {
		var apiKey models.ApiKey
		if err := json.Unmarshal(record.Value, &apiKey); err != nil {
			return nil, fmt.Errorf("failed to decode api key %s: %w", record.Key, err)
		}
		out = append(out, apiKey)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *etcdAPIKeyRepository) ListByKeyPrefix(ctx context.Context, keyPrefix string) ([]models.ApiKey, error) {
	return r.filter(ctx, func(apiKey models.ApiKey) bool {
		return apiKey.KeyPrefix == keyPrefix
	})
}

func (r *etcdAPIKeyRepository) ListManagedByUser(ctx context.Context, userID, managedBy string) ([]models.ApiKey, error) {
	return r.filter(ctx, func(apiKey models.ApiKey) bool {
		return apiKey.UserID == userID && apiKey.ManagedBy != nil && *apiKey.ManagedBy == managedBy
	})
}

func (r *etcdAPIKeyRepository) filter(ctx context.Context, match func(models.ApiKey) bool) ([]models.ApiKey, error) {
	apiKeys, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.ApiKey, 0)
	for _, apiKey := range apiKeys {
		if match(apiKey) {
			out = append(out, apiKey)
		}
	}
	return out, nil
}

func NewSQLAPIKeyRepository(db *database.DB) APIKeyRepository {
	return &sqlAPIKeyRepository{db: db}
}
