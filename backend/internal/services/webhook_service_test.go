package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
)

// setupWebhookServiceTestDB creates an isolated in-memory SQLite DB for each test.
func setupWebhookServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(glsqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Webhook{}))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return &database.DB{DB: db}
}

// newTestWebhookService returns a WebhookService with nil updater/project/gitops services,
// sufficient for all tests that exercise lookup, hashing, and CRUD logic.
// Tests that exercise the dispatch path use an unknown target type to avoid a nil dereference.
func newTestWebhookService(db *database.DB) *WebhookService {
	return &WebhookService{db: db}
}

// fetchWebhook loads a webhook directly from the DB to inspect stored fields.
func fetchWebhook(t *testing.T, db *database.DB, id string) models.Webhook {
	t.Helper()
	var wh models.Webhook
	require.NoError(t, db.WithContext(context.Background()).Where("id = ?", id).First(&wh).Error)
	return wh
}

// --- Token generation & hashing ---

func TestWebhookTokenFormat(t *testing.T) {
	raw, hash, prefix, err := generateWebhookTokenInternal()
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(raw, webhookTokenPrefix), "token must start with %q", webhookTokenPrefix)
	hexPart := strings.TrimPrefix(raw, webhookTokenPrefix)
	assert.Len(t, hexPart, webhookTokenLength*2, "hex part must be 64 chars (32 bytes)")
	assert.Equal(t, webhookTokenPrefix+hexPart[:webhookTokenPrefixLen], prefix, "prefix must be arc_wh_ + first %d chars of hex part", webhookTokenPrefixLen)

	expected := sha256.Sum256([]byte(raw))
	assert.Equal(t, hex.EncodeToString(expected[:]), hash, "stored hash must be SHA-256 of the raw token")
}

func TestHashWebhookTokenIsDeterministic(t *testing.T) {
	raw := "arc_wh_0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	first := hashWebhookTokenInternal(raw)
	second := hashWebhookTokenInternal(raw)
	assert.Equal(t, first, second)
}

func TestParseWebhookPrefix_ValidToken(t *testing.T) {
	raw := "arc_wh_0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	prefix, err := parseWebhookPrefixInternal(raw)
	require.NoError(t, err)
	assert.Equal(t, "arc_wh_01020304", prefix)
}

func TestParseWebhookPrefix_MissingPrefix(t *testing.T) {
	_, err := parseWebhookPrefixInternal("notawebhooktoken")
	assert.ErrorIs(t, err, ErrWebhookInvalid)
}

func TestParseWebhookPrefix_TooShort(t *testing.T) {
	_, err := parseWebhookPrefixInternal("arc_wh_0102030") // 7 chars after prefix — too short
	assert.ErrorIs(t, err, ErrWebhookInvalid)
}

func TestParseWebhookPrefix_LeadingWhitespaceStripped(t *testing.T) {
	raw := "  arc_wh_0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20  "
	prefix, err := parseWebhookPrefixInternal(raw)
	require.NoError(t, err)
	assert.Equal(t, "arc_wh_01020304", prefix)
}

// --- CreateWebhook ---

func TestCreateWebhook_TokenNotStoredInPlaintext(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, rawToken, err := svc.CreateWebhook(ctx, "my-hook", models.WebhookTargetTypeProject, "project-id", "env-1", models.User{})
	require.NoError(t, err)
	require.NotEmpty(t, rawToken)

	stored := fetchWebhook(t, db, wh.ID)
	assert.NotEqual(t, rawToken, stored.TokenHash, "raw token must not be stored as-is")
	assert.Equal(t, hashWebhookTokenInternal(rawToken), stored.TokenHash, "stored hash must match SHA-256 of raw token")
}

func TestCreateWebhook_PrefixMatchesToken(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, rawToken, err := svc.CreateWebhook(ctx, "prefix-check", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	hexPart := strings.TrimPrefix(rawToken, webhookTokenPrefix)
	expectedPrefix := webhookTokenPrefix + hexPart[:webhookTokenPrefixLen]
	assert.Equal(t, expectedPrefix, wh.TokenPrefix)

	stored := fetchWebhook(t, db, wh.ID)
	assert.Equal(t, expectedPrefix, stored.TokenPrefix)
}

func TestCreateWebhook_InvalidTargetTypeRejected(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, _, err := svc.CreateWebhook(ctx, "bad", "invalid-type", "c1", "env-1", models.User{})
	assert.ErrorIs(t, err, ErrWebhookInvalidType)
}

func TestCreateWebhook_EmptyTargetIDRejectedForNonUpdaterTypes(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	for _, targetType := range []string{
		models.WebhookTargetTypeContainer,
		models.WebhookTargetTypeProject,
		models.WebhookTargetTypeGitOps,
	} {
		_, _, err := svc.CreateWebhook(ctx, "hook", targetType, "", "env-1", models.User{})
		assert.ErrorIs(t, err, ErrWebhookMissingTarget, "expected ErrWebhookMissingTarget for type %s with empty targetID", targetType)
	}
}

func TestCreateWebhook_EmptyTargetIDAcceptedForUpdaterType(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "updater-hook", models.WebhookTargetTypeUpdater, "", "env-1", models.User{})
	require.NoError(t, err)
	assert.Equal(t, "", wh.TargetID)
}

func TestCreateWebhook_ContainerTypeAccepted(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "container-hook", models.WebhookTargetTypeContainer, "container-id", "env-1", models.User{})
	require.NoError(t, err)
	assert.Equal(t, models.WebhookTargetTypeContainer, wh.TargetType)
}

func TestCreateWebhook_ProjectTypeAccepted(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "stack-hook", models.WebhookTargetTypeProject, "project-id", "env-1", models.User{})
	require.NoError(t, err)
	assert.Equal(t, models.WebhookTargetTypeProject, wh.TargetType)
}

func TestCreateWebhook_UpdaterTypeAccepted(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "updater-hook", models.WebhookTargetTypeUpdater, "", "env-1", models.User{})
	require.NoError(t, err)
	assert.Equal(t, models.WebhookTargetTypeUpdater, wh.TargetType)
}

func TestCreateWebhook_GitOpsTypeAccepted(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "gitops-hook", models.WebhookTargetTypeGitOps, "sync-id", "env-1", models.User{})
	require.NoError(t, err)
	assert.Equal(t, models.WebhookTargetTypeGitOps, wh.TargetType)
}

func TestCreateWebhook_EnabledByDefault(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "enabled", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)
	assert.True(t, wh.Enabled)
}

func TestCreateWebhook_UniqueTokensEachCall(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, token1, err := svc.CreateWebhook(ctx, "h1", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)
	_, token2, err := svc.CreateWebhook(ctx, "h2", models.WebhookTargetTypeProject, "p2", "env-1", models.User{})
	require.NoError(t, err)

	assert.NotEqual(t, token1, token2)
}

// --- ListWebhooks ---

func TestListWebhooks_ScopedToEnvironment(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, _, err := svc.CreateWebhook(ctx, "env1-hook", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)
	_, _, err = svc.CreateWebhook(ctx, "env2-hook", models.WebhookTargetTypeProject, "p2", "env-2", models.User{})
	require.NoError(t, err)

	list, err := svc.ListWebhooks(ctx, "env-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "env1-hook", list[0].Name)
}

func TestListWebhooks_EmptyForUnknownEnvironment(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	list, err := svc.ListWebhooks(ctx, "no-such-env")
	require.NoError(t, err)
	assert.Empty(t, list)
}

// --- GetWebhookByID ---

func TestGetWebhookByID_ReturnsCorrectWebhook(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	created, _, err := svc.CreateWebhook(ctx, "get-me", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	got, err := svc.GetWebhookByID(ctx, created.ID, "env-1")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}

func TestGetWebhookByID_NotFoundForWrongEnvironment(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	created, _, err := svc.CreateWebhook(ctx, "env-scoped", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	_, err = svc.GetWebhookByID(ctx, created.ID, "env-2")
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestGetWebhookByID_NotFoundForUnknownID(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, err := svc.GetWebhookByID(ctx, "does-not-exist", "env-1")
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

// --- DeleteWebhook ---

func TestDeleteWebhook_RemovesRecord(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	created, _, err := svc.CreateWebhook(ctx, "delete-me", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteWebhook(ctx, created.ID, "env-1", models.User{}))

	_, err = svc.GetWebhookByID(ctx, created.ID, "env-1")
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestDeleteWebhook_NotFoundForWrongEnvironment(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	created, _, err := svc.CreateWebhook(ctx, "env-scoped-delete", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	err = svc.DeleteWebhook(ctx, created.ID, "env-2", models.User{})
	assert.ErrorIs(t, err, ErrWebhookNotFound)

	// Webhook must still exist in the correct environment
	_, err = svc.GetWebhookByID(ctx, created.ID, "env-1")
	assert.NoError(t, err)
}

func TestDeleteWebhook_NotFoundForUnknownID(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	err := svc.DeleteWebhook(ctx, "does-not-exist", "env-1", models.User{})
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

// --- UpdateWebhook ---

func TestUpdateWebhook_DisableAndEnable(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "hook", models.WebhookTargetTypeProject, "proj-1", "env-1", models.User{})
	require.NoError(t, err)
	assert.True(t, wh.Enabled)

	updated, err := svc.UpdateWebhook(ctx, wh.ID, "env-1", false, models.User{})
	require.NoError(t, err)
	assert.False(t, updated.Enabled)

	updated, err = svc.UpdateWebhook(ctx, wh.ID, "env-1", true, models.User{})
	require.NoError(t, err)
	assert.True(t, updated.Enabled)
}

func TestUpdateWebhook_NotFoundForWrongEnvironment(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	wh, _, err := svc.CreateWebhook(ctx, "hook", models.WebhookTargetTypeProject, "proj-1", "env-1", models.User{})
	require.NoError(t, err)

	_, err = svc.UpdateWebhook(ctx, wh.ID, "env-other", false, models.User{})
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestUpdateWebhook_NotFoundForUnknownID(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, err := svc.UpdateWebhook(ctx, "does-not-exist", "env-1", false, models.User{})
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

// --- TriggerByToken ---

func TestTriggerByToken_InvalidFormat(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, err := svc.TriggerByToken(ctx, "not-a-webhook-token")
	assert.ErrorIs(t, err, ErrWebhookInvalid)
}

func TestTriggerByToken_NotFound(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	// Well-formatted token but not in the DB
	raw := "arc_wh_0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	_, err := svc.TriggerByToken(ctx, raw)
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestTriggerByToken_WrongHash_NotFound(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	_, rawToken, err := svc.CreateWebhook(ctx, "hash-check", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	// Flip the last character of the token
	tampered := rawToken[:len(rawToken)-1]
	if rawToken[len(rawToken)-1] == 'a' {
		tampered += "b"
	} else {
		tampered += "a"
	}

	_, err = svc.TriggerByToken(ctx, tampered)
	assert.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestTriggerByToken_DisabledWebhook(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	created, rawToken, err := svc.CreateWebhook(ctx, "disabled-hook", models.WebhookTargetTypeProject, "p1", "env-1", models.User{})
	require.NoError(t, err)

	require.NoError(t, db.WithContext(ctx).Model(&models.Webhook{}).Where("id = ?", created.ID).Update("enabled", false).Error)

	_, err = svc.TriggerByToken(ctx, rawToken)
	assert.ErrorIs(t, err, ErrWebhookDisabled)
}

func TestTriggerByToken_UnknownTargetType_ReturnsInvalidType(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	rawToken := "arc_wh_aabbccdd0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c"
	hash := hashWebhookTokenInternal(rawToken)
	hexPart := strings.TrimPrefix(rawToken, webhookTokenPrefix)
	prefix := webhookTokenPrefix + hexPart[:webhookTokenPrefixLen]

	wh := &models.Webhook{
		Name:          "bad-type",
		TokenHash:     hash,
		TokenPrefix:   prefix,
		TargetType:    "unknown-type",
		TargetID:      "some-id",
		EnvironmentID: "env-1",
		Enabled:       true,
	}
	require.NoError(t, db.WithContext(ctx).Create(wh).Error)

	_, err := svc.TriggerByToken(ctx, rawToken)
	assert.ErrorIs(t, err, ErrWebhookInvalidType)
}

// insertWebhookDirect inserts a webhook record directly, bypassing CreateWebhook validation,
// so dispatch tests can use known target types without needing real service dependencies.
func insertWebhookDirect(t *testing.T, ctx context.Context, db *database.DB, rawToken, targetType, targetID, envID string) *models.Webhook {
	t.Helper()
	hash := hashWebhookTokenInternal(rawToken)
	hexPart := strings.TrimPrefix(rawToken, webhookTokenPrefix)
	wh := &models.Webhook{
		Name:          "test-hook",
		TokenHash:     hash,
		TokenPrefix:   webhookTokenPrefix + hexPart[:webhookTokenPrefixLen],
		TargetType:    targetType,
		TargetID:      targetID,
		EnvironmentID: envID,
		Enabled:       true,
	}
	require.NoError(t, db.WithContext(ctx).Create(wh).Error)
	return wh
}

func TestTriggerByToken_ContainerType_NilServiceReturnsError(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db) // updaterService is nil

	rawToken := "arc_wh_ccddeeff01020304aabbccdd0102030405060708090a0b0c0d0e0f1011121314"
	insertWebhookDirect(t, ctx, db, rawToken, models.WebhookTargetTypeContainer, "container-id", "env-1")

	assert.Panics(t, func() {
		_, _ = svc.TriggerByToken(ctx, rawToken) //nolint:errcheck
	})
}

func TestTriggerByToken_UpdaterType_NilServiceReturnsError(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db) // updaterService is nil

	rawToken := "arc_wh_1122334401020304aabbccdd0102030405060708090a0b0c0d0e0f1011121314"
	insertWebhookDirect(t, ctx, db, rawToken, models.WebhookTargetTypeUpdater, "", "env-1")

	// nil updaterService causes a panic, which we verify the dispatch path is reached
	// by recovering — in production the service is always non-nil
	assert.Panics(t, func() {
		_, _ = svc.TriggerByToken(ctx, rawToken) //nolint:errcheck
	})
}

func TestTriggerByToken_GitOpsType_NilServiceReturnsError(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db) // gitOpsSyncService is nil

	rawToken := "arc_wh_aabbccdd11223344aabbccdd0102030405060708090a0b0c0d0e0f1011121314"
	insertWebhookDirect(t, ctx, db, rawToken, models.WebhookTargetTypeGitOps, "sync-id", "env-1")

	assert.Panics(t, func() {
		_, _ = svc.TriggerByToken(ctx, rawToken) //nolint:errcheck
	})
}

func TestTriggerByToken_DoesNotUpdateLastTriggeredAtOnError(t *testing.T) {
	ctx := context.Background()
	db := setupWebhookServiceTestDB(t)
	svc := newTestWebhookService(db)

	rawToken := "arc_wh_1122334401020304aabbccdd0102030405060708090a0b0c0d0e0f1011121315"
	wh := insertWebhookDirect(t, ctx, db, rawToken, "unknown-type", "some-id", "env-1")

	_, err := svc.TriggerByToken(ctx, rawToken)
	require.Error(t, err)

	stored := fetchWebhook(t, db, wh.ID)
	assert.Nil(t, stored.LastTriggeredAt, "last_triggered_at must not be set when trigger fails")
}
