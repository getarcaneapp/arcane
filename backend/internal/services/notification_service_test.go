package services

import (
	"context"
	"encoding/json"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
)

func setupNotificationTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.NotificationSettings{}))

	// Initialize crypto for tests (requires 32+ byte key)
	testCfg := &config.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	}
	crypto.InitEncryption(testCfg)

	return &database.DB{DB: db}
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{}
	svc := NewNotificationService(db, cfg)

	// Create legacy Discord config with webhookUrl
	legacyConfig := map[string]any{
		"webhookUrl": "https://discord.com/api/webhooks/123456789/abcdef123456",
		"username":   "Arcane Bot",
		"avatarUrl":  "https://example.com/avatar.png",
		"events": map[string]bool{
			"image_update":     true,
			"container_update": false,
		},
	}

	configBytes, err := json.Marshal(legacyConfig)
	require.NoError(t, err)

	var configJSON models.JSON
	require.NoError(t, json.Unmarshal(configBytes, &configJSON))

	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  true,
		Config:   configJSON,
	}
	require.NoError(t, db.Create(&setting).Error)

	// Run migration
	err = svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify migration results
	var migratedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&migratedSetting).Error)

	var discordConfig models.DiscordConfig
	configBytes, err = json.Marshal(migratedSetting.Config)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &discordConfig))

	// Verify webhookId and token were extracted
	require.Equal(t, "123456789", discordConfig.WebhookID)
	require.NotEmpty(t, discordConfig.Token)

	// Verify token is encrypted and can be decrypted
	decryptedToken, err := crypto.Decrypt(discordConfig.Token)
	require.NoError(t, err)
	require.Equal(t, "abcdef123456", decryptedToken)

	// Verify other fields were preserved
	require.Equal(t, "Arcane Bot", discordConfig.Username)
	require.Equal(t, "https://example.com/avatar.png", discordConfig.AvatarURL)
	require.True(t, discordConfig.Events[models.NotificationEventImageUpdate])
	require.False(t, discordConfig.Events[models.NotificationEventContainerUpdate])
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_SkipsIfAlreadyMigrated(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{}
	svc := NewNotificationService(db, cfg)

	// Create already-migrated config with webhookId and token
	encryptedToken, err := crypto.Encrypt("already-migrated-token")
	require.NoError(t, err)

	migratedConfig := models.DiscordConfig{
		WebhookID: "999999999",
		Token:     encryptedToken,
		Username:  "Already Migrated",
	}

	configBytes, err := json.Marshal(migratedConfig)
	require.NoError(t, err)

	var configJSON models.JSON
	require.NoError(t, json.Unmarshal(configBytes, &configJSON))

	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  true,
		Config:   configJSON,
	}
	require.NoError(t, db.Create(&setting).Error)

	// Run migration - should skip
	err = svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify config was NOT changed
	var unchangedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&unchangedSetting).Error)

	var discordConfig models.DiscordConfig
	configBytes, err = json.Marshal(unchangedSetting.Config)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &discordConfig))

	require.Equal(t, "999999999", discordConfig.WebhookID)
	require.Equal(t, encryptedToken, discordConfig.Token)
	require.Equal(t, "Already Migrated", discordConfig.Username)
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_NoDiscordConfig(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{}
	svc := NewNotificationService(db, cfg)

	// No Discord config exists - migration should not error
	err := svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify no settings were created
	var count int64
	require.NoError(t, db.Model(&models.NotificationSettings{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_InvalidWebhookUrl(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{}
	svc := NewNotificationService(db, cfg)

	testCases := []struct {
		name       string
		webhookUrl string
	}{
		{
			name:       "missing webhooks path",
			webhookUrl: "https://discord.com/api/other/123456789/abcdef",
		},
		{
			name:       "incomplete webhook path",
			webhookUrl: "https://discord.com/api/webhooks/123456789",
		},
		{
			name:       "empty webhook url",
			webhookUrl: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up before each sub-test
			db.Exec("DELETE FROM notification_settings")

			legacyConfig := map[string]any{
				"webhookUrl": tc.webhookUrl,
			}

			configBytes, err := json.Marshal(legacyConfig)
			require.NoError(t, err)

			var configJSON models.JSON
			require.NoError(t, json.Unmarshal(configBytes, &configJSON))

			setting := models.NotificationSettings{
				Provider: models.NotificationProviderDiscord,
				Enabled:  true,
				Config:   configJSON,
			}
			require.NoError(t, db.Create(&setting).Error)

			// Migration should not error but should skip invalid URLs
			err = svc.MigrateDiscordWebhookUrlToFields(ctx)
			require.NoError(t, err)

			// Verify config was not changed
			var unchangedSetting models.NotificationSettings
			require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&unchangedSetting).Error)

			var resultConfig map[string]any
			configBytes, err = json.Marshal(unchangedSetting.Config)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(configBytes, &resultConfig))

			// Should still have webhookUrl (not migrated)
			if tc.webhookUrl != "" {
				require.Equal(t, tc.webhookUrl, resultConfig["webhookUrl"])
			}
		})
	}
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_EmptyConfig(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{}
	svc := NewNotificationService(db, cfg)

	// Create Discord setting with empty config
	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  false,
		Config:   models.JSON{},
	}
	require.NoError(t, db.Create(&setting).Error)

	// Migration should not error
	err := svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify config remains empty
	var unchangedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&unchangedSetting).Error)
	require.Empty(t, unchangedSetting.Config)
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_PreservesAllFields(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{}
	svc := NewNotificationService(db, cfg)

	// Create legacy config with all optional fields
	legacyConfig := map[string]any{
		"webhookUrl": "https://discord.com/api/webhooks/111222333/token444555",
		"username":   "Custom Bot Name",
		"avatarUrl":  "https://cdn.example.com/bot-avatar.jpg",
		"events": map[string]bool{
			"image_update":     false,
			"container_update": true,
		},
	}

	configBytes, err := json.Marshal(legacyConfig)
	require.NoError(t, err)

	var configJSON models.JSON
	require.NoError(t, json.Unmarshal(configBytes, &configJSON))

	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  true,
		Config:   configJSON,
	}
	require.NoError(t, db.Create(&setting).Error)

	// Run migration
	err = svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify all fields were preserved
	var migratedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&migratedSetting).Error)

	var discordConfig models.DiscordConfig
	configBytes, err = json.Marshal(migratedSetting.Config)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &discordConfig))

	require.Equal(t, "111222333", discordConfig.WebhookID)
	require.NotEmpty(t, discordConfig.Token)

	decryptedToken, err := crypto.Decrypt(discordConfig.Token)
	require.NoError(t, err)
	require.Equal(t, "token444555", decryptedToken)

	require.Equal(t, "Custom Bot Name", discordConfig.Username)
	require.Equal(t, "https://cdn.example.com/bot-avatar.jpg", discordConfig.AvatarURL)
	require.False(t, discordConfig.Events[models.NotificationEventImageUpdate])
	require.True(t, discordConfig.Events[models.NotificationEventContainerUpdate])
}
