package enrollment

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	testEnvironmentRow struct {
		ID   string `gorm:"primaryKey"`
		Name string
	}
)

func (testEnvironmentRow) TableName() string { return "environments" }

func newTestService(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&testEnvironmentRow{}, &Token{}))
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	crypto.InitEncryption(crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})
	db := &database.DB{DB: gormDB}
	return New(db), db
}

func TestService_CreateListDelete(t *testing.T) {
	ctx := t.Context()
	svc, db := newTestService(t)

	created, err := svc.Create(ctx, CreateParams{Name: "  Lab hosts  ", MaxUses: new(5)})
	require.NoError(t, err)
	assert.Equal(t, "Lab hosts", created.Name)
	assert.Equal(t, 5, kit.FromPtr(created.MaxUses))
	assert.True(t, strings.HasPrefix(created.Token, types.EnrollmentTokenPrefix))
	assert.Len(t, created.Token, len(types.EnrollmentTokenPrefix)+2*secretLength)

	var stored Token
	require.NoError(t, db.Where("id = ?", created.ID).First(&stored).Error)
	assert.Equal(t, kit.SHA256Hex(created.Token), stored.TokenHash)
	assert.NotContains(t, stored.TokenEncrypted, created.Token, "the token is stored encrypted")

	revealed, secret, err := svc.Reveal(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.Token, secret)
	assert.Equal(t, "Lab hosts", revealed.Name)

	tokens, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, created.ID, tokens[0].ID)
	assert.Equal(t, "Lab hosts", tokens[0].Name)
	assert.Equal(t, 5, kit.FromPtr(tokens[0].MaxUses))
	assert.Zero(t, tokens[0].UseCount)
	assert.Nil(t, tokens[0].EnvironmentID)
	assert.Nil(t, tokens[0].LastUsedAt)

	deleted, err := svc.Delete(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Lab hosts", deleted.Name)
	tokens, err = svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, tokens)
	_, err = svc.Delete(ctx, created.ID)
	require.ErrorIs(t, err, ErrTokenNotFound)
	_, _, err = svc.Reveal(ctx, created.ID)
	require.ErrorIs(t, err, ErrTokenNotFound)
	_, err = svc.Resolve(ctx, created.Token)
	require.ErrorIs(t, err, common.ErrUnauthorized)
}

func TestService_MaxUsesEnforced(t *testing.T) {
	ctx := t.Context()
	svc, _ := newTestService(t)

	created, err := svc.Create(ctx, CreateParams{Name: "twice", MaxUses: new(2)})
	require.NoError(t, err)

	_, err = svc.Resolve(ctx, "arc_etkn_unknown")
	require.ErrorIs(t, err, common.ErrUnauthorized)

	for range 2 {
		token, resolveErr := svc.Resolve(ctx, created.Token)
		require.NoError(t, resolveErr)
		require.NoError(t, svc.Consume(ctx, token.ID))
	}
	require.ErrorIs(t, svc.Consume(ctx, created.ID), common.ErrUnauthorized)
	exhausted, err := svc.Resolve(ctx, created.Token)
	require.NoError(t, err, "an exhausted token still resolves so enrolled agents can restart")
	assert.Equal(t, 2, exhausted.UseCount)

	require.NoError(t, svc.Refund(ctx, created.ID))
	token, err := svc.Resolve(ctx, created.Token)
	require.NoError(t, err)
	assert.Equal(t, 1, token.UseCount, "a refund gives one use back")
	assert.NotNil(t, token.LastUsedAt)
}

func TestService_ConcurrentConsumeNeverExceedsMaxUses(t *testing.T) {
	ctx := t.Context()
	svc, _ := newTestService(t)

	created, err := svc.Create(ctx, CreateParams{Name: "limited", MaxUses: new(3)})
	require.NoError(t, err)

	var consumed atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if svc.Consume(ctx, created.ID) == nil {
				consumed.Add(1)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, int32(3), consumed.Load())
	tokens, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, tokens[0].UseCount)
}

func TestService_UnlimitedToken(t *testing.T) {
	ctx := t.Context()
	svc, _ := newTestService(t)

	created, err := svc.Create(ctx, CreateParams{Name: "unlimited"})
	require.NoError(t, err)
	assert.Nil(t, created.MaxUses)

	for range 25 {
		require.NoError(t, svc.Consume(ctx, created.ID))
	}
	token, err := svc.Resolve(ctx, created.Token)
	require.NoError(t, err)
	assert.Equal(t, 25, token.UseCount)
	assert.Nil(t, token.MaxUses)
}

func TestService_BoundTokenReplacesPreviousBoundTokens(t *testing.T) {
	ctx := t.Context()
	svc, db := newTestService(t)
	require.NoError(t, db.Create(&testEnvironmentRow{ID: "env-1", Name: "Production"}).Error)

	unbound, err := svc.Create(ctx, CreateParams{Name: "fleet"})
	require.NoError(t, err)
	first, err := svc.Create(ctx, CreateParams{
		EnvironmentID: new("env-1"),
		MaxUses:       new(1),
		Name:          "Production",
	})
	require.NoError(t, err)
	second, err := svc.Create(ctx, CreateParams{
		EnvironmentID: new("env-1"),
		MaxUses:       new(1),
		Name:          "Production",
	})
	require.NoError(t, err)

	_, err = svc.Resolve(ctx, first.Token)
	require.ErrorIs(t, err, common.ErrUnauthorized, "minting a new bound token deletes the previous one")
	bound, err := svc.Resolve(ctx, second.Token)
	require.NoError(t, err)
	assert.Equal(t, "env-1", kit.FromPtr(bound.EnvironmentID))

	tokens, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	byID := map[string]types.EnrollmentToken{
		tokens[0].ID: tokens[0],
		tokens[1].ID: tokens[1],
	}
	assert.Nil(t, byID[unbound.ID].EnvironmentName)
	assert.Equal(t, "Production", kit.FromPtr(byID[second.ID].EnvironmentName))
	assert.Equal(t, "env-1", kit.FromPtr(byID[second.ID].EnvironmentID))
}
