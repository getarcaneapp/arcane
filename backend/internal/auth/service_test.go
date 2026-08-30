package auth

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"

	"github.com/getarcaneapp/arcane/backend/v2/internal/role"

	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"context"
	"crypto/mldsa"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mldsajose"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samber/hot"
	"github.com/stretchr/testify/assert"
)

func setupAuthServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// Each pooled connection to a ":memory:" DSN gets its own empty database;
	// keep a single connection so every query sees the migrated schema.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&common.User{},
		&session.UserSession{},
		&environment.Environment{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&apikey.ApiKey{},
		&role.ApiKeyPermission{},
		&role.OidcRoleMapping{},
	))
	return &database.DB{DB: db}
}

func newSettingsServiceForAuthTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	writes, err := actors.NewExecutor(t.Context(), runtime, "auth-settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "auth-settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, writes.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, writes, effects)
}

func newTestSigningKeyInternal() *mldsa.PrivateKey {
	key, err := mldsa.GenerateKey(mldsa.MLDSA87())
	if err != nil {
		panic(err)
	}
	return key
}

func newTestAuthService() *AuthService {
	return &AuthService{
		signingKey:    newTestSigningKeyInternal(),
		refreshExpiry: 24 * time.Hour,
		config:        &config.Config{},
		tokenCache: hot.NewHotCache[string, verifiedTokenEntry](hot.LRU, 4096).
			WithTTL(15 * time.Second).
			WithJanitor().
			Build(),
	}
}

func makeAccessToken(t *testing.T, key *mldsa.PrivateKey, subject string, id string, username string, _ []string, email, displayName string, exp time.Time, sessionIDs ...string) string {
	t.Helper()
	sessionID := ""
	if len(sessionIDs) > 0 {
		sessionID = sessionIDs[0]
	}
	claims := userClaims{
		ID:          id,
		Subject:     subject,
		IssuedAt:    jwt.NewNumericDate(time.Now()),
		ExpiresAt:   jwt.NewNumericDate(exp),
		SessionID:   sessionID,
		UserID:      id,
		Username:    username,
		Email:       email,
		DisplayName: displayName,
		AppVersion:  config.Version,
	}
	tok := jwt.NewWithClaims(mldsajose.SigningMethodMLDSA87, claims)
	signed, err := tok.SignedString(key)

	require.NoError(t, err,
		"sign:  %v", err)

	return signed
}

func makeRefreshToken(t *testing.T, key *mldsa.PrivateKey, subject string, id string, exp time.Time, userIDAndSessionID ...string) string {
	t.Helper()
	userID := id
	sessionID := ""
	if len(userIDAndSessionID) > 0 {
		userID = userIDAndSessionID[0]
	}
	if len(userIDAndSessionID) > 1 {
		sessionID = userIDAndSessionID[1]
	}
	claims := refreshClaims{
		ID:         id,
		Subject:    subject,
		IssuedAt:   jwt.NewNumericDate(time.Now()),
		ExpiresAt:  jwt.NewNumericDate(exp),
		UserID:     userID,
		SessionID:  sessionID,
		AppVersion: config.Version,
	}
	tok := jwt.NewWithClaims(mldsajose.SigningMethodMLDSA87, claims)
	signed, err := tok.SignedString(key)

	require.NoError(t, err,
		"sign: %v", err)

	return signed
}

func createTestSession(t *testing.T, db *database.DB, userID string, expiresAt time.Time) (*session.UserSession, string) {
	t.Helper()
	sessionSvc := session.NewSessionService(db)
	session, refreshJTI, err := sessionSvc.CreateSession(context.Background(), userID, expiresAt, auth.SessionMeta{
		UserAgent: "test-agent",
		IPAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	return session, refreshJTI
}

func makeUnsignedToken(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)

	require.NoError(t, err,
		"sign none: %v", err)

	return signed
}

func TestVerifyToken_ValidClaims(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	// Create user in DB
	user := &common.User{
		ID:          "u123",
		Username:    "alice",
		Email:       new("a@example.com"),
		DisplayName: new("Alice"),
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _ := createTestSession(t, db, "u123", exp)
	token := makeAccessToken(t, s.signingKey, "access", "u123", "alice", []string{"user", "admin"}, "a@example.com", "Alice", exp, session.ID)

	verifiedUser, _, err := s.VerifyToken(context.Background(), token)

	require.NoError(t, err,
		"VerifyToken error: %v", err)

	assert.Equal(t, "u123", verifiedUser.ID,
		"id %q", verifiedUser.ID)

	assert.Equal(t, "alice", verifiedUser.Username,
		"username %q", verifiedUser.Username)

	assert.False(t, verifiedUser.Email == nil || *verifiedUser.Email != "a@example.com",
		"email %v", verifiedUser.Email)

	assert.False(t, verifiedUser.DisplayName == nil || *verifiedUser.DisplayName != "Alice",
		"displayName %v", verifiedUser.DisplayName)

}

func TestVerifyToken_RejectsNonMLDSAAlg(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := makeUnsignedToken(t, userClaims{
		ID:         "u1",
		Subject:    "access",
		IssuedAt:   jwt.NewNumericDate(time.Now()),
		ExpiresAt:  jwt.NewNumericDate(exp),
		UserID:     "u1",
		Username:   "bob",
		AppVersion: config.Version,
	})

	_, _, err := s.VerifyToken(context.Background(), token)

	assert.ErrorIs(t, err, common.ErrInvalidToken,
		"want common.ErrInvalidToken, got %v", err)

}

func TestVerifyToken_Expired(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	// Create user in DB
	user := &common.User{
		ID:       "u1",
		Username: "bob",
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(-1 * time.Minute)
	token := makeAccessToken(t, s.signingKey, "access", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err = s.VerifyToken(context.Background(), token)

	assert.ErrorIs(t, err, common.ErrExpiredToken,
		"want ErrExpiredToken, got %v", err)

}

func TestVerifyToken_InvalidSubject(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := makeAccessToken(t, s.signingKey, "refresh", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(context.Background(), token)
	require.ErrorIs(t, err, common.ErrTokenValidation)
}

func TestVerifyToken_InvalidSignature(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	otherKey := newTestSigningKeyInternal()

	token := makeAccessToken(t, otherKey, "access", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(context.Background(), token)

	assert.ErrorIs(t, err, common.ErrInvalidToken,
		"want common.ErrInvalidToken, got %v", err)

}

func TestVerifyToken_MissingUserID(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := makeAccessToken(t, s.signingKey, "access", "", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(context.Background(), token)
	require.ErrorIs(t, err, common.ErrTokenValidation)
}

func TestGenerateUsernameFromEmail(t *testing.T) {
	u := generateUsernameFromEmail("john.doe@example.com", "sub-abcdef01")

	assert.Equal(t, "john.doe", u,
		"username %q", u)

	u2 := generateUsernameFromEmail("", "1234567890abcdef")

	assert.Equal(t, "user_90abcdef", u2,
		"fallback username %q", u2)

	u3 := generateUsernameFromEmail("", "short")

	assert.Equal(t, "user_short", u3,
		"short subject username %q", u3)

}

func TestUniqueOidcUsernameInternal(t *testing.T) {
	u := uniqueOidcUsernameInternal("alice", "1234567890abcdef")

	assert.Equal(t, "alice_90abcdef", u,
		"suffixed username %q", u)

	u2 := uniqueOidcUsernameInternal("alice", "short")

	assert.Equal(t, "alice_short", u2,
		"short subject username %q", u2)
}

func TestPersistOidcTokens_SetsFields(t *testing.T) {
	s := newTestAuthService()
	user := &common.User{}
	start := time.Now()
	resp := &auth.OidcTokenResponse{
		AccessToken:  "at-123",
		RefreshToken: "rt-456",
		ExpiresIn:    7,
		IDToken:      "",
	}
	s.persistOidcTokens(user, resp)

	assert.False(t, user.OidcAccessToken == nil || *user.OidcAccessToken != "at-123",
		"access token %v", user.OidcAccessToken)

	assert.False(t, user.OidcRefreshToken == nil || *user.OidcRefreshToken != "rt-456",
		"refresh token %v", user.OidcRefreshToken)

	assert.NotNil(t, user.OidcAccessTokenExpiresAt,
		"expiresAt nil")

	// Check approx expiry within [start+7s, start+12s] to allow CI slop
	earliest := start.Add(7 * time.Second)
	latest := start.Add(12 * time.Second)

	assert.False(t, user.OidcAccessTokenExpiresAt.Before(earliest) || user.OidcAccessTokenExpiresAt.After(latest),
		"expiresAt %v not in [%v,%v]", user.OidcAccessTokenExpiresAt, earliest, latest)

}

func TestVerifyToken_VersionMismatch(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)

	oldVersion := config.Version
	config.Version = "1.0.0"
	token := makeAccessToken(t, s.signingKey, "access", "u1", "bob", []string{"user"}, "", "", exp)
	config.Version = "2.0.0"

	_, _, err := s.VerifyToken(context.Background(), token)

	assert.ErrorIs(t, err, common.ErrTokenVersionMismatch,
		"want ErrTokenVersionMismatch, got %v", err)

	config.Version = oldVersion
}

func TestRefreshToken_Valid(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-refresh",
		Username: "refresh-user",
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, "u-refresh", exp)
	token := makeRefreshToken(t, s.signingKey, "refresh", refreshJTI, exp, "u-refresh", session.ID)

	tokenPair, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.NotEmpty(t, tokenPair.AccessToken)
	require.NotEmpty(t, tokenPair.RefreshToken)
}

// A refresh token minted under an older config.Version must still refresh successfully
// and the newly issued tokens must carry the current config.Version. This is what keeps
// users logged in across backend releases — see plans/how-can-we-make-compiled-quilt.md.
func TestRefreshToken_VersionMismatchRotates(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-versionmismatch",
		Username: "versionmismatch-user",
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, user.ID, exp)

	oldVersion := config.Version
	t.Cleanup(func() { config.Version = oldVersion })
	config.Version = "1.0.0"
	token := makeRefreshToken(t, s.signingKey, "refresh", refreshJTI, exp, user.ID, session.ID)
	config.Version = "2.0.0"

	tokenPair, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.NotEmpty(t, tokenPair.AccessToken)
	require.NotEmpty(t, tokenPair.RefreshToken)

	parsedAccess, err := jwt.ParseWithClaims(tokenPair.AccessToken, &userClaims{}, func(*jwt.Token) (any, error) {
		return s.signingKey.PublicKey(), nil
	})
	require.NoError(t, err)
	accessClaims, ok := parsedAccess.Claims.(*userClaims)
	require.True(t, ok)
	require.Equal(t, "2.0.0", accessClaims.AppVersion)

	parsedRefresh, err := jwt.ParseWithClaims(tokenPair.RefreshToken, &refreshClaims{}, func(*jwt.Token) (any, error) {
		return s.signingKey.PublicKey(), nil
	})
	require.NoError(t, err)
	rClaims, ok := parsedRefresh.Claims.(*refreshClaims)
	require.True(t, ok)
	require.Equal(t, "2.0.0", rClaims.AppVersion)
}

func TestVerifyToken_RejectsRevokedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-revoked",
		Username: "revoked-user",
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _ := createTestSession(t, db, user.ID, exp)
	require.NoError(t, s.RevokeSession(context.Background(), session.ID))
	token := makeAccessToken(t, s.signingKey, "access", user.ID, user.Username, []string{"user"}, "", "", exp, session.ID)

	_, _, err = s.VerifyToken(context.Background(), token)
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestVerifyToken_RejectsMissingSessionID(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-no-sid",
		Username: "no-sid-user",
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	token := makeAccessToken(t, s.signingKey, "access", user.ID, user.Username, []string{"user"}, "", "", time.Now().Add(5*time.Minute))

	_, _, err = s.VerifyToken(context.Background(), token)
	require.ErrorIs(t, err, common.ErrTokenValidation)
}

func TestRevokeSessionThenVerifyTokenFails(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-logout",
		Username: "logout-user",
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _ := createTestSession(t, db, user.ID, exp)
	token := makeAccessToken(t, s.signingKey, "access", user.ID, user.Username, []string{"user"}, "", "", exp, session.ID)
	require.NoError(t, s.RevokeSession(context.Background(), session.ID))

	_, _, err = s.VerifyToken(context.Background(), token)
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestVerifyToken_RejectsRevokedCachedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-cached-revoked",
		Username: "cached-revoked-user",
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	sessionRecord, _ := createTestSession(t, db, user.ID, exp)
	token := makeAccessToken(t, s.signingKey, "access", user.ID, user.Username, []string{"user"}, "", "", exp, sessionRecord.ID)

	_, _, err = s.VerifyToken(context.Background(), token)
	require.NoError(t, err)
	require.NoError(t, session.NewSessionService(db).RevokeSession(context.Background(), sessionRecord.ID))

	_, _, err = s.VerifyToken(context.Background(), token)
	require.ErrorIs(t, err, common.ErrSessionRevoked, "expected cached access token to be rejected after cross-process revocation, got %v", err)
}

func TestRefreshToken_RotatesJTI(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-rotate",
		Username: "rotate-user",
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, user.ID, exp)
	token := makeRefreshToken(t, s.signingKey, "refresh", refreshJTI, exp, user.ID, session.ID)

	tokenPair, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.NoError(t, err)
	require.NotEmpty(t, tokenPair.RefreshToken)

	_, err = s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.ErrorIs(t, err, common.ErrInvalidToken)
}

func TestRefreshToken_RejectsRevokedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	user := &common.User{
		ID:       "u-refresh-revoked",
		Username: "refresh-revoked-user",
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, user.ID, exp)
	require.NoError(t, s.RevokeSession(context.Background(), session.ID))
	token := makeRefreshToken(t, s.signingKey, "refresh", refreshJTI, exp, user.ID, session.ID)

	_, err = s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestChangePassword_RevokesAllSessions(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	passwordHash, err := userSvc.HashPassword("old-password")
	require.NoError(t, err)
	user := &common.User{
		ID:           "u-password",
		Username:     "password-user",
		PasswordHash: passwordHash,
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	sessionA, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))
	sessionB, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))

	require.NoError(t, s.ChangePassword(context.Background(), user.ID, "old-password", "New-password1!", ""))

	sessionA, err = s.sessionService.GetSessionByID(context.Background(), sessionA.ID)
	require.NoError(t, err)
	sessionB, err = s.sessionService.GetSessionByID(context.Background(), sessionB.ID)
	require.NoError(t, err)
	require.NotNil(t, sessionA.RevokedAt)
	require.NotNil(t, sessionB.RevokedAt)
}

func TestChangePassword_KeepsCurrentSessionAlive(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	passwordHash, err := userSvc.HashPassword("old-password")
	require.NoError(t, err)
	user := &common.User{
		ID:           "u-keep",
		Username:     "keep-user",
		PasswordHash: passwordHash,
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	current, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))
	other, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))

	require.NoError(t, s.ChangePassword(context.Background(), user.ID, "old-password", "New-password1!", current.ID))

	current, err = s.sessionService.GetSessionByID(context.Background(), current.ID)
	require.NoError(t, err)
	other, err = s.sessionService.GetSessionByID(context.Background(), other.ID)
	require.NoError(t, err)
	require.Nil(t, current.RevokedAt, "current session should remain active")
	require.NotNil(t, other.RevokedAt, "other sessions should be revoked")
}

func TestRefreshToken_RejectsNonHMACAlg(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := makeUnsignedToken(t, jwt.RegisteredClaims{
		ID:        "u1",
		Subject:   "refresh",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(exp),
	})

	_, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})

	assert.ErrorIs(t, err, common.ErrInvalidToken,
		"want common.ErrInvalidToken, got %v", err)

}

func TestGetOidcConfigurationStatus(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	// Disabled
	s := newTestAuthService()
	s.config = &config.Config{}
	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s.settingsService = settingsSvc

	status, err := s.GetOidcConfigurationStatus(context.Background())

	require.NoError(t, err,
		"GetOidcConfigurationStatus error: %v", err)

	assert.False(t, status.EnvForced || status.EnvConfigured,
		"expected disabled, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)

	// MergeAccounts will be false since GetSettings will fail

	assert.False(t, status.MergeAccounts,
		"expected mergeAccounts=false, got true")

	// Explicit env override to false should still be treated as forced
	t.Setenv("OIDC_ENABLED", "false")
	settingsSvc, err = newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s.settingsService = settingsSvc
	s.config.OidcEnabled = false
	status, err = s.GetOidcConfigurationStatus(context.Background())

	require.NoError(t, err,
		"GetOidcConfigurationStatus error: %v", err)

	assert.False(t, !status.EnvForced || status.EnvConfigured,
		"expected forced=false-override and not configured, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)

	// Enabled but missing fields
	t.Setenv("OIDC_ENABLED", "true")
	settingsSvc, err = newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s.settingsService = settingsSvc
	s.config.OidcEnabled = true
	status, err = s.GetOidcConfigurationStatus(context.Background())

	require.NoError(t, err,
		"GetOidcConfigurationStatus error: %v", err)

	assert.False(t, !status.EnvForced || status.EnvConfigured,
		"expected enabled but not configured, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)

	// Enabled and configured
	s.config.OidcClientID = "client-id"
	s.config.OidcIssuerURL = "https://example.com"
	status, err = s.GetOidcConfigurationStatus(context.Background())

	require.NoError(t, err,
		"GetOidcConfigurationStatus error: %v", err)

	assert.False(t, !status.EnvForced || !status.EnvConfigured,
		"expected enabled and configured, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)

}

func TestFindOrCreateOidcUser_MergeEnabled_EmailNotVerified_NoExistingUser_CreatesNewUser(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := user.NewUserService(db)
	authSvc := newTestAuthService()
	authSvc.userService = userSvc
	authSvc.settingsService = settingsSvc

	userInfo := auth.OidcUserInfo{
		Subject:       "sub-123",
		Email:         "new@example.com",
		EmailVerified: false, // provider omitted/false
	}

	created, isNew, err := authSvc.findOrCreateOidcUser(ctx, userInfo, &auth.OidcTokenResponse{AccessToken: "at"})
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotNil(t, created)
	require.NotNil(t, created.OidcSubjectId)
	require.Equal(t, userInfo.Subject, *created.OidcSubjectId)
	require.NotNil(t, created.Email)
	require.Equal(t, userInfo.Email, *created.Email)
	require.NotEmpty(t, created.Username)

	// Ensure the user actually persisted
	fetched, err := userSvc.GetUserByOidcSubjectId(ctx, userInfo.Subject)
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
}

func TestFindOrCreateOidcUser_MergeEnabled_EmailNotVerified_WithExistingUser_ReturnsError(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := user.NewUserService(db)
	// Seed an existing local user with matching email
	email := "existing@example.com"
	existing := &common.User{
		ID:       "u1",
		Username: "existing",
		Email:    &email,
	}
	_, err = userSvc.CreateUser(ctx, existing)
	require.NoError(t, err)

	authSvc := newTestAuthService()
	authSvc.userService = userSvc
	authSvc.settingsService = settingsSvc

	userInfo := auth.OidcUserInfo{
		Subject:       "sub-merge",
		Email:         email,
		EmailVerified: false,
		Extra: map[string]any{
			"email_verified": false,
		},
	}

	_, _, err = authSvc.findOrCreateOidcUser(ctx, userInfo, &auth.OidcTokenResponse{AccessToken: "at"})
	require.Error(t, err)

	// Ensure existing user is not linked
	fetched, err := userSvc.GetUserByID(ctx, existing.ID)
	require.NoError(t, err)
	require.True(t, fetched.OidcSubjectId == nil || *fetched.OidcSubjectId == "")
}

func TestFindOrCreateOidcUser_MergeEnabled_EmailVerificationMissing_WithExistingUser_Merges(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := user.NewUserService(db)
	// Seed an existing local user with matching email
	email := "existing@example.com"
	existing := &common.User{
		ID:       "u1",
		Username: "existing",
		Email:    &email,
	}
	_, err = userSvc.CreateUser(ctx, existing)
	require.NoError(t, err)

	authSvc := newTestAuthService()
	authSvc.userService = userSvc
	authSvc.settingsService = settingsSvc

	userInfo := auth.OidcUserInfo{
		Subject:       "sub-merge-missing-verified",
		Email:         email,
		EmailVerified: false,
		Extra:         map[string]any{},
	}

	mergedUser, isNew, err := authSvc.findOrCreateOidcUser(ctx, userInfo, &auth.OidcTokenResponse{AccessToken: "at"})
	require.NoError(t, err)
	require.False(t, isNew)
	require.NotNil(t, mergedUser)
	require.Equal(t, existing.ID, mergedUser.ID)

	// Ensure existing user is linked
	fetched, err := userSvc.GetUserByID(ctx, existing.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.OidcSubjectId)
	require.Equal(t, userInfo.Subject, *fetched.OidcSubjectId)
}

func TestAuthenticateLocalPrimary_EmailFallback(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db)
	settingsSvc, err := newSettingsServiceForAuthTestInternal(t, context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc

	hash, err := userSvc.HashPassword("hunter22!")
	require.NoError(t, err)
	dupHash, err := userSvc.HashPassword("dup-two-pass!")
	require.NoError(t, err)
	for _, u := range []*common.User{
		{ID: "u-email-login", Username: "bob", Email: new("bob@example.com"), PasswordHash: hash},
		{ID: "u-dup-1", Username: "dup1", Email: new("dup@example.com"), PasswordHash: hash},
		{ID: "u-dup-2", Username: "dup2", Email: new("dup@example.com"), PasswordHash: dupHash},
		{ID: "u-carol", Username: "carol@example.com", Email: new("carol@example.com"), PasswordHash: hash},
		{ID: "u-col-a", Username: "owner@example.com", PasswordHash: hash},
		{ID: "u-col-b", Username: "colb", Email: new("owner@example.com"), PasswordHash: dupHash},
	} {
		_, err = userSvc.CreateUser(context.Background(), u)
		require.NoError(t, err)
	}

	tests := []struct {
		name     string
		login    string
		password string
		wantID   string
		wantErr  error
	}{
		{name: "email resolves user", login: "bob@example.com", password: "hunter22!", wantID: "u-email-login"},
		{name: "unknown email", login: "nobody@example.com", password: "hunter22!", wantErr: ErrInvalidCredentials},
		{name: "duplicate email rejected", login: "dup@example.com", password: "dup-two-pass!", wantErr: ErrInvalidCredentials},
		{name: "username equal to own email", login: "carol@example.com", password: "hunter22!", wantID: "u-carol"},
		{name: "username-email collision rejected", login: "owner@example.com", password: "dup-two-pass!", wantErr: ErrInvalidCredentials},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.AuthenticateLocalPrimary(context.Background(), tc.login, tc.password)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantID, got.ID)
		})
	}
}
