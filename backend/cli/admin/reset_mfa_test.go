package admin

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/types/v2/auth"
)

func newResetMFATestDBInternal(t *testing.T) *database.DB {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(
		&models.User{},
		&models.UserSession{},
		&models.Passkey{},
		&models.PasskeyCeremony{},
		&models.AuthTransaction{},
		&models.PasskeyRecoveryCode{},
	))

	db := &database.DB{DB: gormDB}
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

func TestEnsureMFAResetEnabledInternal(t *testing.T) {
	require.ErrorContains(t, ensureMFAResetEnabledInternal(&config.Config{}), "ALLOW_CLI_MFA_RESET=true")
	require.NoError(t, ensureMFAResetEnabledInternal(&config.Config{AllowCLIMFAReset: true}))
}

func TestConfirmMFAResetInternal(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, confirmMFAResetInternal(bytes.NewBufferString("RESET\n"), &output, "alice"))
	require.Contains(t, output.String(), "alice")

	output.Reset()
	require.ErrorContains(t, confirmMFAResetInternal(bytes.NewBufferString("cancel\n"), &output, "alice"), "cancelled")
}

func TestResetMFACommandServiceStatePreservesPasskey(t *testing.T) {
	db := newResetMFATestDBInternal(t)
	ctx := context.Background()
	user := &models.User{
		BaseModel:         models.BaseModel{ID: "mfa-reset-user"},
		Username:          "alice",
		PasskeyMFAEnabled: true,
	}
	require.NoError(t, db.Create(user).Error)
	passkeyRecord := &models.Passkey{
		BaseModel:    models.BaseModel{ID: "mfa-reset-passkey"},
		UserID:       user.ID,
		RPID:         "arcane.example.test",
		CredentialID: []byte("credential"),
		PublicKey:    []byte("public-key"),
		Name:         "Alice's key",
	}
	require.NoError(t, db.Create(passkeyRecord).Error)
	sessionService := session.NewSessionService(db)
	firstSession, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	secondSession, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.PasskeyRecoveryCode{
		BaseModel: models.BaseModel{ID: "mfa-reset-code"},
		UserID:    user.ID,
		CodeHash:  "stored-hash",
	}).Error)
	transaction := &models.AuthTransaction{
		BaseModel: models.BaseModel{ID: "mfa-reset-transaction"},
		Kind:      "mfa",
		UserID:    user.ID,
		Source:    models.UserSessionSourceLocal,
		Status:    "pending",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, db.Create(transaction).Error)
	require.NoError(t, db.Create(&models.PasskeyCeremony{
		BaseModel:         models.BaseModel{ID: "mfa-reset-ceremony"},
		Purpose:           "mfa",
		UserID:            new(user.ID),
		AuthTransactionID: new(transaction.ID),
		RPID:              passkeyRecord.RPID,
		SessionData:       "{}",
		ExpiresAt:         time.Now().Add(time.Minute),
	}).Error)

	service := passkey.NewPasskeyService(db, &config.Config{AppUrl: "https://arcane.example.test"})
	require.NoError(t, service.ResetMFAForUser(ctx, user.ID))

	var updatedUser models.User
	require.NoError(t, db.Where("id = ?", user.ID).First(&updatedUser).Error)
	require.False(t, updatedUser.PasskeyMFAEnabled)
	var count int64
	require.NoError(t, db.Model(&models.PasskeyRecoveryCode{}).Where("user_id = ?", user.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&models.PasskeyCeremony{}).Where("user_id = ?", user.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&models.AuthTransaction{}).Where("user_id = ?", user.ID).Count(&count).Error)
	require.Zero(t, count)

	var preserved models.Passkey
	require.NoError(t, db.Where("id = ?", passkeyRecord.ID).First(&preserved).Error)
	require.Equal(t, passkeyRecord.ID, preserved.ID)
	first, err := sessionService.GetSessionByID(ctx, firstSession.ID)
	require.NoError(t, err)
	require.NotNil(t, first.RevokedAt)
	second, err := sessionService.GetSessionByID(ctx, secondSession.ID)
	require.NoError(t, err)
	require.NotNil(t, second.RevokedAt)
}
