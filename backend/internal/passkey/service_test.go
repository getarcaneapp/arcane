package passkey

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"testing"
	"time"
	"uuid"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/types/v2/auth"
)

func newPasskeyServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(
		&common.User{},
		&session.UserSession{},
		&Passkey{},
		&PasskeyCeremony{},
		&AuthTransaction{},
		&PasskeyRecoveryCode{},
	))

	db := &database.DB{DB: gormDB}
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

func newPasskeyServiceForTest(t *testing.T, db *database.DB) *PasskeyService {
	t.Helper()
	return NewPasskeyService(db, &config.Config{AppUrl: "https://arcane.example.test"})
}

func createPasskeyTestUser(t *testing.T, db *database.DB, id string) *common.User {
	t.Helper()
	user := &common.User{
		ID:           id,
		Username:     id,
		PasswordHash: "stored-password-hash",
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func createPasskeyTestCredential(t *testing.T, db *database.DB, service *PasskeyService, userID, id string) *Passkey {
	t.Helper()
	credential := &Passkey{
		ID:           id,
		UserID:       userID,
		RPID:         service.rpID,
		CredentialID: []byte("credential-" + id),
		PublicKey:    []byte("public-key-" + id),
		Transports:   database.StringSlice{"internal"},
		Name:         "Test passkey",
	}
	require.NoError(t, db.Create(credential).Error)
	return credential
}

func TestPasskeyService_BeginPasskeyLoginStoresAndConsumesCeremony(t *testing.T) {
	db := newPasskeyServiceTestDB(t)
	service := newPasskeyServiceForTest(t, db)
	ctx := context.Background()

	challenge, err := service.BeginPasskeyLogin(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, challenge.CeremonyID)
	require.NotNil(t, challenge.Options)

	var ceremony PasskeyCeremony
	require.NoError(t, db.Where("id = ?", challenge.CeremonyID).First(&ceremony).Error)
	require.Equal(t, passkeyCeremonyPurposeLogin, ceremony.Purpose)
	require.Equal(t, service.rpID, ceremony.RPID)
	require.Empty(t, ceremony.UserID)
	require.NotEmpty(t, ceremony.SessionData)
	require.WithinDuration(t, challenge.ExpiresAt, ceremony.ExpiresAt, time.Second)

	_, err = service.FinishPasskeyLogin(ctx, challenge.CeremonyID, []byte(`{}`))
	require.ErrorIs(t, err, ErrPasskeyResponse)

	var consumed PasskeyCeremony
	require.NoError(t, db.Where("id = ?", challenge.CeremonyID).First(&consumed).Error)
	require.NotNil(t, consumed.ConsumedAt)

	_, err = service.FinishPasskeyLogin(ctx, challenge.CeremonyID, []byte(`{}`))
	require.ErrorIs(t, err, ErrPasskeyCeremony)
}

func TestPasskeyService_FirstEnrollmentUsesActiveSession(t *testing.T) {
	db := newPasskeyServiceTestDB(t)
	service := newPasskeyServiceForTest(t, db)
	ctx := context.Background()
	user := createPasskeyTestUser(t, db, "first-enrollment-user")
	sessionService := session.NewSessionService(db)
	session, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	capabilities, err := service.GetCapabilities(ctx, user.ID, false)
	require.NoError(t, err)
	require.Zero(t, capabilities.PasskeyCount)
	require.True(t, capabilities.CanEnrollWithActiveSession)
	require.False(t, capabilities.RequiresStepUp)

	challenge, err := service.BeginRegistration(ctx, user.ID, session.ID, "")
	require.NoError(t, err)
	require.NotEmpty(t, challenge.CeremonyID)
	require.NotNil(t, challenge.Options)

	createPasskeyTestCredential(t, db, service, user.ID, "existing-passkey")
	capabilities, err = service.GetCapabilities(ctx, user.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, capabilities.PasskeyCount)
	require.False(t, capabilities.CanEnrollWithActiveSession)
	require.True(t, capabilities.RequiresStepUp)

	_, err = service.BeginRegistration(ctx, user.ID, session.ID, "")
	require.ErrorIs(t, err, ErrPasskeyStepUpRequired)
}

func TestPasskeyService_DefaultPasskeyNameUsesAAGUIDCatalog(t *testing.T) {
	aaguid := uuid.MustParse("fbfc3007-154e-4ecc-8c0b-6e020557d7bd")
	require.Equal(t, "Apple Passwords Passkey", defaultPasskeyNameInternal(aaguid[:]))
	require.Equal(t, aaguid.String(), formatAAGUIDInternal(aaguid[:]))
	require.Equal(t, "New Passkey", defaultPasskeyNameInternal([]byte{0xff, 0xff, 0xff, 0xff}))

	summary := passkeySummaryInternal(Passkey{AAGUID: aaguid[:]})
	require.Equal(t, aaguid.String(), summary.AAGUID)
}

func TestPasskeyService_PasswordStepUpGrantIsBoundToActiveSessionAndReusableUntilExpiry(t *testing.T) {
	db := newPasskeyServiceTestDB(t)
	service := newPasskeyServiceForTest(t, db)
	ctx := context.Background()
	user := createPasskeyTestUser(t, db, "step-up-user")
	sessionService := session.NewSessionService(db)
	session, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	_, err = service.CreatePasswordStepUpGrant(ctx, user.ID, "missing-session")
	require.ErrorIs(t, err, ErrPasskeyStepUpRequired)

	grant, err := service.CreatePasswordStepUpGrant(ctx, user.ID, session.ID)
	require.NoError(t, err)
	require.NotEmpty(t, grant.Token)
	require.True(t, grant.ExpiresAt.After(time.Now()))

	require.NoError(t, service.VerifyStepUpToken(ctx, user.ID, session.ID, grant.Token))
	require.NoError(t, service.VerifyStepUpToken(ctx, user.ID, session.ID, grant.Token))
	require.ErrorIs(t, service.VerifyStepUpToken(ctx, user.ID, "other-session", grant.Token), ErrPasskeyStepUpRequired)

	require.NoError(t, sessionService.RevokeSession(ctx, session.ID))
	_, err = service.CreatePasswordStepUpGrant(ctx, user.ID, session.ID)
	require.ErrorIs(t, err, ErrPasskeyStepUpRequired)
}

func TestPasskeyService_RecoveryCodeConsumptionIsAtomicAndSingleUse(t *testing.T) {
	db := newPasskeyServiceTestDB(t)
	service := newPasskeyServiceForTest(t, db)
	ctx := context.Background()
	user := createPasskeyTestUser(t, db, "recovery-user")

	codes, rows, err := generateRecoveryCodeRowsInternal(user.ID)
	require.NoError(t, err)
	require.Len(t, codes, recoveryCodeCount)
	require.NoError(t, db.Create(&rows).Error)

	transaction := newAuthTransactionInternal(user.ID, authTransactionKindMFA, session.UserSessionSourceLocal, auth.SessionMeta{
		UserAgent: "test-agent",
		IPAddress: "127.0.0.1",
	}, nil)
	require.NoError(t, db.Create(transaction).Error)

	_, err = service.FinishRecoveryCode(ctx, transaction.ID, "not-a-recovery-code")
	require.ErrorIs(t, err, ErrPasskeyRecoveryCode)

	var pending AuthTransaction
	require.NoError(t, db.Where("id = ?", transaction.ID).First(&pending).Error)
	require.Equal(t, authTransactionPending, pending.Status)

	completion, err := service.FinishRecoveryCode(ctx, transaction.ID, codes[0])
	require.NoError(t, err)
	require.Equal(t, user.ID, completion.User.ID)
	require.Equal(t, session.UserSessionSourceLocal, completion.Source)
	require.Equal(t, session.RecoveryCodeMFAMethod, completion.Meta.MFAMethod)
	require.Equal(t, "test-agent", completion.Meta.UserAgent)
	require.Equal(t, "127.0.0.1", completion.Meta.IPAddress)
	require.NotNil(t, completion.Meta.MFAVerifiedAt)

	var completed AuthTransaction
	require.NoError(t, db.Where("id = ?", transaction.ID).First(&completed).Error)
	require.Equal(t, authTransactionCompleted, completed.Status)
	require.NotNil(t, completed.CompletedAt)

	var consumed PasskeyRecoveryCode
	require.NoError(t, db.Where("user_id = ? AND code_hash = ?", user.ID, rows[0].CodeHash).First(&consumed).Error)
	require.NotNil(t, consumed.UsedAt)

	_, err = service.FinishRecoveryCode(ctx, transaction.ID, codes[0])
	require.ErrorIs(t, err, ErrPasskeyTransaction)
}

func TestPasskeyService_EnableAndDisableMFAManagesCodesAndSessions(t *testing.T) {
	db := newPasskeyServiceTestDB(t)
	service := newPasskeyServiceForTest(t, db)
	ctx := context.Background()
	user := createPasskeyTestUser(t, db, "mfa-user")
	createPasskeyTestCredential(t, db, service, user.ID, "mfa-passkey")
	sessionService := session.NewSessionService(db)
	currentSession, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	otherSession, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	grant, err := service.CreatePasswordStepUpGrant(ctx, user.ID, currentSession.ID)
	require.NoError(t, err)
	codes, err := service.EnableMFA(ctx, user.ID, currentSession.ID, grant.Token)
	require.NoError(t, err)
	require.Len(t, codes, recoveryCodeCount)

	var enabledUser common.User
	require.NoError(t, db.Where("id = ?", user.ID).First(&enabledUser).Error)
	require.True(t, enabledUser.PasskeyMFAEnabled)
	var recoveryCodeCountInDB int64
	require.NoError(t, db.Model(&PasskeyRecoveryCode{}).Where("user_id = ? AND used_at IS NULL", user.ID).Count(&recoveryCodeCountInDB).Error)
	require.Equal(t, int64(recoveryCodeCount), recoveryCodeCountInDB)

	disableGrant, err := service.CreatePasswordStepUpGrant(ctx, user.ID, currentSession.ID)
	require.NoError(t, err)
	require.NoError(t, service.DisableMFA(ctx, user.ID, currentSession.ID, disableGrant.Token))

	var disabledUser common.User
	require.NoError(t, db.Where("id = ?", user.ID).First(&disabledUser).Error)
	require.False(t, disabledUser.PasskeyMFAEnabled)
	require.NoError(t, db.Model(&PasskeyRecoveryCode{}).Where("user_id = ?", user.ID).Count(&recoveryCodeCountInDB).Error)
	require.Zero(t, recoveryCodeCountInDB)

	current, err := sessionService.GetSessionByID(ctx, currentSession.ID)
	require.NoError(t, err)
	require.Nil(t, current.RevokedAt)
	other, err := sessionService.GetSessionByID(ctx, otherSession.ID)
	require.NoError(t, err)
	require.NotNil(t, other.RevokedAt)
}

func TestPasskeyService_ResetMFARevokesSessionsAndPreservesPasskeys(t *testing.T) {
	db := newPasskeyServiceTestDB(t)
	service := newPasskeyServiceForTest(t, db)
	ctx := context.Background()
	user := createPasskeyTestUser(t, db, "reset-user")
	user.PasskeyMFAEnabled = true
	require.NoError(t, db.Save(user).Error)
	passkey := createPasskeyTestCredential(t, db, service, user.ID, "reset-passkey")
	sessionService := session.NewSessionService(db)
	firstSession, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	secondSession, _, err := sessionService.CreateSession(ctx, user.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	_, rows, err := generateRecoveryCodeRowsInternal(user.ID)
	require.NoError(t, err)
	require.NoError(t, db.Create(&rows).Error)
	transaction := newAuthTransactionInternal(user.ID, authTransactionKindMFA, session.UserSessionSourceLocal, auth.SessionMeta{}, nil)
	require.NoError(t, db.Create(transaction).Error)
	ceremony := &PasskeyCeremony{
		ID:                "reset-ceremony",
		Purpose:           passkeyCeremonyPurposeMFA,
		UserID:            new(user.ID),
		AuthTransactionID: new(transaction.ID),
		RPID:              service.rpID,
		SessionData:       "{}",
		ExpiresAt:         time.Now().Add(time.Minute),
	}
	require.NoError(t, db.Create(ceremony).Error)

	require.NoError(t, service.ResetMFAForUser(ctx, user.ID))

	var resetUser common.User
	require.NoError(t, db.Where("id = ?", user.ID).First(&resetUser).Error)
	require.False(t, resetUser.PasskeyMFAEnabled)
	var remaining int64
	require.NoError(t, db.Model(&PasskeyRecoveryCode{}).Where("user_id = ?", user.ID).Count(&remaining).Error)
	require.Zero(t, remaining)
	require.NoError(t, db.Model(&PasskeyCeremony{}).Where("user_id = ?", user.ID).Count(&remaining).Error)
	require.Zero(t, remaining)
	require.NoError(t, db.Model(&AuthTransaction{}).Where("user_id = ?", user.ID).Count(&remaining).Error)
	require.Zero(t, remaining)

	var preserved Passkey
	require.NoError(t, db.Where("id = ?", passkey.ID).First(&preserved).Error)
	require.Equal(t, passkey.ID, preserved.ID)

	first, err := sessionService.GetSessionByID(ctx, firstSession.ID)
	require.NoError(t, err)
	require.NotNil(t, first.RevokedAt)
	second, err := sessionService.GetSessionByID(ctx, secondSession.ID)
	require.NoError(t, err)
	require.NotNil(t, second.RevokedAt)
}
