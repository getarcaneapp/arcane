package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"go.getarcane.app/types/auth"
)

type OidcService struct {
	authService   *AuthService
	config        *config.Config
	httpClient    *http.Client
	providerCache *oidc.Provider
	providerMutex sync.RWMutex
	cachedIssuer  string
	sfGroup       singleflight.Group
}

type OidcState struct {
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"code_verifier"`
	RedirectTo   string    `json:"redirect_to"`
	CreatedAt    time.Time `json:"created_at"`
}

func NewOidcService(authService *AuthService, cfg *config.Config, httpClient *http.Client) *OidcService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// Create a copy of the client to avoid modifying the shared one
	// and remove the timeout so we can control it via context
	oidcClient := *httpClient
	oidcClient.Timeout = 0

	return &OidcService{
		authService: authService,
		config:      cfg,
		httpClient:  &oidcClient,
	}
}

func (s *OidcService) getEffectiveConfig(ctx context.Context) (*models.OidcConfig, error) {
	config, err := s.authService.GetOidcConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get OIDC config: %w", err)
	}
	if config.IssuerURL == "" {
		return nil, errors.New("issuer URL must be configured")
	}
	return config, nil
}

func (s *OidcService) ensureOpenIDScope(scopes []string) []string {
	hasOpenID := false
	for _, scope := range scopes {
		if scope == oidc.ScopeOpenID {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return scopes
}

func (s *OidcService) getOauth2Config(cfg *models.OidcConfig, provider *oidc.Provider, origin string) oauth2.Config {
	scopes := strings.Fields(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"email", "profile"}
	}
	scopes = s.ensureOpenIDScope(scopes)

	return oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.GetOidcRedirectURL(origin),
		Scopes:       scopes,
	}
}

func (s *OidcService) GenerateAuthURL(ctx context.Context, redirectTo string, origin string) (string, string, error) {
	config, err := s.getEffectiveConfig(ctx)
	if err != nil {
		slog.Error("GenerateAuthURL: failed to get OIDC config", "error", err)
		return "", "", err
	}

	provider, err := s.getOrDiscoverProvider(ctx, config.IssuerURL)
	if err != nil {
		slog.Error("GenerateAuthURL: provider discovery failed", "issuer", config.IssuerURL, "error", err)
		return "", "", fmt.Errorf("failed to discover provider: %w", err)
	}

	state := utils.GenerateRandomString(32)
	nonce := utils.GenerateRandomString(32)
	codeVerifier := utils.GenerateRandomString(128)

	oauth2Config := s.getOauth2Config(config, provider, origin)

	authURL := oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	)

	stateData := OidcState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		RedirectTo:   redirectTo,
		CreatedAt:    time.Now(),
	}

	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		slog.Error("GenerateAuthURL: failed to marshal state", "error", err)
		return "", "", fmt.Errorf("failed to encode state: %w", err)
	}
	encodedState := base64.URLEncoding.EncodeToString(stateJSON)

	slog.Debug("GenerateAuthURL: generated authorization URL", "issuer", config.IssuerURL, "scopes", oauth2Config.Scopes)
	return authURL, encodedState, nil
}

func (s *OidcService) GetOidcRedirectURL(origin string) string {
	baseUrl := origin
	if baseUrl == "" {
		baseUrl = strings.TrimSuffix(s.config.AppUrl, "/")
	}
	return baseUrl + "/auth/oidc/callback"
}

func (s *OidcService) getOrDiscoverProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.providerMutex.RLock()
	if s.providerCache != nil && s.cachedIssuer == issuer {
		provider := s.providerCache
		s.providerMutex.RUnlock()
		return provider, nil
	}
	s.providerMutex.RUnlock()

	// Use singleflight to prevent thundering herd
	v, err, _ := s.sfGroup.Do(issuer, func() (interface{}, error) {
		// Double check inside the lock/singleflight
		s.providerMutex.RLock()
		if s.providerCache != nil && s.cachedIssuer == issuer {
			provider := s.providerCache
			s.providerMutex.RUnlock()
			return provider, nil
		}
		s.providerMutex.RUnlock()

		// Create a context with a longer timeout for discovery
		// We use context.WithoutCancel(ctx) as the parent because we don't want the discovery
		// to be cancelled if the incoming request context is cancelled (e.g. client disconnect).
		// This ensures the provider is cached for subsequent requests.
		discoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()

		// Use the custom HTTP client with the new context
		providerCtx := oidc.ClientContext(discoveryCtx, s.httpClient)
		provider, err := oidc.NewProvider(providerCtx, issuer)
		if err != nil {
			slog.ErrorContext(ctx, "getOrDiscoverProvider: discovery failed", "issuer", issuer, "error", err)
			return nil, fmt.Errorf("failed to discover provider at %s: %w", issuer, err)
		}

		s.providerMutex.Lock()
		s.providerCache = provider
		s.cachedIssuer = issuer
		s.providerMutex.Unlock()

		slog.DebugContext(ctx, "getOrDiscoverProvider: provider cached", "issuer", issuer)
		return provider, nil
	})

	if err != nil {
		return nil, err
	}

	return v.(*oidc.Provider), nil
}

func (s *OidcService) exchangeToken(ctx context.Context, cfg *models.OidcConfig, provider *oidc.Provider, code string, verifier string, origin string) (*oauth2.Token, error) {
	oauth2Config := s.getOauth2Config(cfg, provider, origin)

	providerCtx := oidc.ClientContext(ctx, s.httpClient)
	token, err := oauth2Config.Exchange(providerCtx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		slog.Error("exchangeToken: token exchange failed", "token_endpoint", oauth2Config.Endpoint.TokenURL, "error", err)
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	slog.Debug("exchangeToken: token exchange successful", "has_access_token", token.AccessToken != "", "has_refresh_token", token.RefreshToken != "")
	return token, nil
}

func (s *OidcService) fetchClaims(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, idToken *oidc.IDToken) (map[string]any, error) {
	providerCtx := oidc.ClientContext(ctx, s.httpClient)
	var claims map[string]any

	if idToken != nil {
		if err := idToken.Claims(&claims); err != nil {
			slog.Warn("fetchClaims: failed to extract claims from ID token", "error", err)
		} else {
			slog.Debug("fetchClaims: extracted claims from ID token")
		}
	}

	userInfo, err := provider.UserInfo(providerCtx, oauth2.StaticTokenSource(token))
	if err != nil {
		slog.Debug("fetchClaims: userinfo endpoint call failed", "error", err)
		if claims != nil {
			return claims, nil
		}
		return nil, fmt.Errorf("failed to fetch userinfo: %w", err)
	}

	var userInfoClaims map[string]any
	if err := userInfo.Claims(&userInfoClaims); err != nil {
		slog.Warn("fetchClaims: failed to decode userinfo claims", "error", err)
		if claims != nil {
			return claims, nil
		}
		return nil, fmt.Errorf("failed to decode userinfo claims: %w", err)
	}

	slog.Debug("fetchClaims: fetched userinfo claims successfully")

	if claims == nil {
		claims = make(map[string]any)
	}
	for k, v := range userInfoClaims {
		if _, exists := claims[k]; !exists {
			claims[k] = v
		}
	}

	return claims, nil
}

func (s *OidcService) HandleCallback(ctx context.Context, code, state, storedState, origin string) (*auth.OidcUserInfo, *auth.OidcTokenResponse, error) {
	slog.Debug("HandleCallback: processing callback", "code_present", code != "", "state_present", state != "")

	stateData, err := s.validateState(state, storedState)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := s.getEffectiveConfig(ctx)
	if err != nil {
		slog.Error("HandleCallback: failed to get OIDC config", "error", err)
		return nil, nil, err
	}

	provider, err := s.getOrDiscoverProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, nil, err
	}

	token, err := s.exchangeToken(ctx, cfg, provider, code, stateData.CodeVerifier, origin)
	if err != nil {
		return nil, nil, err
	}

	idToken, rawIDToken, err := s.verifyIDToken(ctx, provider, cfg, token, stateData.Nonce)
	if err != nil {
		return nil, nil, err
	}

	return s.buildUserInfo(ctx, provider, token, idToken, rawIDToken)
}

func (s *OidcService) validateState(state, storedState string) (*OidcState, error) {
	stateData, err := s.decodeState(storedState)
	if err != nil {
		slog.Error("HandleCallback: failed to decode stored state", "error", err)
		return nil, fmt.Errorf("invalid state parameter: %w", err)
	}

	if state != stateData.State {
		slog.Error("HandleCallback: state mismatch", "received_len", len(state), "expected_len", len(stateData.State))
		return nil, errors.New("state parameter mismatch")
	}

	if time.Since(stateData.CreatedAt) > 10*time.Minute {
		slog.Error("HandleCallback: state expired", "age", time.Since(stateData.CreatedAt))
		return nil, errors.New("authentication state has expired")
	}
	return stateData, nil
}

func (s *OidcService) verifyIDToken(ctx context.Context, provider *oidc.Provider, cfg *models.OidcConfig, token *oauth2.Token, nonce string) (*oidc.IDToken, string, error) {
	var rawIDToken string
	if idTokenValue := token.Extra("id_token"); idTokenValue != nil {
		if idTokenStr, ok := idTokenValue.(string); ok {
			rawIDToken = idTokenStr
		}
	}

	if rawIDToken == "" {
		slog.Warn("HandleCallback: no ID token in response (non-compliant OIDC response)")
		return nil, "", nil
	}

	verifierConfig := &oidc.Config{
		ClientID: cfg.ClientID,
	}

	if nonce != "" {
		verifierConfig.Now = time.Now
	}

	verifier := provider.Verifier(verifierConfig)
	providerCtx := oidc.ClientContext(ctx, s.httpClient)

	idToken, err := verifier.Verify(providerCtx, rawIDToken)
	if err != nil {
		slog.Error("HandleCallback: ID token verification failed", "error", err)
		return nil, "", fmt.Errorf("failed to verify ID token: %w", err)
	}

	if nonce != "" {
		var claims struct {
			Nonce string `json:"nonce"`
		}
		if err := idToken.Claims(&claims); err != nil {
			slog.Error("HandleCallback: failed to extract nonce from ID token", "error", err)
			return nil, "", fmt.Errorf("failed to verify nonce: %w", err)
		}
		if claims.Nonce != nonce {
			slog.Error("HandleCallback: nonce mismatch", "expected", nonce, "got", claims.Nonce)
			return nil, "", errors.New("nonce verification failed")
		}
	}

	slog.Debug("HandleCallback: ID token verified successfully", "subject", idToken.Subject, "issuer", idToken.Issuer)
	return idToken, rawIDToken, nil
}

func (s *OidcService) buildUserInfo(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, idToken *oidc.IDToken, rawIDToken string) (*auth.OidcUserInfo, *auth.OidcTokenResponse, error) {
	claims, err := s.fetchClaims(ctx, provider, token, idToken)
	if err != nil {
		slog.Error("HandleCallback: failed to fetch claims", "error", err)
		return nil, nil, fmt.Errorf("failed to fetch user claims: %w", err)
	}

	subject := utils.GetStringClaim(claims, "sub")
	if subject == "" {
		slog.Error("HandleCallback: missing required 'sub' claim")
		return nil, nil, errors.New("missing required 'sub' claim in user info")
	}

	userInfoDto := auth.OidcUserInfo{
		Subject:           subject,
		Name:              utils.GetStringClaim(claims, "name"),
		Email:             utils.GetStringClaim(claims, "email"),
		EmailVerified:     utils.GetBoolClaim(claims, "email_verified"),
		PreferredUsername: utils.GetStringClaim(claims, "preferred_username"),
		GivenName:         utils.GetStringClaim(claims, "given_name"),
		FamilyName:        utils.GetStringClaim(claims, "family_name"),
		Admin:             utils.GetBoolClaim(claims, "admin"),
		Roles:             utils.GetStringSliceClaim(claims, "roles"),
		Groups:            utils.GetStringSliceClaim(claims, "groups"),
		Extra:             claims,
	}

	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	tokenResp := &auth.OidcTokenResponse{
		AccessToken:  token.AccessToken,
		TokenType:    tokenType,
		RefreshToken: token.RefreshToken,
		IDToken:      rawIDToken,
	}
	if !token.Expiry.IsZero() {
		expiresIn := int(time.Until(token.Expiry).Seconds())
		if expiresIn < 0 {
			expiresIn = 0
		}
		tokenResp.ExpiresIn = expiresIn
	}

	slog.Info("HandleCallback: authentication successful", "subject", userInfoDto.Subject, "email", userInfoDto.Email)
	return &userInfoDto, tokenResp, nil
}

func (s *OidcService) decodeState(encodedState string) (*OidcState, error) {
	stateJSON, err := base64.URLEncoding.DecodeString(encodedState)
	if err != nil {
		slog.Error("decodeState: failed to decode base64 state", "error", err)
		return nil, err
	}

	var stateData OidcState
	if err := json.Unmarshal(stateJSON, &stateData); err != nil {
		slog.Error("decodeState: failed to unmarshal state JSON", "error", err)
		return nil, err
	}

	return &stateData, nil
}
