package auth

import (
	"bytes"
	"context"
	"crypto/mldsa"
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/libtnb/sqlite"
	"github.com/samber/hot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/group"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/session"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/internal/version"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
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
		&audit.AuditLog{},
		&settings.SettingVariable{},
		&user.User{},
		&session.UserSession{},
		&environment.Environment{},
		&role.Role{},
		&role.RoleBinding{},
		&group.Group{},
		&group.GroupMember{},
		&group.OidcGroupMapping{},
		&apikey.ApiKey{},
		&role.ApiKeyPermission{},
	))
	return &database.DB{DB: db}
}

func newSettingsServiceForAuthTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	settingsEnv, err := settings.LoadEnv()
	require.NoError(t, err)
	svc, err := settings.NewSettingsService(ctx, db, &config.Config{}, settingsEnv)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func newTestSigningKey() *mldsa.PrivateKey {
	key, err := mldsa.GenerateKey(mldsa.MLDSA87())
	if err != nil {
		panic(err)
	}
	return key
}

func newTestAuthService() *AuthService {
	return &AuthService{
		signingKey:        newTestSigningKey(),
		browserSigningKey: bytes.Repeat([]byte{0x42}, browserSessionSigningKeySize),
		refreshExpiry:     24 * time.Hour,
		config:            &config.Config{},
		tokenCache: hot.NewHotCache[string, verifiedTokenEntry](hot.LRU, 4096).
			WithTTL(15 * time.Second).
			WithJanitor().
			Build(),
	}
}

func signLegacyToken(t testing.TB, key *mldsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": jwa.MLDSA87().String(), "typ": "JWT"})
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature, err := key.Sign(nil, []byte(signingInput), nil)
	require.NoError(t, err)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func signJWXToken(t testing.TB, key *mldsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	token, err := jwt.ParseInsecure(payload)
	require.NoError(t, err)
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.MLDSA87(), key))
	require.NoError(t, err)
	return string(signed)
}

func signUnsignedToken(t testing.TB, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	token, err := jwt.ParseInsecure(payload)
	require.NoError(t, err)
	signed, err := jwt.Sign(token, jwt.WithInsecureNoSignature())
	require.NoError(t, err)
	return string(signed)
}

func makeAccessToken(t *testing.T, key *mldsa.PrivateKey, subject, id, username string, _ []string, email, displayName string, exp time.Time, sessionIDs ...string) string {
	t.Helper()
	sessionID := ""
	if len(sessionIDs) > 0 {
		sessionID = sessionIDs[0]
	}
	claims := map[string]any{
		"jti":          id,
		"sub":          subject,
		"iat":          time.Now().Unix(),
		"exp":          exp.Unix(),
		"sid":          sessionID,
		"user_id":      id,
		"username":     username,
		"email":        email,
		"display_name": displayName,
		"app_version":  config.Version,
	}
	return signLegacyToken(t, key, claims)
}

func makeRefreshToken(t *testing.T, key *mldsa.PrivateKey, id string, exp time.Time, userIDAndSessionID ...string) string {
	t.Helper()
	userID := id
	sessionID := ""
	if len(userIDAndSessionID) > 0 {
		userID = userIDAndSessionID[0]
	}
	if len(userIDAndSessionID) > 1 {
		sessionID = userIDAndSessionID[1]
	}
	claims := map[string]any{
		"jti":         id,
		"sub":         "refresh",
		"iat":         time.Now().Unix(),
		"exp":         exp.Unix(),
		"user_id":     userID,
		"sid":         sessionID,
		"app_version": config.Version,
	}
	return signLegacyToken(t, key, claims)
}

func createTestSession(t *testing.T, db *database.DB, userID string, expiresAt time.Time) (*session.UserSession, string) {
	t.Helper()
	sessionSvc := session.NewSessionService(db)
	localSession, refreshJTI, err := sessionSvc.CreateSession(t.Context(), userID, expiresAt, types.SessionMeta{
		UserAgent: "test-agent",
		IPAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	return localSession, refreshJTI
}

func TestVerifyToken_ValidClaims(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	// Create user in DB
	localUser := &user.User{
		ID:          "u123",
		Username:    "alice",
		Email:       new("a@example.com"),
		DisplayName: new("Alice"),
	}
	_, err := userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, _ := createTestSession(t, db, "u123", exp)
	token := makeAccessToken(t, s.signingKey, "access", "u123", "alice", []string{"user", "admin"}, "a@example.com", "Alice", exp, localSession.ID)

	verifiedUser, _, err := s.VerifyToken(t.Context(), token)

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
	payload, err := json.Marshal(map[string]any{
		"jti":         "u1",
		"sub":         "access",
		"iat":         time.Now().Unix(),
		"exp":         exp.Unix(),
		"user_id":     "u1",
		"username":    "bob",
		"app_version": config.Version,
	})
	require.NoError(t, err)
	parsed, err := jwt.ParseInsecure(payload)
	require.NoError(t, err)
	token, err := jwt.Sign(parsed, jwt.WithKey(jwa.HS512(), s.browserSigningKey))
	require.NoError(t, err)

	_, _, err = s.VerifyToken(t.Context(), string(token))

	assert.ErrorIs(t, err, common.ErrInvalidToken,
		"want common.ErrInvalidToken, got %v", err)
}

func TestVerifyToken_Expired(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	// Create user in DB
	localUser := &user.User{
		ID:       "u1",
		Username: "bob",
	}
	_, err := userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(-1 * time.Minute)
	token := makeAccessToken(t, s.signingKey, "access", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err = s.VerifyToken(t.Context(), token)

	assert.ErrorIs(t, err, common.ErrExpiredToken,
		"want ErrExpiredToken, got %v", err)
}

func TestVerifyToken_InvalidSubject(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := makeAccessToken(t, s.signingKey, "refresh", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(t.Context(), token)
	require.ErrorIs(t, err, common.ErrTokenValidation)
}

func TestVerifyToken_InvalidSignature(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	otherKey := newTestSigningKey()

	token := makeAccessToken(t, otherKey, "access", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(t.Context(), token)

	assert.ErrorIs(t, err, common.ErrInvalidToken,
		"want common.ErrInvalidToken, got %v", err)
}

func TestVerifyToken_MissingUserID(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := makeAccessToken(t, s.signingKey, "access", "", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(t.Context(), token)
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

func TestUniqueOidcUsername(t *testing.T) {
	u := uniqueOidcUsername("alice", "1234567890abcdef")

	assert.Equal(t, "alice_90abcdef", u,
		"suffixed username %q", u)

	u2 := uniqueOidcUsername("alice", "short")

	assert.Equal(t, "alice_short", u2,
		"short subject username %q", u2)
}

func TestPersistOidcTokens_SetsFields(t *testing.T) {
	s := newTestAuthService()
	localUser := &user.User{}
	start := time.Now()
	resp := &types.OidcTokenResponse{
		AccessToken:  "at-123",
		RefreshToken: "rt-456",
		ExpiresIn:    7,
		IDToken:      "",
	}
	s.persistOidcTokens(localUser, resp)

	assert.False(t, localUser.OidcAccessToken == nil || *localUser.OidcAccessToken != "at-123",
		"access token %v", localUser.OidcAccessToken)

	assert.False(t, localUser.OidcRefreshToken == nil || *localUser.OidcRefreshToken != "rt-456",
		"refresh token %v", localUser.OidcRefreshToken)

	assert.NotNil(t, localUser.OidcAccessTokenExpiresAt,
		"expiresAt nil")

	// Check approx expiry within [start+7s, start+12s] to allow CI slop
	earliest := start.Add(7 * time.Second)
	latest := start.Add(12 * time.Second)

	assert.False(t, localUser.OidcAccessTokenExpiresAt.Before(earliest) || localUser.OidcAccessTokenExpiresAt.After(latest),
		"expiresAt %v not in [%v,%v]", localUser.OidcAccessTokenExpiresAt, earliest, latest)
}

func TestVerifyToken_VersionMismatch(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)

	oldVersion := config.Version
	config.Version = "1.0.0"
	token := makeAccessToken(t, s.signingKey, "access", "u1", "bob", []string{"user"}, "", "", exp, "session-version-mismatch")
	config.Version = "2.0.0"

	_, _, err := s.VerifyToken(t.Context(), token)

	assert.ErrorIs(t, err, common.ErrTokenVersionMismatch,
		"want ErrTokenVersionMismatch, got %v", err)

	config.Version = oldVersion
}

func TestRefreshToken_Valid(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-refresh",
		Username: "refresh-user",
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, refreshJTI := createTestSession(t, db, "u-refresh", exp)
	token := makeRefreshToken(t, s.signingKey, refreshJTI, exp, "u-refresh", localSession.ID)

	tokenPair, err := s.RefreshToken(t.Context(), token, types.SessionMeta{})
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.NotEmpty(t, tokenPair.AccessToken)
	require.NotEmpty(t, tokenPair.BrowserToken)
	require.NotEmpty(t, tokenPair.RefreshToken)
	require.Less(t, len(tokenPair.BrowserToken), 1024)

	accessAlgorithm, ok := signedTokenAlgorithm(tokenPair.AccessToken)
	require.True(t, ok)
	require.Equal(t, jwa.MLDSA87().String(), accessAlgorithm)
	browserAlgorithm, ok := signedTokenAlgorithm(tokenPair.BrowserToken)
	require.True(t, ok)
	require.Equal(t, jwa.HS512().String(), browserAlgorithm)
	refreshAlgorithm, ok := signedTokenAlgorithm(tokenPair.RefreshToken)
	require.True(t, ok)
	require.Equal(t, jwa.MLDSA87().String(), refreshAlgorithm)
}

// A refresh token minted under an older config.Version must still refresh successfully
// and the newly issued tokens must carry the current config.Version. This is what keeps
// users logged in across backend releases — see plans/how-can-we-make-compiled-quilt.md.
func TestRefreshToken_VersionMismatchRotates(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-versionmismatch",
		Username: "versionmismatch-user",
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, refreshJTI := createTestSession(t, db, localUser.ID, exp)

	oldVersion := config.Version
	t.Cleanup(func() { config.Version = oldVersion })
	config.Version = "1.0.0"
	token := makeRefreshToken(t, s.signingKey, refreshJTI, exp, localUser.ID, localSession.ID)
	config.Version = "2.0.0"

	tokenPair, err := s.RefreshToken(t.Context(), token, types.SessionMeta{})
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.NotEmpty(t, tokenPair.AccessToken)
	require.NotEmpty(t, tokenPair.RefreshToken)

	parsedAccess, err := jwt.ParseString(tokenPair.AccessToken, jwt.WithKey(jwa.MLDSA87(), s.signingKey.PublicKey()))
	require.NoError(t, err)
	accessVersion, err := jwt.Get[string](parsedAccess, claimAppVersion)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", accessVersion)

	parsedBrowser, err := jwt.ParseString(tokenPair.BrowserToken, jwt.WithKey(jwa.HS512(), s.browserSigningKey))
	require.NoError(t, err)
	browserSubject, ok := parsedBrowser.Subject()
	require.True(t, ok)
	require.Equal(t, browserTokenSubject, browserSubject)
	browserSessionID, err := jwt.Get[string](parsedBrowser, claimSessionID)
	require.NoError(t, err)
	require.Equal(t, localSession.ID, browserSessionID)
	browserUserID, err := jwt.Get[string](parsedBrowser, claimUserID)
	require.NoError(t, err)
	require.Equal(t, localUser.ID, browserUserID)
	browserVersion, err := jwt.Get[string](parsedBrowser, claimAppVersion)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", browserVersion)

	parsedRefresh, err := jwt.ParseString(tokenPair.RefreshToken, jwt.WithKey(jwa.MLDSA87(), s.signingKey.PublicKey()))
	require.NoError(t, err)
	refreshVersion, err := jwt.Get[string](parsedRefresh, claimAppVersion)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", refreshVersion)
}

func TestVerifyToken_RejectsRevokedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-revoked",
		Username: "revoked-user",
	}
	_, err := userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, _ := createTestSession(t, db, localUser.ID, exp)
	token := makeAccessToken(t, s.signingKey, "access", localUser.ID, localUser.Username, []string{"user"}, "", "", exp, localSession.ID)
	browser, err := jwt.NewBuilder().
		JwtID("revoked-browser-token").
		Subject(browserTokenSubject).
		IssuedAt(time.Now()).
		Expiration(exp).
		Claim(claimSessionID, localSession.ID).
		Claim(claimUserID, localUser.ID).
		Build()
	require.NoError(t, err)
	browserToken, err := jwt.Sign(browser, jwt.WithKey(jwa.HS512(), s.browserSigningKey))
	require.NoError(t, err)
	require.NoError(t, s.RevokeSession(t.Context(), localSession.ID))

	_, _, err = s.VerifyToken(t.Context(), token)
	require.ErrorIs(t, err, common.ErrSessionRevoked)
	_, _, err = s.VerifyBrowserToken(t.Context(), string(browserToken))
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestVerifyToken_RejectsMissingSessionID(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-no-sid",
		Username: "no-sid-user",
	}
	_, err := userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	token := makeAccessToken(t, s.signingKey, "access", localUser.ID, localUser.Username, []string{"user"}, "", "", time.Now().Add(5*time.Minute))

	_, _, err = s.VerifyToken(t.Context(), token)
	require.ErrorIs(t, err, common.ErrTokenValidation)
}

func TestRevokeSessionThenVerifyTokenFails(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-logout",
		Username: "logout-user",
	}
	_, err := userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, _ := createTestSession(t, db, localUser.ID, exp)
	token := makeAccessToken(t, s.signingKey, "access", localUser.ID, localUser.Username, []string{"user"}, "", "", exp, localSession.ID)
	require.NoError(t, s.RevokeSession(t.Context(), localSession.ID))

	_, _, err = s.VerifyToken(t.Context(), token)
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestVerifyToken_RejectsRevokedCachedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-cached-revoked",
		Username: "cached-revoked-user",
	}
	_, err := userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	sessionRecord, _ := createTestSession(t, db, localUser.ID, exp)
	token := makeAccessToken(t, s.signingKey, "access", localUser.ID, localUser.Username, []string{"user"}, "", "", exp, sessionRecord.ID)

	_, _, err = s.VerifyToken(t.Context(), token)
	require.NoError(t, err)
	require.NoError(t, s.RevokeSession(t.Context(), sessionRecord.ID))

	_, _, err = s.VerifyToken(t.Context(), token)
	require.ErrorIs(t, err, common.ErrSessionRevoked, "expected cached access token to be rejected after revocation, got %v", err)
}

func TestRefreshToken_RotatesJTI(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-rotate",
		Username: "rotate-user",
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, refreshJTI := createTestSession(t, db, localUser.ID, exp)
	token := makeRefreshToken(t, s.signingKey, refreshJTI, exp, localUser.ID, localSession.ID)

	tokenPair, err := s.RefreshToken(t.Context(), token, types.SessionMeta{})
	require.NoError(t, err)
	require.NotEmpty(t, tokenPair.RefreshToken)

	_, err = s.RefreshToken(t.Context(), token, types.SessionMeta{})
	require.ErrorIs(t, err, common.ErrInvalidToken)
}

func TestRefreshToken_AcceptsBrowserRefreshToken(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-browser-refresh",
		Username: "browser-refresh-user",
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, refreshJTI := createTestSession(t, db, localUser.ID, exp)
	first, err := s.RefreshToken(t.Context(), makeRefreshToken(t, s.signingKey, refreshJTI, exp, localUser.ID, localSession.ID), types.SessionMeta{})
	require.NoError(t, err)
	require.NotEmpty(t, first.BrowserRefresh)
	require.Less(t, len(first.BrowserRefresh), 4096)
	require.WithinDuration(t, exp, first.RefreshExpiresAt, time.Second)

	second, err := s.RefreshToken(t.Context(), first.BrowserRefresh, types.SessionMeta{})
	require.NoError(t, err)

	_, err = s.RefreshToken(t.Context(), first.RefreshToken, types.SessionMeta{})
	require.ErrorIs(t, err, common.ErrInvalidToken)
	_, err = s.RefreshToken(t.Context(), first.BrowserRefresh, types.SessionMeta{})
	require.ErrorIs(t, err, common.ErrInvalidToken)

	require.NoError(t, s.RevokeSessionByRefreshToken(t.Context(), second.BrowserRefresh))
	_, err = s.RefreshToken(t.Context(), second.BrowserRefresh, types.SessionMeta{})
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestRefreshToken_RejectsRevokedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = session.NewSessionService(db)

	localUser := &user.User{
		ID:       "u-refresh-revoked",
		Username: "refresh-revoked-user",
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, refreshJTI := createTestSession(t, db, localUser.ID, exp)
	require.NoError(t, s.RevokeSession(t.Context(), localSession.ID))
	token := makeRefreshToken(t, s.signingKey, refreshJTI, exp, localUser.ID, localSession.ID)

	_, err = s.RefreshToken(t.Context(), token, types.SessionMeta{})
	require.ErrorIs(t, err, common.ErrSessionRevoked)
}

func TestChangePassword_RevokesAllSessions(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	passwordHash, err := userSvc.HashPassword(t.Context(), "old-password")
	require.NoError(t, err)
	localUser := &user.User{
		ID:           "u-password",
		Username:     "password-user",
		PasswordHash: passwordHash,
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	sessionA, _ := createTestSession(t, db, localUser.ID, time.Now().Add(time.Hour))
	sessionB, _ := createTestSession(t, db, localUser.ID, time.Now().Add(time.Hour))

	require.NoError(t, s.ChangePassword(t.Context(), localUser.ID, "old-password", "New-password1!", ""))

	sessionA, err = s.sessionService.GetSessionByID(t.Context(), sessionA.ID)
	require.NoError(t, err)
	sessionB, err = s.sessionService.GetSessionByID(t.Context(), sessionB.ID)
	require.NoError(t, err)
	require.NotNil(t, sessionA.RevokedAt)
	require.NotNil(t, sessionB.RevokedAt)
}

func TestChangePassword_KeepsCurrentSessionAlive(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	s := newTestAuthService()
	s.userService = userSvc
	s.sessionService = session.NewSessionService(db)

	passwordHash, err := userSvc.HashPassword(t.Context(), "old-password")
	require.NoError(t, err)
	localUser := &user.User{
		ID:           "u-keep",
		Username:     "keep-user",
		PasswordHash: passwordHash,
	}
	_, err = userSvc.CreateUser(t.Context(), localUser)
	require.NoError(t, err)

	current, _ := createTestSession(t, db, localUser.ID, time.Now().Add(time.Hour))
	other, _ := createTestSession(t, db, localUser.ID, time.Now().Add(time.Hour))

	require.NoError(t, s.ChangePassword(t.Context(), localUser.ID, "old-password", "New-password1!", current.ID))

	current, err = s.sessionService.GetSessionByID(t.Context(), current.ID)
	require.NoError(t, err)
	other, err = s.sessionService.GetSessionByID(t.Context(), other.ID)
	require.NoError(t, err)
	require.Nil(t, current.RevokedAt, "current session should remain active")
	require.NotNil(t, other.RevokedAt, "other sessions should be revoked")
}

func TestRefreshToken_RejectsNonHMACAlg(t *testing.T) {
	s := newTestAuthService()
	exp := time.Now().Add(5 * time.Minute)
	token := signUnsignedToken(t, map[string]any{
		"jti": "u1",
		"sub": "refresh",
		"iat": time.Now().Unix(),
		"exp": exp.Unix(),
	})

	_, err := s.RefreshToken(t.Context(), token, types.SessionMeta{})

	assert.ErrorIs(t, err, common.ErrInvalidToken,
		"want common.ErrInvalidToken, got %v", err)
}

func TestGetOidcConfigurationStatus(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	s := newTestAuthService()
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s.settingsService = settingsSvc

	status, err := s.GetOidcConfigurationStatus(t.Context())
	require.NoError(t, err)
	assert.False(t, status.MergeAccounts)
	assert.Empty(t, status.ProviderName)
	assert.Empty(t, status.ProviderLogoUrl)

	t.Setenv("ARCANE_OIDC_PROVIDER_NAME", "Env Provider")
	t.Setenv("ARCANE_OIDC_PROVIDER_LOGO_URL", "https://example.com/logo.png")
	settingsSvc, err = newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s.settingsService = settingsSvc

	status, err = s.GetOidcConfigurationStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Env Provider", status.ProviderName)
	assert.Equal(t, "https://example.com/logo.png", status.ProviderLogoUrl)
}

func TestFindOrCreateOidcUser_MergeEnabled_EmailNotVerified_NoExistingUser_CreatesNewUser(t *testing.T) {
	ctx := t.Context()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := newSettingsServiceForAuthTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	authSvc := newTestAuthService()
	authSvc.userService = userSvc
	authSvc.settingsService = settingsSvc
	authSvc.groupService = group.NewGroupService(db, role.NewRoleService(db), func(context.Context) string {
		return ""
	})

	existing, err := userSvc.CreateUser(ctx, &user.User{
		ID: "existing-canonical-identity", Username: "José", Email: new("néw@example.com"),
	})
	require.NoError(t, err)

	userInfo := types.OidcUserInfo{
		Subject:           "sub-123",
		Email:             " ne\u0301w@example.com ",
		PreferredUsername: " Jose\u0301 ",
		Name:              " Jose\u0301 ",
		EmailVerified:     false, // provider omitted/false
	}

	created, isNew, err := authSvc.findOrCreateOidcUser(ctx, userInfo, &types.OidcTokenResponse{AccessToken: "at"})
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotEqual(t, existing.ID, created.ID)
	require.NotNil(t, created)
	require.NotNil(t, created.OidcSubjectId)
	require.Equal(t, userInfo.Subject, *created.OidcSubjectId)
	require.NotNil(t, created.Email)
	require.Equal(t, userInfo.Email, *created.Email)
	require.Equal(t, "Jose\u0301", created.Username)
	require.Equal(t, "José", *created.DisplayName)
	require.NotEmpty(t, created.Username)

	// Ensure the user actually persisted
	fetched, err := userSvc.GetUserByOidcSubjectId(ctx, userInfo.Subject)
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	otherInfo := userInfo
	otherInfo.Subject = "another-subject"
	otherInfo.Email = "other@example.com"
	other, isNew, err := authSvc.findOrCreateOidcUser(ctx, otherInfo, &types.OidcTokenResponse{AccessToken: "other-token"})
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotEqual(t, created.ID, other.ID)
	require.NotEqual(t, created.Username, other.Username)
}

func TestFindOrCreateOidcUser_MergeEnabled_WithExistingUser(t *testing.T) {
	tests := []struct {
		name          string
		emailVerified bool
		extra         map[string]any
		wantMerge     bool
	}{
		{name: "verified email merges", emailVerified: true, extra: map[string]any{"email_verified": true}, wantMerge: true},
		{name: "explicit false rejected", extra: map[string]any{"email_verified": false}},
		{name: "missing claim with nil extra rejected"},
		{name: "missing claim with empty extra rejected", extra: map[string]any{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db := setupAuthServiceTestDB(t)

			settingsSvc, err := newSettingsServiceForAuthTest(t, ctx, db)
			require.NoError(t, err)
			require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

			userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
			email := "existing@example.com"
			existing := &user.User{ID: "u1", Username: "existing", Email: &email}
			_, err = userSvc.CreateUser(ctx, existing)
			require.NoError(t, err)

			authSvc := newTestAuthService()
			authSvc.userService = userSvc
			authSvc.settingsService = settingsSvc
			authSvc.groupService = group.NewGroupService(db, role.NewRoleService(db), func(context.Context) string {
				return ""
			})

			userInfo := types.OidcUserInfo{
				Subject:       "sub-merge",
				Email:         email,
				Name:          "OIDC Name",
				EmailVerified: tc.emailVerified,
				Extra:         tc.extra,
			}

			got, isNew, err := authSvc.findOrCreateOidcUser(ctx, userInfo, &types.OidcTokenResponse{AccessToken: "at", RefreshToken: "rt", ExpiresIn: 60})
			fetched, fetchErr := userSvc.GetUserByID(ctx, existing.ID)
			require.NoError(t, fetchErr)

			if tc.wantMerge {
				require.NoError(t, err)
				require.False(t, isNew)
				require.Equal(t, existing.ID, got.ID)
				require.NotNil(t, fetched.OidcSubjectId)
				require.Equal(t, userInfo.Subject, *fetched.OidcSubjectId)
				require.NotNil(t, fetched.OidcAccessToken)
				require.Equal(t, "at", *fetched.OidcAccessToken)
				return
			}

			require.EqualError(t, err, "email not verified by OIDC provider; cannot merge accounts")
			require.Nil(t, got)
			require.False(t, isNew)

			var userCount int64
			require.NoError(t, db.Model(&user.User{}).Count(&userCount).Error)
			require.Equal(t, int64(1), userCount, "no separate account should be created")
			var bindingCount int64
			require.NoError(t, db.Model(&role.RoleBinding{}).Where("user_id = ?", existing.ID).Count(&bindingCount).Error)
			require.Zero(t, bindingCount)

			require.True(t, fetched.OidcSubjectId == nil || *fetched.OidcSubjectId == "")
			require.Nil(t, fetched.OidcAccessToken)
			require.Nil(t, fetched.OidcRefreshToken)
			require.Nil(t, fetched.OidcAccessTokenExpiresAt)
			require.Nil(t, fetched.DisplayName)
			require.Nil(t, fetched.LastLogin)
		})
	}
}

func TestAuthenticateLocalPrimary_EmailFallback(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	settingsSvc, err := newSettingsServiceForAuthTest(t, t.Context(), db)
	require.NoError(t, err)
	s := newTestAuthService()
	s.userService = userSvc
	s.settingsService = settingsSvc

	hash, err := userSvc.HashPassword(t.Context(), "hunter22!")
	require.NoError(t, err)
	dupHash, err := userSvc.HashPassword(t.Context(), "dup-two-pass!")
	require.NoError(t, err)
	for _, u := range []*user.User{
		{ID: "u-email-login", Username: "bob", Email: new("bob@example.com"), PasswordHash: hash},
		{ID: "u-unicode", Username: "José", Email: new("josé@example.com"), PasswordHash: hash},
		{ID: "u-decomposed", Username: "Jose\u0301", Email: new("jose\u0301@example.com"), PasswordHash: hash},
		{ID: "u-dup-1", Username: "dup1", Email: new("dup@example.com"), PasswordHash: hash},
		{ID: "u-carol", Username: "carol@example.com", Email: new("carol@example.com"), PasswordHash: hash},
		{ID: "u-col-a", Username: "owner@example.com", PasswordHash: hash},
		{ID: "u-col-b", Username: "colb", Email: new("owner@example.com"), PasswordHash: dupHash},
	} {
		_, err = userSvc.CreateUser(t.Context(), u)
		require.NoError(t, err)
	}

	require.NoError(t, db.Create(&user.User{ID: "u-dup-2", Username: "dup2", Email: new("dup@example.com"), PasswordHash: dupHash}).Error)
	require.NoError(t, db.Create(&user.User{
		ID: "u-legacy-spaces", Username: " Jose\u0301 ", Email: new(" spaced@example.com "), PasswordHash: hash,
	}).Error)

	tests := []struct {
		name     string
		login    string
		password string
		wantID   string
		wantErr  error
	}{
		{name: "composed username", login: "José", password: "hunter22!", wantID: "u-unicode"},
		{name: "decomposed username stays distinct", login: "Jose\u0301", password: "hunter22!", wantID: "u-decomposed"},
		{name: "composed email", login: "josé@example.com", password: "hunter22!", wantID: "u-unicode"},
		{name: "decomposed email stays distinct", login: "jose\u0301@example.com", password: "hunter22!", wantID: "u-decomposed"},
		{name: "legacy username whitespace", login: " Jose\u0301 ", password: "hunter22!", wantID: "u-legacy-spaces"},
		{name: "legacy email whitespace", login: " spaced@example.com ", password: "hunter22!", wantID: "u-legacy-spaces"},
		{name: "username whitespace is not a new alias", login: " José ", password: "hunter22!", wantErr: ErrInvalidCredentials},
		{name: "email whitespace is not a new alias", login: " josé@example.com ", password: "hunter22!", wantErr: ErrInvalidCredentials},
		{name: "password not trimmed", login: "José", password: " hunter22! ", wantErr: ErrInvalidCredentials},
		{name: "email resolves user", login: "bob@example.com", password: "hunter22!", wantID: "u-email-login"},
		{name: "unknown email", login: "nobody@example.com", password: "hunter22!", wantErr: ErrInvalidCredentials},
		{name: "duplicate email rejected", login: "dup@example.com", password: "dup-two-pass!", wantErr: ErrInvalidCredentials},
		{name: "username equal to own email", login: "carol@example.com", password: "hunter22!", wantID: "u-carol"},
		{name: "username-email collision rejected", login: "owner@example.com", password: "dup-two-pass!", wantErr: ErrInvalidCredentials},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, authenticateLocalPrimaryErr := s.AuthenticateLocalPrimary(t.Context(), tc.login, tc.password)
			if tc.wantErr != nil {
				require.ErrorIs(t, authenticateLocalPrimaryErr, tc.wantErr)
				return
			}
			require.NoError(t, authenticateLocalPrimaryErr)
			require.Equal(t, tc.wantID, got.ID)
		})
	}
}

func TestGetSessionBootstrap(t *testing.T) {
	tests := []struct {
		name    string
		ps      *authz.PermissionSet
		wantEnv []string
	}{
		{
			name:    "global list permission returns every environment",
			ps:      &authz.PermissionSet{Global: map[string]struct{}{authz.PermEnvironmentsList: {}}},
			wantEnv: []string{"env-a", "env-b"},
		},
		{
			name:    "environment grants restrict the list",
			ps:      &authz.PermissionSet{PerEnv: map[string]map[string]struct{}{"env-b": {authz.PermEnvironmentsList: {}}}},
			wantEnv: []string{"env-b"},
		},
		{
			name:    "missing permissions return no environments",
			wantEnv: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupAuthServiceTestDB(t)
			userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
			_, err := userSvc.CreateUser(t.Context(), &user.User{
				ID:       "u-bootstrap",
				Username: "bootstrap",
			})
			require.NoError(t, err)
			require.NoError(t, db.Create(&[]environment.Environment{
				{ID: "env-a", Name: "A", Enabled: true},
				{ID: "env-b", Name: "B", Enabled: true},
			}).Error)
			versionSettings, err := newSettingsServiceForAuthTest(t, t.Context(), db)
			require.NoError(t, err)
			require.NoError(t, versionSettings.SetBoolSetting(t.Context(), "updateCheckEnabled", false))
			s := &AuthService{
				userService:        userSvc,
				environmentService: environment.NewEnvironmentService(db, http.DefaultClient, nil, nil, nil, nil),
				versionService:     version.NewVersionService(nil, "1.2.3", "revision", nil, nil, nil, versionSettings),
			}

			bootstrap, err := s.GetSessionBootstrap(t.Context(), "u-bootstrap", tt.ps)

			require.NoError(t, err)
			assert.Equal(t, "u-bootstrap", bootstrap.User.ID)
			assert.Equal(t, "v1.2.3", bootstrap.Version.CurrentVersion)
			assert.True(t, bootstrap.Environments.Success)
			envIDs := make([]string, 0, len(bootstrap.Environments.Data))
			for _, env := range bootstrap.Environments.Data {
				envIDs = append(envIDs, env.ID)
			}
			assert.ElementsMatch(t, tt.wantEnv, envIDs)
			assert.NotEmpty(t, bootstrap.PermissionsManifest.Resources)
		})
	}

	t.Run("unknown user fails", func(t *testing.T) {
		db := setupAuthServiceTestDB(t)
		versionSettings, err := newSettingsServiceForAuthTest(t, t.Context(), db)
		require.NoError(t, err)
		require.NoError(t, versionSettings.SetBoolSetting(t.Context(), "updateCheckEnabled", false))
		s := &AuthService{
			userService:        user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB),
			environmentService: environment.NewEnvironmentService(db, http.DefaultClient, nil, nil, nil, nil),
			versionService:     version.NewVersionService(nil, "1.2.3", "revision", nil, nil, nil, versionSettings),
		}

		_, err = s.GetSessionBootstrap(t.Context(), "missing", nil)

		require.Error(t, err)
	})
}
