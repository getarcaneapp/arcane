package services

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/net/http2"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/backend/internal/utils/stringutils"
	"github.com/getarcaneapp/arcane/types/auth"
)

type OidcService struct {
	authService        *AuthService
	config             *config.Config
	httpClient         *http.Client
	insecureHttpClient *http.Client
	providerCache      *oidc.Provider
	providerMutex      sync.RWMutex
	cachedIssuer       string
	cachedSkipTls      bool
	sfGroup            singleflight.Group
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

func (s *OidcService) getHttpClient(skipTlsVerify bool) *http.Client {
	if skipTlsVerify {
		return s.getInsecureHttpClient()
	}
	return s.httpClient
}

func (s *OidcService) getInsecureHttpClient() *http.Client {
	s.providerMutex.RLock()
	if s.insecureHttpClient != nil {
		s.providerMutex.RUnlock()
		return s.insecureHttpClient
	}
	s.providerMutex.RUnlock()

	s.providerMutex.Lock()
	defer s.providerMutex.Unlock()

	if s.insecureHttpClient != nil {
		return s.insecureHttpClient
	}

	// Create insecure client
	insecureClient := *s.httpClient

	var insecureTransport *http.Transport
	if transport, ok := insecureClient.Transport.(*http.Transport); ok {
		insecureTransport = transport.Clone()
	} else {
		// Transport is nil or not *http.Transport - create a new default transport
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			insecureTransport = defaultTransport.Clone()
		} else {
			insecureTransport = &http.Transport{}
		}
	}

	if insecureTransport.TLSClientConfig == nil {
		// #nosec G402 - This is explicitly an insecure client for OIDC discovery when TLS verification is skipped
		insecureTransport.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		}
	} else {
		insecureTransport.TLSClientConfig.InsecureSkipVerify = true
	}
	// Force HTTP/2 even with custom TLS config to avoid "malformed HTTP response" errors
	// when the server speaks HTTP/2 but the client disabled it due to custom TLS config.
	if err := http2.ConfigureTransport(insecureTransport); err != nil {
		slog.Warn("getInsecureHttpClient: failed to configure http2 transport", "error", err)
	}
	insecureClient.Transport = insecureTransport
	s.insecureHttpClient = &insecureClient
	return s.insecureHttpClient
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

func (s *OidcService) hasManualEndpoints(cfg *models.OidcConfig) bool {
	return cfg.AuthorizationEndpoint != "" || cfg.TokenEndpoint != "" || cfg.UserinfoEndpoint != ""
}

func (s *OidcService) getOauth2Config(cfg *models.OidcConfig, provider *oidc.Provider, origin string) (oauth2.Config, error) {
	scopes := strings.Fields(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"email", "profile"}
	}
	scopes = s.ensureOpenIDScope(scopes)

	var endpoint oauth2.Endpoint
	if provider != nil {
		endpoint = provider.Endpoint()
	} else {
		endpoint = oauth2.Endpoint{
			AuthURL:  cfg.AuthorizationEndpoint,
			TokenURL: cfg.TokenEndpoint,
		}
	}
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" {
		return oauth2.Config{}, errors.New("authorization and token endpoints must be configured")
	}

	return oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  s.GetOidcRedirectURL(origin),
		Scopes:       scopes,
	}, nil
}

func (s *OidcService) GenerateAuthURL(ctx context.Context, redirectTo string, origin string) (string, string, error) {
	config, err := s.getEffectiveConfig(ctx)
	if err != nil {
		slog.Error("GenerateAuthURL: failed to get OIDC config", "error", err)
		return "", "", err
	}

	var provider *oidc.Provider
	if !s.hasManualEndpoints(config) {
		var err error
		provider, err = s.getOrDiscoverProvider(ctx, config)
		if err != nil {
			slog.Error("GenerateAuthURL: provider discovery failed", "issuer", config.IssuerURL, "error", err)
			return "", "", fmt.Errorf("failed to discover provider: %w", err)
		}
	}

	state := stringutils.GenerateRandomString(32)
	nonce := stringutils.GenerateRandomString(32)
	codeVerifier := stringutils.GenerateRandomString(128)

	oauth2Config, err := s.getOauth2Config(config, provider, origin)
	if err != nil {
		slog.Error("GenerateAuthURL: invalid OIDC endpoints", "error", err)
		return "", "", err
	}

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
		baseUrl = strings.TrimSuffix(s.config.GetAppURL(), "/")
	}
	return baseUrl + "/auth/oidc/callback"
}

func (s *OidcService) getOrDiscoverProvider(ctx context.Context, cfg *models.OidcConfig) (*oidc.Provider, error) {
	issuer := cfg.IssuerURL
	skipTls := cfg.SkipTlsVerify

	s.providerMutex.RLock()
	if s.providerCache != nil && s.cachedIssuer == issuer && s.cachedSkipTls == skipTls {
		provider := s.providerCache
		s.providerMutex.RUnlock()
		return provider, nil
	}
	s.providerMutex.RUnlock()

	// Use singleflight to prevent thundering herd. Include skipTls in key to handle toggling.
	sfKey := fmt.Sprintf("%s|%v", issuer, skipTls)
	v, err, _ := s.sfGroup.Do(sfKey, func() (interface{}, error) {
		// Double check inside the lock/singleflight
		s.providerMutex.RLock()
		if s.providerCache != nil && s.cachedIssuer == issuer && s.cachedSkipTls == skipTls {
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
		providerCtx := oidc.ClientContext(discoveryCtx, s.getHttpClient(skipTls))
		provider, discoveredIssuer, err := s.discoverProvider(providerCtx, issuer)
		if err != nil {
			slog.ErrorContext(ctx, "getOrDiscoverProvider: discovery failed", "issuer", issuer, "skipTls", skipTls, "error", err)
			return nil, fmt.Errorf("failed to discover provider at %s: %w", issuer, err)
		}

		s.providerMutex.Lock()
		s.providerCache = provider
		// Cache based on the configured issuer to avoid repeated discovery on
		// trailing-slash-only mismatches.
		s.cachedIssuer = issuer
		s.cachedSkipTls = skipTls
		s.providerMutex.Unlock()

		slog.DebugContext(ctx, "getOrDiscoverProvider: provider cached", "issuer", issuer, "effectiveIssuer", discoveredIssuer, "skipTls", skipTls)
		return provider, nil
	})

	if err != nil {
		return nil, err
	}

	return v.(*oidc.Provider), nil
}

func (s *OidcService) discoverProvider(ctx context.Context, issuer string) (*oidc.Provider, string, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err == nil {
		return provider, issuer, nil
	}

	altIssuer := strings.TrimRight(issuer, "/")
	if altIssuer == issuer {
		altIssuer = issuer + "/"
	}

	slog.WarnContext(ctx, "getOrDiscoverProvider: retrying discovery with alternate issuer", "configured", issuer, "alternate", altIssuer)
	provider, altErr := oidc.NewProvider(ctx, altIssuer)
	if altErr == nil {
		return provider, altIssuer, nil
	}

	slog.ErrorContext(ctx, "getOrDiscoverProvider: discovery failed with alternate issuer", "issuer", altIssuer, "error", altErr)
	return nil, issuer, err
}

func (s *OidcService) exchangeToken(ctx context.Context, cfg *models.OidcConfig, provider *oidc.Provider, code string, verifier string, origin string) (*oauth2.Token, error) {
	oauth2Config, err := s.getOauth2Config(cfg, provider, origin)
	if err != nil {
		return nil, err
	}

	providerCtx := oidc.ClientContext(ctx, s.getHttpClient(cfg.SkipTlsVerify))
	token, err := oauth2Config.Exchange(providerCtx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		slog.Error("exchangeToken: token exchange failed", "token_endpoint", oauth2Config.Endpoint.TokenURL, "error", err)
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	slog.Debug("exchangeToken: token exchange successful", "has_access_token", token.AccessToken != "", "has_refresh_token", token.RefreshToken != "")
	return token, nil
}

func (s *OidcService) fetchClaims(ctx context.Context, cfg *models.OidcConfig, provider *oidc.Provider, token *oauth2.Token, idToken *oidc.IDToken) (map[string]any, error) {
	providerCtx := oidc.ClientContext(ctx, s.getHttpClient(cfg.SkipTlsVerify))
	var claims map[string]any

	if idToken != nil {
		if err := idToken.Claims(&claims); err != nil {
			slog.Warn("fetchClaims: failed to extract claims from ID token", "error", err)
		} else {
			slog.Debug("fetchClaims: extracted claims from ID token")
		}
	}

	var userInfoClaims map[string]any
	switch {
	case provider != nil:
		userInfo, err := provider.UserInfo(providerCtx, oauth2.StaticTokenSource(token))
		if err != nil {
			slog.Debug("fetchClaims: userinfo endpoint call failed", "error", err)
			if claims != nil {
				return claims, nil
			}
			return nil, fmt.Errorf("failed to fetch userinfo: %w", err)
		}
		if err := userInfo.Claims(&userInfoClaims); err != nil {
			slog.Warn("fetchClaims: failed to decode userinfo claims", "error", err)
			if claims != nil {
				return claims, nil
			}
			return nil, fmt.Errorf("failed to decode userinfo claims: %w", err)
		}
		slog.Debug("fetchClaims: fetched userinfo claims successfully")
	case cfg.UserinfoEndpoint != "":
		manualClaims, err := s.fetchUserInfoClaims(providerCtx, cfg, token)
		if err != nil {
			slog.Debug("fetchClaims: userinfo endpoint call failed", "error", err)
			if claims != nil {
				return claims, nil
			}
			return nil, fmt.Errorf("failed to fetch userinfo: %w", err)
		}
		userInfoClaims = manualClaims
		slog.Debug("fetchClaims: fetched userinfo claims successfully")
	case claims != nil:
		return claims, nil
	default:
		return nil, errors.New("userinfo endpoint not configured")
	}

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

func (s *OidcService) fetchUserInfoClaims(ctx context.Context, cfg *models.OidcConfig, token *oauth2.Token) (map[string]any, error) {
	if cfg.UserinfoEndpoint == "" {
		return nil, errors.New("userinfo endpoint not configured")
	}
	if token == nil || token.AccessToken == "" {
		return nil, errors.New("missing access token for userinfo request")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}

	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	request.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, token.AccessToken))
	request.Header.Set("Accept", "application/json")

	client := s.getHttpClient(cfg.SkipTlsVerify)
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("userinfo endpoint returned status %d", resp.StatusCode)
	}

	var claims map[string]any
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&claims); err != nil {
		return nil, err
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

	var provider *oidc.Provider
	if !s.hasManualEndpoints(cfg) {
		provider, err = s.getOrDiscoverProvider(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
	}

	token, err := s.exchangeToken(ctx, cfg, provider, code, stateData.CodeVerifier, origin)
	if err != nil {
		return nil, nil, err
	}

	idToken, rawIDToken, err := s.verifyIDToken(ctx, provider, cfg, token, stateData.Nonce)
	if err != nil {
		return nil, nil, err
	}

	return s.buildUserInfo(ctx, provider, cfg, token, idToken, rawIDToken)
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

	providerCtx := oidc.ClientContext(ctx, s.getHttpClient(cfg.SkipTlsVerify))
	var verifier *oidc.IDTokenVerifier
	if provider != nil {
		verifier = provider.Verifier(verifierConfig)
	} else {
		if cfg.JwksURI == "" {
			return nil, "", errors.New("jwks URI must be configured when using manual OIDC endpoints")
		}
		keySet := oidc.NewRemoteKeySet(providerCtx, cfg.JwksURI)
		verifier = oidc.NewVerifier(cfg.IssuerURL, keySet, verifierConfig)
	}

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

func (s *OidcService) buildUserInfo(ctx context.Context, provider *oidc.Provider, cfg *models.OidcConfig, token *oauth2.Token, idToken *oidc.IDToken, rawIDToken string) (*auth.OidcUserInfo, *auth.OidcTokenResponse, error) {
	claims, err := s.fetchClaims(ctx, cfg, provider, token, idToken)
	if err != nil {
		slog.Error("HandleCallback: failed to fetch claims", "error", err)
		return nil, nil, fmt.Errorf("failed to fetch user claims: %w", err)
	}

	subject := crypto.GetStringClaim(claims, "sub")
	if subject == "" {
		slog.Error("HandleCallback: missing required 'sub' claim")
		return nil, nil, errors.New("missing required 'sub' claim in user info")
	}

	userInfoDto := auth.OidcUserInfo{
		Subject:           subject,
		Name:              crypto.GetStringClaim(claims, "name"),
		Email:             crypto.GetStringClaim(claims, "email"),
		EmailVerified:     crypto.GetBoolClaim(claims, "email_verified"),
		PreferredUsername: crypto.GetStringClaim(claims, "preferred_username"),
		GivenName:         crypto.GetStringClaim(claims, "given_name"),
		FamilyName:        crypto.GetStringClaim(claims, "family_name"),
		Admin:             crypto.GetBoolClaim(claims, "admin"),
		Roles:             crypto.GetStringSliceClaim(claims, "roles"),
		Groups:            crypto.GetStringSliceClaim(claims, "groups"),
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

// InitiateDeviceAuth initiates the OIDC device authorization flow.
func (s *OidcService) InitiateDeviceAuth(ctx context.Context) (*auth.OidcDeviceAuthResponse, error) {
	cfg, err := s.getEffectiveConfig(ctx)
	if err != nil {
		slog.Error("InitiateDeviceAuth: failed to get OIDC config", "error", err)
		return nil, err
	}

	deviceEndpoint, err := s.getDeviceAuthorizationEndpoint(ctx, cfg)
	if err != nil {
		slog.Error("InitiateDeviceAuth: failed to get device endpoint", "error", err)
		return nil, err
	}

	scopes := strings.Fields(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"email", "profile"}
	}
	scopes = s.ensureOpenIDScope(scopes)

	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("scope", strings.Join(scopes, " "))
	if cfg.ClientSecret != "" {
		values.Set("client_secret", cfg.ClientSecret)
	}

	respData, err := s.makeDeviceAuthRequest(ctx, deviceEndpoint, values, cfg.SkipTlsVerify)
	if err != nil {
		return nil, err
	}

	deviceCode, ok := respData["device_code"].(string)
	if !ok || deviceCode == "" {
		return nil, errors.New("invalid device_code in response")
	}
	userCode, ok := respData["user_code"].(string)
	if !ok || userCode == "" {
		return nil, errors.New("invalid user_code in response")
	}
	verificationUri, ok := respData["verification_uri"].(string)
	if !ok || verificationUri == "" {
		return nil, errors.New("invalid verification_uri in response")
	}
	expiresIn, ok := respData["expires_in"].(float64)
	if !ok {
		return nil, errors.New("invalid expires_in in response")
	}

	response := &auth.OidcDeviceAuthResponse{
		DeviceCode:      deviceCode,
		UserCode:        userCode,
		VerificationUri: verificationUri,
		ExpiresIn:       int(expiresIn),
	}

	if uri, ok := respData["verification_uri_complete"].(string); ok {
		response.VerificationUriComplete = uri
	}
	if interval, ok := respData["interval"].(float64); ok {
		response.Interval = int(interval)
	} else {
		response.Interval = 5
	}

	slog.Debug("InitiateDeviceAuth: device authorization initiated", "user_code", response.UserCode, "expires_in", response.ExpiresIn)
	return response, nil
}

// getDeviceAuthorizationEndpoint discovers or returns the configured device authorization endpoint.
func (s *OidcService) getDeviceAuthorizationEndpoint(ctx context.Context, cfg *models.OidcConfig) (string, error) {
	if cfg.DeviceAuthorizationEndpoint != "" {
		return cfg.DeviceAuthorizationEndpoint, nil
	}

	provider, err := s.getOrDiscoverProvider(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to discover provider: %w", err)
	}

	var claims struct {
		DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	}
	if err := provider.Claims(&claims); err != nil {
		return "", fmt.Errorf("failed to get device authorization endpoint from provider: %w", err)
	}

	if claims.DeviceAuthorizationEndpoint == "" {
		return "", errors.New("device authorization endpoint not found in provider configuration")
	}

	return claims.DeviceAuthorizationEndpoint, nil
}

// makeDeviceAuthRequest makes a device authorization request.
func (s *OidcService) makeDeviceAuthRequest(ctx context.Context, endpoint string, params url.Values, skipTls bool) (map[string]any, error) {
	body := params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := s.getHttpClient(skipTls)
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("makeDeviceAuthRequest: request failed", "error", err)
		return nil, fmt.Errorf("device authorization request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			if errMsg, ok := errorResp["error"].(string); ok {
				return nil, fmt.Errorf("device authorization failed: %s", errMsg)
			}
		}
		return nil, fmt.Errorf("device authorization endpoint returned status %d", resp.StatusCode)
	}

	var respData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, fmt.Errorf("failed to decode device authorization response: %w", err)
	}

	return respData, nil
}

// ExchangeDeviceToken exchanges a device code for tokens.
func (s *OidcService) ExchangeDeviceToken(ctx context.Context, deviceCode string) (*auth.OidcUserInfo, *auth.OidcTokenResponse, error) {
	cfg, err := s.getEffectiveConfig(ctx)
	if err != nil {
		slog.Error("ExchangeDeviceToken: failed to get OIDC config", "error", err)
		return nil, nil, err
	}

	var tokenEndpoint string
	if cfg.TokenEndpoint != "" {
		tokenEndpoint = cfg.TokenEndpoint
	} else {
		provider, err := s.getOrDiscoverProvider(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		tokenEndpoint = provider.Endpoint().TokenURL
	}

	params := map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
		"client_id":   cfg.ClientID,
	}
	if cfg.ClientSecret != "" {
		params["client_secret"] = cfg.ClientSecret
	}

	tokenResp, err := s.makeTokenRequest(ctx, tokenEndpoint, params, cfg.SkipTlsVerify)
	if err != nil {
		return nil, nil, err
	}

	var provider *oidc.Provider
	if !s.hasManualEndpoints(cfg) {
		provider, err = s.getOrDiscoverProvider(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
	}

	accessToken, ok := tokenResp["access_token"].(string)
	if !ok || accessToken == "" {
		return nil, nil, errors.New("invalid access_token in response")
	}

	token := &oauth2.Token{
		AccessToken: accessToken,
		TokenType:   stringutils.GetStringOrDefault(tokenResp, "token_type", "Bearer"),
	}
	if refreshToken, ok := tokenResp["refresh_token"].(string); ok {
		token.RefreshToken = refreshToken
	}
	if expiresIn, ok := tokenResp["expires_in"].(float64); ok {
		token.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	if idToken, ok := tokenResp["id_token"].(string); ok {
		token = token.WithExtra(map[string]any{"id_token": idToken})
	}

	idToken, rawIDToken, err := s.verifyIDToken(ctx, provider, cfg, token, "")
	if err != nil {
		return nil, nil, err
	}

	return s.buildUserInfo(ctx, provider, cfg, token, idToken, rawIDToken)
}

// makeTokenRequest makes a token exchange request.
func (s *OidcService) makeTokenRequest(ctx context.Context, endpoint string, params map[string]string, skipTls bool) (map[string]any, error) {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	body := values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := s.getHttpClient(skipTls)
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("makeTokenRequest: request failed", "error", err)
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if errMsg, ok := tokenResp["error"].(string); ok {
			switch errMsg {
			case "authorization_pending":
				return nil, errors.New("authorization_pending")
			case "slow_down":
				return nil, errors.New("slow_down")
			case "expired_token":
				return nil, errors.New("expired_token")
			case "access_denied":
				return nil, errors.New("access_denied")
			default:
				return nil, fmt.Errorf("token exchange failed: %s", errMsg)
			}
		}
		return nil, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	return tokenResp, nil
}
