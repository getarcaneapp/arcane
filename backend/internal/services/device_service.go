package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"gorm.io/gorm"
)

var (
	ErrDeviceNotFound = errors.New("device not found")
)

// DeviceService manages paired mobile (etc.) devices.
type DeviceService struct {
	db *database.DB
}

func NewDeviceService(db *database.DB) *DeviceService {
	return &DeviceService{db: db}
}

// UpsertDeviceInput is the input to UpsertForUserTx.
type UpsertDeviceInput struct {
	UserID      string
	DeviceID    string // client-supplied stable UUID
	Name        string
	AppVersion  string
	OsVersion   string
	DeviceModel string
	ApiKeyID    string // FK to ApiKey row created in the same transaction
}

// UpsertForUserTx creates or updates the Device row for a (user_id, device_id)
// pair. If a row already exists, its api_key_id is replaced with the new one
// (re-pair scenario) — the caller is responsible for deleting the previous
// ApiKey row to avoid orphans. Returns the resulting device.
func (s *DeviceService) UpsertForUserTx(tx *gorm.DB, in UpsertDeviceInput) (models.Device, error) {
	if in.UserID == "" || in.DeviceID == "" || in.ApiKeyID == "" {
		return models.Device{}, fmt.Errorf("device upsert requires user_id, device_id, and api_key_id")
	}

	var existing models.Device
	err := tx.Where("user_id = ? AND device_id = ?", in.UserID, in.DeviceID).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Device{}, fmt.Errorf("failed to lookup device: %w", err)
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		device := models.Device{
			UserID:      in.UserID,
			ApiKeyID:    in.ApiKeyID,
			Name:        in.Name,
			DeviceID:    in.DeviceID,
			AppVersion:  in.AppVersion,
			OsVersion:   in.OsVersion,
			DeviceModel: in.DeviceModel,
		}
		if err := tx.Create(&device).Error; err != nil {
			return models.Device{}, fmt.Errorf("failed to create device: %w", err)
		}
		return device, nil
	}

	previousApiKeyID := existing.ApiKeyID
	existing.ApiKeyID = in.ApiKeyID
	existing.Name = in.Name
	existing.AppVersion = in.AppVersion
	existing.OsVersion = in.OsVersion
	existing.DeviceModel = in.DeviceModel
	if err := tx.Save(&existing).Error; err != nil {
		return models.Device{}, fmt.Errorf("failed to update device: %w", err)
	}
	if previousApiKeyID != "" && previousApiKeyID != in.ApiKeyID {
		if err := tx.Delete(&models.ApiKey{}, "id = ?", previousApiKeyID).Error; err != nil {
			return models.Device{}, fmt.Errorf("failed to delete previous device api key: %w", err)
		}
	}
	return existing, nil
}

// GetByID returns the device with the given primary key, or ErrDeviceNotFound.
func (s *DeviceService) GetByID(ctx context.Context, id string) (models.Device, error) {
	var d models.Device
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Device{}, ErrDeviceNotFound
		}
		return models.Device{}, fmt.Errorf("failed to load device: %w", err)
	}
	return d, nil
}

// GetByApiKeyID looks up a device by its associated ApiKey row's ID.
// Used by the gRPC auth interceptor's TokenValidator callback.
func (s *DeviceService) GetByApiKeyID(ctx context.Context, apiKeyID string) (models.Device, error) {
	var d models.Device
	if err := s.db.WithContext(ctx).Where("api_key_id = ?", apiKeyID).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Device{}, ErrDeviceNotFound
		}
		return models.Device{}, fmt.Errorf("failed to load device by api_key_id: %w", err)
	}
	return d, nil
}

// ListForUser returns all devices owned by the given user, newest first.
func (s *DeviceService) ListForUser(ctx context.Context, userID string) ([]models.Device, error) {
	var devices []models.Device
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&devices).Error; err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}
	return devices, nil
}

// TouchLastSeen updates the last_seen_at timestamp on the given device.
// Errors are logged but not returned (this is best-effort).
func (s *DeviceService) TouchLastSeen(ctx context.Context, id string) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).
		Model(&models.Device{}).
		Where("id = ?", id).
		Update("last_seen_at", &now).Error; err != nil {
		return fmt.Errorf("failed to touch device last_seen_at: %w", err)
	}
	return nil
}

// Revoke deletes a device by ID. The associated ApiKey row is deleted via
// the FK ON DELETE CASCADE — but we delete it explicitly first because the
// FK direction is from devices.api_key_id → api_keys.id, which means
// cascade on api_keys delete (not the reverse). So the order matters.
func (s *DeviceService) Revoke(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d models.Device
		if err := tx.Where("id = ?", id).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeviceNotFound
			}
			return fmt.Errorf("failed to load device for revoke: %w", err)
		}
		if err := tx.Delete(&models.Device{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete device: %w", err)
		}
		if err := tx.Delete(&models.ApiKey{}, "id = ?", d.ApiKeyID).Error; err != nil {
			return fmt.Errorf("failed to delete device api key: %w", err)
		}
		return nil
	})
}

// Rename updates the human-readable name of a device.
func (s *DeviceService) Rename(ctx context.Context, id string, name string) (models.Device, error) {
	if err := s.db.WithContext(ctx).
		Model(&models.Device{}).
		Where("id = ?", id).
		Update("name", name).Error; err != nil {
		return models.Device{}, fmt.Errorf("failed to rename device: %w", err)
	}
	return s.GetByID(ctx, id)
}
