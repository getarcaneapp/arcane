package services

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cache"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/golang-jwt/jwt/v5"
)

func setupAuthServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}, &models.User{}, &models.UserSession{}))
	return &database.DB{DB: db}
}

func newTestAuthService(secret string) *AuthService {
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		return &AuthService{
			jwtSecret:     b,
			refreshExpiry: 24 * time.Hour,
			config:        &config.Config{},
			tokenCache:    cache.NewTTL[verifiedTokenEntry](15 * time.Second),
		}
	}
	return &AuthService{
		jwtSecret:     []byte(secret),
		refreshExpiry: 24 * time.Hour,
		config:        &config.Config{},
		tokenCache:    cache.NewTTL[verifiedTokenEntry](15 * time.Second),
	}
}

func makeAccessToken(t *testing.T, secret []byte, subject string, id string, username string, roles []string, email, displayName string, exp time.Time, sessionIDs ...string) string {
	t.Helper()
	sessionID := ""
	if len(sessionIDs) > 0 {
		sessionID = sessionIDs[0]
	}
	claims := userClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        id,
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		SessionID:   sessionID,
		UserID:      id,
		Username:    username,
		Roles:       roles,
		Email:       email,
		DisplayName: displayName,
		AppVersion:  config.Version,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign:  %v", err)
	}
	return signed
}

func makeRefreshToken(t *testing.T, secret []byte, subject string, id string, exp time.Time, userIDAndSessionID ...string) string {
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
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        id,
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		UserID:     userID,
		SessionID:  sessionID,
		AppVersion: config.Version,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func createTestSession(t *testing.T, db *database.DB, userID string, expiresAt time.Time) (*models.UserSession, string) {
	t.Helper()
	sessionSvc := NewSessionService(db)
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
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	return signed
}

func TestVerifyToken_ValidClaims(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	// Create user in DB
	user := &models.User{
		BaseModel:   models.BaseModel{ID: "u123"},
		Username:    "alice",
		Email:       new("a@example.com"),
		DisplayName: new("Alice"),
		Roles:       models.StringSlice{"user", "admin"},
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _ := createTestSession(t, db, "u123", exp)
	token := makeAccessToken(t, s.jwtSecret, "access", "u123", "alice", []string{"user", "admin"}, "a@example.com", "Alice", exp, session.ID)

	verifiedUser, _, err := s.VerifyToken(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	if verifiedUser.ID != "u123" {
		t.Errorf("id %q", verifiedUser.ID)
	}
	if verifiedUser.Username != "alice" {
		t.Errorf("username %q", verifiedUser.Username)
	}
	if len(verifiedUser.Roles) != 2 || verifiedUser.Roles[0] != "user" || verifiedUser.Roles[1] != "admin" {
		t.Errorf("roles %v", verifiedUser.Roles)
	}
	if verifiedUser.Email == nil || *verifiedUser.Email != "a@example.com" {
		t.Errorf("email %v", verifiedUser.Email)
	}
	if verifiedUser.DisplayName == nil || *verifiedUser.DisplayName != "Alice" {
		t.Errorf("displayName %v", verifiedUser.DisplayName)
	}
}

func TestVerifyToken_RejectsNonHMACAlg(t *testing.T) {
	s := newTestAuthService("")
	exp := time.Now().Add(5 * time.Minute)
	token := makeUnsignedToken(t, userClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        "u1",
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		UserID:     "u1",
		Username:   "bob",
		Roles:      []string{"user"},
		AppVersion: config.Version,
	})

	_, _, err := s.VerifyToken(context.Background(), token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyToken_Expired(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	// Create user in DB
	user := &models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "bob",
		Roles:     models.StringSlice{"user"},
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(-1 * time.Minute)
	token := makeAccessToken(t, s.jwtSecret, "access", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err = s.VerifyToken(context.Background(), token)
	if !errors.Is(err, ErrExpiredToken) {
		t.Errorf("want ErrExpiredToken, got %v", err)
	}
}

func TestVerifyToken_InvalidSubject(t *testing.T) {
	s := newTestAuthService("")
	exp := time.Now().Add(5 * time.Minute)
	token := makeAccessToken(t, s.jwtSecret, "refresh", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(context.Background(), token)
	require.ErrorAs(t, err, new(*common.AccessTokenSubjectError))
}

func TestVerifyToken_InvalidSignature(t *testing.T) {
	s := newTestAuthService("")
	exp := time.Now().Add(5 * time.Minute)
	otherSecret := make([]byte, 32)
	if _, err := rand.Read(otherSecret); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	token := makeAccessToken(t, otherSecret, "access", "u1", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(context.Background(), token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyToken_MissingUserID(t *testing.T) {
	s := newTestAuthService("")
	exp := time.Now().Add(5 * time.Minute)
	token := makeAccessToken(t, s.jwtSecret, "access", "", "bob", []string{"user"}, "", "", exp)

	_, _, err := s.VerifyToken(context.Background(), token)
	require.ErrorAs(t, err, new(*common.MissingTokenUserIDError))
}

func TestGenerateUsernameFromEmail(t *testing.T) {
	u := generateUsernameFromEmail("john.doe@example.com", "sub-abcdef01")
	if u != "john.doe" {
		t.Errorf("username %q", u)
	}
	u2 := generateUsernameFromEmail("", "1234567890abcdef")
	if u2 != "user_90abcdef" {
		t.Errorf("fallback username %q", u2)
	}
	u3 := generateUsernameFromEmail("", "short")
	if u3 != "user_short" {
		t.Errorf("short subject username %q", u3)
	}
}

func TestPersistOidcTokens_SetsFields(t *testing.T) {
	s := newTestAuthService("")
	user := &models.User{}
	start := time.Now()
	resp := &auth.OidcTokenResponse{
		AccessToken:  "at-123",
		RefreshToken: "rt-456",
		ExpiresIn:    7,
		IDToken:      "",
	}
	s.persistOidcTokens(user, resp)

	if user.OidcAccessToken == nil || *user.OidcAccessToken != "at-123" {
		t.Errorf("access token %v", user.OidcAccessToken)
	}
	if user.OidcRefreshToken == nil || *user.OidcRefreshToken != "rt-456" {
		t.Errorf("refresh token %v", user.OidcRefreshToken)
	}
	if user.OidcAccessTokenExpiresAt == nil {
		t.Errorf("expiresAt nil")
	}
	// Check approx expiry within [start+7s, start+12s] to allow CI slop
	min := start.Add(7 * time.Second)
	max := start.Add(12 * time.Second)
	if user.OidcAccessTokenExpiresAt.Before(min) || user.OidcAccessTokenExpiresAt.After(max) {
		t.Errorf("expiresAt %v not in [%v,%v]", user.OidcAccessTokenExpiresAt, min, max)
	}
}

func TestVerifyToken_VersionMismatch(t *testing.T) {
	s := newTestAuthService("")
	exp := time.Now().Add(5 * time.Minute)

	oldVersion := config.Version
	config.Version = "1.0.0"
	token := makeAccessToken(t, s.jwtSecret, "access", "u1", "bob", []string{"user"}, "", "", exp)
	config.Version = "2.0.0"

	_, _, err := s.VerifyToken(context.Background(), token)
	if !errors.Is(err, ErrTokenVersionMismatch) {
		t.Errorf("want ErrTokenVersionMismatch, got %v", err)
	}

	config.Version = oldVersion
}

func TestRefreshToken_Valid(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	settingsSvc, err := NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService("")
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = NewSessionService(db)

	user := &models.User{
		BaseModel: models.BaseModel{ID: "u-refresh"},
		Username:  "refresh-user",
		Roles:     models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, "u-refresh", exp)
	token := makeRefreshToken(t, s.jwtSecret, "refresh", refreshJTI, exp, "u-refresh", session.ID)

	tokenPair, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.NoError(t, err)
	require.NotNil(t, tokenPair)
	require.NotEmpty(t, tokenPair.AccessToken)
	require.NotEmpty(t, tokenPair.RefreshToken)
}

func TestVerifyToken_RejectsRevokedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	user := &models.User{
		BaseModel: models.BaseModel{ID: "u-revoked"},
		Username:  "revoked-user",
		Roles:     models.StringSlice{"user"},
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _ := createTestSession(t, db, user.ID, exp)
	require.NoError(t, s.RevokeSession(context.Background(), session.ID))
	token := makeAccessToken(t, s.jwtSecret, "access", user.ID, user.Username, []string{"user"}, "", "", exp, session.ID)

	_, _, err = s.VerifyToken(context.Background(), token)
	require.True(t, common.IsSessionRevokedError(err))
}

func TestVerifyToken_RejectsMissingSessionID(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	user := &models.User{
		BaseModel: models.BaseModel{ID: "u-no-sid"},
		Username:  "no-sid-user",
		Roles:     models.StringSlice{"user"},
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	token := makeAccessToken(t, s.jwtSecret, "access", user.ID, user.Username, []string{"user"}, "", "", time.Now().Add(5*time.Minute))

	_, _, err = s.VerifyToken(context.Background(), token)
	require.ErrorAs(t, err, new(*common.MissingTokenSessionIDError))
}

func TestRevokeSessionThenVerifyTokenFails(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	user := &models.User{
		BaseModel: models.BaseModel{ID: "u-logout"},
		Username:  "logout-user",
		Roles:     models.StringSlice{"user"},
	}
	_, err := userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _ := createTestSession(t, db, user.ID, exp)
	token := makeAccessToken(t, s.jwtSecret, "access", user.ID, user.Username, []string{"user"}, "", "", exp, session.ID)
	require.NoError(t, s.RevokeSession(context.Background(), session.ID))

	_, _, err = s.VerifyToken(context.Background(), token)
	require.True(t, common.IsSessionRevokedError(err))
}

func TestRefreshToken_RotatesJTI(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	settingsSvc, err := NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService("")
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = NewSessionService(db)

	user := &models.User{
		BaseModel: models.BaseModel{ID: "u-rotate"},
		Username:  "rotate-user",
		Roles:     models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, user.ID, exp)
	token := makeRefreshToken(t, s.jwtSecret, "refresh", refreshJTI, exp, user.ID, session.ID)

	tokenPair, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.NoError(t, err)
	require.NotEmpty(t, tokenPair.RefreshToken)

	_, err = s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestRefreshToken_RejectsRevokedSession(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	settingsSvc, err := NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	s := newTestAuthService("")
	s.userService = userSvc
	s.settingsService = settingsSvc
	s.sessionService = NewSessionService(db)

	user := &models.User{
		BaseModel: models.BaseModel{ID: "u-refresh-revoked"},
		Username:  "refresh-revoked-user",
		Roles:     models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, refreshJTI := createTestSession(t, db, user.ID, exp)
	require.NoError(t, s.RevokeSession(context.Background(), session.ID))
	token := makeRefreshToken(t, s.jwtSecret, "refresh", refreshJTI, exp, user.ID, session.ID)

	_, err = s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	require.True(t, common.IsSessionRevokedError(err))
}

func TestChangePassword_RevokesAllSessions(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	passwordHash, err := userSvc.hashPassword("old-password")
	require.NoError(t, err)
	user := &models.User{
		BaseModel:    models.BaseModel{ID: "u-password"},
		Username:     "password-user",
		PasswordHash: passwordHash,
		Roles:        models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	sessionA, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))
	sessionB, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))

	require.NoError(t, s.ChangePassword(context.Background(), user.ID, "old-password", "new-password", ""))

	sessionA, err = s.sessionService.GetSessionByID(context.Background(), sessionA.ID)
	require.NoError(t, err)
	sessionB, err = s.sessionService.GetSessionByID(context.Background(), sessionB.ID)
	require.NoError(t, err)
	require.NotNil(t, sessionA.RevokedAt)
	require.NotNil(t, sessionB.RevokedAt)
}

func TestChangePassword_KeepsCurrentSessionAlive(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	s := newTestAuthService("")
	s.userService = userSvc
	s.sessionService = NewSessionService(db)

	passwordHash, err := userSvc.hashPassword("old-password")
	require.NoError(t, err)
	user := &models.User{
		BaseModel:    models.BaseModel{ID: "u-keep"},
		Username:     "keep-user",
		PasswordHash: passwordHash,
		Roles:        models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	current, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))
	other, _ := createTestSession(t, db, user.ID, time.Now().Add(time.Hour))

	require.NoError(t, s.ChangePassword(context.Background(), user.ID, "old-password", "new-password", current.ID))

	current, err = s.sessionService.GetSessionByID(context.Background(), current.ID)
	require.NoError(t, err)
	other, err = s.sessionService.GetSessionByID(context.Background(), other.ID)
	require.NoError(t, err)
	require.Nil(t, current.RevokedAt, "current session should remain active")
	require.NotNil(t, other.RevokedAt, "other sessions should be revoked")
}

func TestRefreshToken_RejectsNonHMACAlg(t *testing.T) {
	s := newTestAuthService("")
	exp := time.Now().Add(5 * time.Minute)
	token := makeUnsignedToken(t, jwt.RegisteredClaims{
		ID:        "u1",
		Subject:   "refresh",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(exp),
	})

	_, err := s.RefreshToken(context.Background(), token, auth.SessionMeta{})
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken, got %v", err)
	}
}

func TestGetOidcConfigurationStatus(t *testing.T) {
	// Disabled
	s := newTestAuthService("")
	s.config = &config.Config{}
	// Set a non-nil settingsService to prevent nil pointer dereference
	// GetSettings will fail gracefully and mergeAccounts will default to false
	s.settingsService = &SettingsService{}

	status, err := s.GetOidcConfigurationStatus(context.Background())
	if err != nil {
		t.Fatalf("GetOidcConfigurationStatus error: %v", err)
	}
	if status.EnvForced || status.EnvConfigured {
		t.Errorf("expected disabled, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)
	}
	// MergeAccounts will be false since GetSettings will fail
	if status.MergeAccounts {
		t.Errorf("expected mergeAccounts=false, got true")
	}

	// Explicit env override to false should still be treated as forced
	t.Setenv("OIDC_ENABLED", "false")
	s.config.OidcEnabled = false
	status, err = s.GetOidcConfigurationStatus(context.Background())
	if err != nil {
		t.Fatalf("GetOidcConfigurationStatus error: %v", err)
	}
	if !status.EnvForced || status.EnvConfigured {
		t.Errorf("expected forced=false-override and not configured, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)
	}

	// Enabled but missing fields
	t.Setenv("OIDC_ENABLED", "true")
	s.config.OidcEnabled = true
	status, err = s.GetOidcConfigurationStatus(context.Background())
	if err != nil {
		t.Fatalf("GetOidcConfigurationStatus error: %v", err)
	}
	if !status.EnvForced || status.EnvConfigured {
		t.Errorf("expected enabled but not configured, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)
	}

	// Enabled and configured
	s.config.OidcClientID = "client-id"
	s.config.OidcIssuerURL = "https://example.com"
	status, err = s.GetOidcConfigurationStatus(context.Background())
	if err != nil {
		t.Fatalf("GetOidcConfigurationStatus error: %v", err)
	}
	if !status.EnvForced || !status.EnvConfigured {
		t.Errorf("expected enabled and configured, got forced=%v configured=%v", status.EnvForced, status.EnvConfigured)
	}
}

func TestFindOrCreateOidcUser_MergeEnabled_EmailNotVerified_NoExistingUser_CreatesNewUser(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := NewUserService(db)
	authSvc := newTestAuthService("")
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

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := NewUserService(db)
	// Seed an existing local user with matching email
	email := "existing@example.com"
	existing := &models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "existing",
		Email:     &email,
		Roles:     models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(ctx, existing)
	require.NoError(t, err)

	authSvc := newTestAuthService("")
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

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "oidcMergeAccounts", true))

	userSvc := NewUserService(db)
	// Seed an existing local user with matching email
	email := "existing@example.com"
	existing := &models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "existing",
		Email:     &email,
		Roles:     models.StringSlice{"user"},
	}
	_, err = userSvc.CreateUser(ctx, existing)
	require.NoError(t, err)

	authSvc := newTestAuthService("")
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
