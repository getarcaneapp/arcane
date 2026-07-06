package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
)

type federatedTestIssuerInternal struct {
	IssuerURL string
	private   *rsa.PrivateKey
	keyID     string
	server    *httptest.Server
}

func newFederatedTestIssuerInternal(t *testing.T) *federatedTestIssuerInternal {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	issuer := &federatedTestIssuerInternal{
		private: privateKey,
		keyID:   "federated-test-key",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer.IssuerURL,
			"jwks_uri":                              issuer.IssuerURL + "/jwks",
			"authorization_endpoint":                issuer.IssuerURL + "/authorize",
			"token_endpoint":                        issuer.IssuerURL + "/token",
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}))
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := privateKey.PublicKey
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"use": "sig",
					"kid": issuer.keyID,
					"alg": "RS256",
					"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
				},
			},
		}))
	})

	issuer.server = httptest.NewServer(mux)
	issuer.IssuerURL = issuer.server.URL
	t.Cleanup(issuer.server.Close)

	return issuer
}

func (i *federatedTestIssuerInternal) tokenInternal(t *testing.T, subject string, audience []string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": i.IssuerURL,
		"sub": subject,
		"aud": audience,
		"iat": now.Unix(),
		"nbf": now.Add(-time.Minute).Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = i.keyID
	signed, err := token.SignedString(i.private)
	require.NoError(t, err)
	return signed
}

func setupFederatedCredentialServiceTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.SettingVariable{},
		&models.User{},
		&models.UserSession{},
		&models.Role{},
		&models.UserRoleAssignment{},
		&models.FederatedCredential{},
		&models.FederatedTokenReplay{},
		&models.Event{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return &database.DB{DB: db}
}

func setupFederatedCredentialServiceInternal(t *testing.T, issuer *federatedTestIssuerInternal) (*FederatedCredentialService, *AuthService, *database.DB) {
	t.Helper()

	ctx := context.Background()
	db := setupFederatedCredentialServiceTestDBInternal(t)
	roleSvc := NewRoleService(db)
	userSvc := NewUserService(db).WithRoleService(roleSvc)
	sessionSvc := NewSessionService(db)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	eventSvc := NewEventService(db, &config.Config{}, nil)
	authSvc := NewAuthService(userSvc, settingsSvc, eventSvc, sessionSvc, roleSvc, "test-federated-secret", &config.Config{
		JWTRefreshExpiry: 24 * time.Hour,
	})

	service := NewFederatedCredentialService(db, authSvc, userSvc, settingsSvc, eventSvc, issuer.server.Client()).WithRoleService(roleSvc)

	role := models.Role{
		BaseModel:   models.BaseModel{ID: "role-federated-viewer"},
		Name:        "Federated Viewer",
		Permissions: models.StringSlice{authz.PermProjectsList},
	}
	require.NoError(t, db.WithContext(ctx).Create(&role).Error)

	serviceUser := models.User{
		BaseModel:        models.BaseModel{ID: "user-federated-service"},
		Username:         "svc-federated-demo",
		IsServiceAccount: true,
	}
	require.NoError(t, db.WithContext(ctx).Create(&serviceUser).Error)
	require.NoError(t, db.WithContext(ctx).Create(&models.UserRoleAssignment{
		UserID: serviceUser.ID,
		RoleID: role.ID,
	}).Error)

	credential := models.FederatedCredential{
		BaseModel:       models.BaseModel{ID: "cred-github-actions"},
		Name:            "GitHub Actions",
		Enabled:         true,
		IssuerURL:       issuer.IssuerURL,
		Audiences:       models.StringSlice{"arcane-ci"},
		SubjectClaim:    "sub",
		SubjectMatch:    "repo:getarcaneapp/arcane:*",
		MatchType:       federatedtypes.MatchTypeGlob,
		RoleID:          role.ID,
		IdentityUserID:  serviceUser.ID,
		TokenTTLSeconds: 900,
	}
	require.NoError(t, db.WithContext(ctx).Create(&credential).Error)

	return service, authSvc, db
}

func TestFederatedCredentialServiceExchangeToken(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, authSvc, db := setupFederatedCredentialServiceInternal(t, issuer)
	ctx := context.Background()

	tests := []struct {
		name      string
		token     string
		wantError func(error) bool
	}{
		{
			name:  "issues an Arcane bearer token for a matching issuer audience and subject",
			token: issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		},
		{
			name:  "rejects audience mismatch",
			token: issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"other-audience"}),
			wantError: func(err error) bool {
				return common.IsErrorFederatedCredentialInvalidGrant(err)
			},
		},
		{
			name:  "rejects subject mismatch",
			token: issuer.tokenInternal(t, "repo:other/repo:ref:refs/heads/main", []string{"arcane-ci"}),
			wantError: func(err error) bool {
				return common.IsErrorFederatedCredentialInvalidGrant(err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.ExchangeToken(ctx, federatedtypes.TokenExchangeRequest{
				GrantType:        federatedtypes.TokenExchangeGrantType,
				SubjectToken:     tt.token,
				SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
				Audience:         "https://arcane.example.com",
			})
			if tt.wantError != nil {
				require.Error(t, err)
				require.True(t, tt.wantError(err), "unexpected error: %v", err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "Bearer", resp.TokenType)
			require.Equal(t, federatedtypes.IssuedTokenTypeAccessToken, resp.IssuedTokenType)
			require.Positive(t, resp.ExpiresIn)
			require.NotEmpty(t, resp.AccessToken)

			user, sessionID, err := authSvc.VerifyToken(ctx, resp.AccessToken)
			require.NoError(t, err)
			require.Equal(t, "user-federated-service", user.ID)

			var session models.UserSession
			require.NoError(t, db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error)
			require.Equal(t, models.UserSessionSourceFederated, session.Source)
			require.NotNil(t, session.FederatedCredentialID)
			require.Equal(t, "cred-github-actions", *session.FederatedCredentialID)
		})
	}
}

func TestFederatedCredentialServiceExchangeTokenRejectsIssuerWithoutCredentialInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	otherIssuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)

	resp, err := service.ExchangeToken(context.Background(), federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     otherIssuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.Error(t, err)
	require.True(t, common.IsErrorFederatedCredentialInvalidGrant(err), "unexpected error: %v", err)
	require.Nil(t, resp)
}

func TestFederatedCredentialServiceExchangeTokenDoesNotRequireGlobalFeatureFlagInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)
	service.settingsService = nil

	resp, err := service.ExchangeToken(context.Background(), federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.AccessToken)
}

func TestFederatedCredentialServiceExchangeTokenRejectsExpiredCredentialInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, db := setupFederatedCredentialServiceInternal(t, issuer)
	expiredAt := time.Now().Add(-time.Minute)
	require.NoError(t, db.WithContext(context.Background()).
		Model(&models.FederatedCredential{}).
		Where("id = ?", "cred-github-actions").
		Update("expires_at", expiredAt).Error)

	resp, err := service.ExchangeToken(context.Background(), federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})

	require.Error(t, err)
	require.True(t, common.IsErrorFederatedCredentialInvalidGrant(err), "unexpected error: %v", err)
	require.Nil(t, resp)
}

func TestFederatedCredentialServiceUpdateDisableRevokesIssuedSessionsInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, authSvc, _ := setupFederatedCredentialServiceInternal(t, issuer)
	ctx := context.Background()

	resp, err := service.ExchangeToken(ctx, federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"}),
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = service.Update(ctx, "admin-user", "cred-github-actions", federatedtypes.UpdateFederatedCredential{
		Enabled: new(false),
	})
	require.NoError(t, err)

	_, _, err = authSvc.VerifyToken(ctx, resp.AccessToken)
	require.Error(t, err)
	require.True(t, common.IsSessionRevokedError(err), "unexpected error: %v", err)
}

func TestFederatedCredentialServiceRejectsReplayedSubjectTokenInternal(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)
	ctx := context.Background()
	subjectToken := issuer.tokenInternal(t, "repo:getarcaneapp/arcane:ref:refs/heads/main", []string{"arcane-ci"})
	req := federatedtypes.TokenExchangeRequest{
		GrantType:        federatedtypes.TokenExchangeGrantType,
		SubjectToken:     subjectToken,
		SubjectTokenType: federatedtypes.SubjectTokenTypeJWT,
		Audience:         "https://arcane.example.com",
	}

	first, err := service.ExchangeToken(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := service.ExchangeToken(ctx, req)
	require.Error(t, err)
	require.True(t, common.IsErrorFederatedCredentialInvalidGrant(err), "unexpected error: %v", err)
	require.Nil(t, second)
}

func TestFederatedCredentialServiceCreateRejectsBareWildcardGlob(t *testing.T) {
	issuer := newFederatedTestIssuerInternal(t)
	service, _, _ := setupFederatedCredentialServiceInternal(t, issuer)

	_, err := service.Create(context.Background(), "admin-user", federatedtypes.CreateFederatedCredential{
		Name:            "Unsafe wildcard",
		IssuerURL:       "https://token.actions.githubusercontent.com",
		Audiences:       []string{"arcane-ci"},
		SubjectMatch:    "*",
		MatchType:       federatedtypes.MatchTypeGlob,
		RoleID:          "role-federated-viewer",
		TokenTTLSeconds: 900,
	})

	require.Error(t, err)
	require.True(t, common.IsErrorFederatedCredentialInvalid(err), "unexpected error: %v", err)
}
