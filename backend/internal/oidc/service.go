package oidc

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	authtypes "github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/samber/hot"
	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/jwtclaims"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

type OidcService struct {
	authService        *auth.AuthService
	settingsService    *settings.SettingsService
	config             *config.Config
	httpClient         *http.Client
	insecureHttpClient *http.Client
	providerCache      *hot.HotCache[oidcProviderKey, *oidc.Provider]
	keySetManager      *oidcjwk.KeySetManager
}

type oidcProviderKey struct {
	issuer  string
	skipTLS bool
}

type OidcState struct {
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"code_verifier"`
	RedirectTo   string    `json:"redirect_to"`
	CreatedAt    time.Time `json:"created_at"`
}

func NewOidcService(authService *auth.AuthService, settingsService *settings.SettingsService, cfg *config.Config, httpClient *http.Client, keySetManager *oidcjwk.KeySetManager) *OidcService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// Copy the shared client and drop its timeout so requests are bounded by context.
	oidcClient := *httpClient
	oidcClient.Timeout = 0
	insecureClient := oidcClient

	// Derive the insecure transport from the caller's raw transport, before either client is instrumented.
	insecureTransport := &http.Transport{}
	if transport, ok := httpClient.Transport.(*http.Transport); ok {
		insecureTransport = transport.Clone()
	} else if defaultTransport, isTransport := http.DefaultTransport.(*http.Transport); isTransport {
		insecureTransport = defaultTransport.Clone()
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
	// Enable HTTP/2 even with a custom TLS configuration.
	if insecureTransport.Protocols == nil {
		insecureTransport.Protocols = new(http.Protocols)
		insecureTransport.Protocols.SetHTTP1(true)
	}
	insecureTransport.Protocols.SetHTTP2(true)

	oidcClient.Transport = otelhttp.NewTransport(httpClient.Transport)
	insecureClient.Transport = otelhttp.NewTransport(insecureTransport)

	return &OidcService{
		authService:        authService,
		settingsService:    settingsService,
		config:             cfg,
		httpClient:         &oidcClient,
		insecureHttpClient: &insecureClient,
		keySetManager:      keySetManager,
		providerCache:      hot.NewHotCache[oidcProviderKey, *oidc.Provider](hot.LRU, 4).Build(),
	}
}

func (s *OidcService) effectiveConfig(ctx context.Context) (*settings.OidcConfig, error) {
	oidcConfig, err := s.authService.GetOidcConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get OIDC config: %w", err)
	}
	if oidcConfig.IssuerURL == "" {
		return nil, errors.New("issuer URL must be configured")
	}
	return oidcConfig, nil
}

func (s *OidcService) httpClientFor(skipTLSVerify bool) *http.Client {
	return kit.Ternary(skipTLSVerify, s.insecureHttpClient, s.httpClient)
}

func hasManualEndpoints(cfg *settings.OidcConfig) bool {
	return cfg.AuthorizationEndpoint != "" || cfg.TokenEndpoint != "" || cfg.UserinfoEndpoint != ""
}

// requestedScopes returns the configured scopes (defaulting to email and profile), always including openid.
func requestedScopes(cfg *settings.OidcConfig) []string {
	scopes := strings.Fields(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"email", "profile"}
	}
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return scopes
}

func (s *OidcService) buildOAuth2Config(cfg *settings.OidcConfig, provider *oidc.Provider, origin, mobileRedirectURI string) (oauth2.Config, error) {
	endpoint := oauth2.Endpoint{AuthURL: cfg.AuthorizationEndpoint, TokenURL: cfg.TokenEndpoint}
	if provider != nil {
		endpoint = provider.Endpoint()
	}
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" {
		return oauth2.Config{}, errors.New("authorization and token endpoints must be configured")
	}

	redirectURL := mobileRedirectURI
	if redirectURL == "" {
		redirectURL = s.GetOidcRedirectURL(origin)
	}

	return oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       requestedScopes(cfg),
	}, nil
}

func configAttributes(cfg *settings.OidcConfig) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("arcane.oidc.issuer", cfg.IssuerURL),
		attribute.Bool("arcane.oidc.skip_tls_verify", cfg.SkipTlsVerify),
		attribute.Bool("arcane.oidc.manual_endpoints", hasManualEndpoints(cfg)),
	}
}

// GetMobileRedirectAllowlist returns the configured list of acceptable mobile OAuth redirect URIs.
func (s *OidcService) GetMobileRedirectAllowlist(ctx context.Context) []string {
	raw := ""
	if s.config != nil {
		raw = s.config.OidcMobileRedirectUris
	}
	if s.settingsService != nil {
		raw = s.settingsService.GetStringSetting(ctx, "oidcMobileRedirectUris", raw)
	}
	return kit.TrimNonEmpty(strings.Split(raw, ","))
}

// ValidateMobileRedirectURI requires an exact allowlist match; partial matches would allow open redirects.
func (s *OidcService) ValidateMobileRedirectURI(ctx context.Context, uri string) error {
	if uri == "" {
		return errors.New("mobile redirect URI is empty")
	}
	if slices.Contains(s.GetMobileRedirectAllowlist(ctx), uri) {
		return nil
	}
	return fmt.Errorf("mobile redirect URI %q is not in the configured allowlist", uri)
}

func (s *OidcService) GenerateAuthURL(ctx context.Context, redirectTo, origin, mobileRedirectURI string) (string, string, error) {
	oidcConfig, err := s.effectiveConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "GenerateAuthURL: failed to get OIDC config", "error", err)
		return "", "", err
	}

	var provider *oidc.Provider
	if !hasManualEndpoints(oidcConfig) {
		var discoverProviderErr error
		provider, discoverProviderErr = s.getOrDiscoverProvider(ctx, oidcConfig)
		if discoverProviderErr != nil {
			slog.ErrorContext(ctx, "GenerateAuthURL: provider discovery failed", "issuer", oidcConfig.IssuerURL, "error", discoverProviderErr)
			return "", "", fmt.Errorf("failed to discover provider: %w", discoverProviderErr)
		}
	}

	state := kit.RandomString(32)
	nonce := kit.RandomString(32)
	codeVerifier := kit.RandomString(128)

	oauth2Config, err := s.buildOAuth2Config(oidcConfig, provider, origin, mobileRedirectURI)
	if err != nil {
		slog.ErrorContext(ctx, "GenerateAuthURL: invalid OIDC endpoints", "error", err)
		return "", "", err
	}

	authURL := oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	)

	stateJSON, err := json.Marshal(OidcState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		RedirectTo:   redirectTo,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "GenerateAuthURL: failed to marshal state", "error", err)
		return "", "", fmt.Errorf("failed to encode state: %w", err)
	}

	slog.DebugContext(ctx, "GenerateAuthURL: generated authorization URL", "issuer", oidcConfig.IssuerURL, "scopes", oauth2Config.Scopes)
	return authURL, base64.URLEncoding.EncodeToString(stateJSON), nil
}

func (s *OidcService) GetOidcRedirectURL(origin string) string {
	baseUrl := origin
	if baseUrl == "" {
		baseUrl = strings.TrimSuffix(s.config.GetAppURL(), "/")
	}
	return baseUrl + "/auth/oidc/callback"
}

func (s *OidcService) getOrDiscoverProvider(ctx context.Context, cfg *settings.OidcConfig) (*oidc.Provider, error) {
	key := oidcProviderKey{issuer: cfg.IssuerURL, skipTLS: cfg.SkipTlsVerify}
	provider, found, err := s.providerCache.GetWithLoaders(key, func(keys []oidcProviderKey) (map[oidcProviderKey]*oidc.Provider, error) {
		providers := make(map[oidcProviderKey]*oidc.Provider, len(keys))
		for _, providerKey := range keys {
			discovered, discoverErr := s.discoverProvider(ctx, providerKey)
			if discoverErr != nil {
				slog.ErrorContext(ctx, "getOrDiscoverProvider: discovery failed", "issuer", providerKey.issuer, "skipTls", providerKey.skipTLS, "error", discoverErr)
				return nil, fmt.Errorf("failed to discover provider at %s: %w", providerKey.issuer, discoverErr)
			}
			providers[providerKey] = discovered
		}
		return providers, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("OIDC provider cache loader returned no provider")
	}
	return provider, nil
}

// discoverProvider fetches provider metadata, retrying once with the issuer's trailing slash toggled.
func (s *OidcService) discoverProvider(ctx context.Context, key oidcProviderKey) (_ *oidc.Provider, err error) {
	discoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	discoveryCtx, span := otel.Tracer(tracing.InstrumentationName).Start(discoveryCtx, "oidc.discover_provider", trace.WithAttributes(
		attribute.String("arcane.oidc.issuer", key.issuer),
		attribute.Bool("arcane.oidc.skip_tls_verify", key.skipTLS),
	))
	defer func() { tracing.End(span, err) }()
	providerCtx := oidc.ClientContext(discoveryCtx, s.httpClientFor(key.skipTLS))

	provider, err := oidc.NewProvider(providerCtx, key.issuer)
	if err == nil {
		slog.DebugContext(ctx, "getOrDiscoverProvider: provider cached", "issuer", key.issuer, "skipTls", key.skipTLS)
		return provider, nil
	}

	altIssuer := strings.TrimRight(key.issuer, "/")
	if altIssuer == key.issuer {
		altIssuer = key.issuer + "/"
	}
	slog.WarnContext(ctx, "getOrDiscoverProvider: retrying discovery with alternate issuer", "configured", key.issuer, "alternate", altIssuer)
	provider, altErr := oidc.NewProvider(providerCtx, altIssuer)
	if altErr != nil {
		slog.ErrorContext(ctx, "getOrDiscoverProvider: discovery failed with alternate issuer", "issuer", altIssuer, "error", altErr)
		return nil, err
	}
	span.SetAttributes(attribute.String("arcane.oidc.effective_issuer", altIssuer))
	slog.DebugContext(ctx, "getOrDiscoverProvider: provider cached", "issuer", key.issuer, "effectiveIssuer", altIssuer, "skipTls", key.skipTLS)
	return provider, nil
}

func (s *OidcService) HandleCallback(ctx context.Context, code, state, storedState, origin, mobileRedirectURI string) (_ *authtypes.OidcUserInfo, _ *authtypes.OidcTokenResponse, err error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "oidc.callback")
	defer func() { tracing.End(span, err) }()
	slog.DebugContext(ctx, "HandleCallback: processing callback", "codePresent", code != "", "statePresent", state != "")

	var stateData OidcState
	stateJSON, err := base64.URLEncoding.DecodeString(storedState)
	if err == nil {
		err = json.Unmarshal(stateJSON, &stateData)
	}
	if err != nil {
		slog.ErrorContext(ctx, "HandleCallback: failed to decode stored state", "error", err)
		return nil, nil, fmt.Errorf("invalid state parameter: %w", err)
	}
	if state != stateData.State {
		slog.ErrorContext(ctx, "HandleCallback: state mismatch", "receivedLen", len(state), "expectedLen", len(stateData.State))
		return nil, nil, errors.New("state parameter mismatch")
	}
	if time.Since(stateData.CreatedAt) > 10*time.Minute {
		slog.ErrorContext(ctx, "HandleCallback: state expired", "age", time.Since(stateData.CreatedAt))
		return nil, nil, errors.New("authentication state has expired")
	}

	cfg, err := s.effectiveConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "HandleCallback: failed to get OIDC config", "error", err)
		return nil, nil, err
	}
	span.SetAttributes(configAttributes(cfg)...)

	var provider *oidc.Provider
	if !hasManualEndpoints(cfg) {
		provider, err = s.getOrDiscoverProvider(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
	}

	oauth2Config, err := s.buildOAuth2Config(cfg, provider, origin, mobileRedirectURI)
	if err != nil {
		return nil, nil, err
	}
	token, err := oauth2Config.Exchange(oidc.ClientContext(ctx, s.httpClientFor(cfg.SkipTlsVerify)), code, oauth2.VerifierOption(stateData.CodeVerifier))
	if err != nil {
		slog.ErrorContext(ctx, "HandleCallback: token exchange failed", "tokenEndpoint", oauth2Config.Endpoint.TokenURL, "error", err)
		return nil, nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}
	slog.DebugContext(ctx, "HandleCallback: token exchange successful", "hasAccessToken", token.AccessToken != "", "hasRefreshToken", token.RefreshToken != "")

	idToken, rawIDToken, err := s.verifyIDToken(ctx, provider, cfg, token, stateData.Nonce)
	if err != nil {
		return nil, nil, err
	}

	return s.buildUserInfo(ctx, provider, cfg, token, idToken, rawIDToken)
}

func (s *OidcService) verifyIDToken(ctx context.Context, provider *oidc.Provider, cfg *settings.OidcConfig, token *oauth2.Token, nonce string) (*oidc.IDToken, string, error) {
	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		slog.WarnContext(ctx, "HandleCallback: no ID token in response (non-compliant OIDC response)")
		return nil, "", nil
	}

	issuer, jwksURL := cfg.IssuerURL, cfg.JwksURI
	if provider != nil {
		var meta struct {
			Issuer  string `json:"issuer"`
			JWKSURL string `json:"jwks_uri"`
		}
		if err := provider.Claims(&meta); err != nil {
			return nil, "", fmt.Errorf("failed to read provider metadata: %w", err)
		}
		issuer, jwksURL = meta.Issuer, meta.JWKSURL
	}
	if jwksURL == "" {
		return nil, "", errors.New("jwks URI must be configured when using manual OIDC endpoints")
	}
	if s.keySetManager == nil {
		return nil, "", errors.New("failed to configure provider JWK set: JWK set manager is not configured")
	}

	client := s.httpClientFor(cfg.SkipTlsVerify)
	keySet, err := s.keySetManager.KeySet(context.WithoutCancel(ctx), client, jwksURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to configure provider JWK set: %w", err)
	}
	verifier := oidc.NewVerifier(issuer, keySet, &oidc.Config{
		ClientID:             cfg.ClientID,
		SupportedSigningAlgs: oidcjwk.SupportedSigningAlgs(),
	})
	idToken, err := verifier.Verify(oidc.ClientContext(ctx, client), rawIDToken)
	if err != nil {
		slog.ErrorContext(ctx, "HandleCallback: ID token verification failed", "error", err)
		return nil, "", fmt.Errorf("failed to verify ID token: %w", err)
	}

	if nonce != "" {
		var claims struct {
			Nonce string `json:"nonce"`
		}
		if claimsErr := idToken.Claims(&claims); claimsErr != nil {
			slog.ErrorContext(ctx, "HandleCallback: failed to extract nonce from ID token", "error", claimsErr)
			return nil, "", fmt.Errorf("failed to verify nonce: %w", claimsErr)
		}
		if claims.Nonce != nonce {
			slog.ErrorContext(ctx, "HandleCallback: nonce mismatch", "expected", nonce, "got", claims.Nonce)
			return nil, "", errors.New("nonce verification failed")
		}
	}

	slog.DebugContext(ctx, "HandleCallback: ID token verified successfully", "subject", idToken.Subject, "issuer", idToken.Issuer)
	return idToken, rawIDToken, nil
}

func (s *OidcService) buildUserInfo(
	ctx context.Context, provider *oidc.Provider, cfg *settings.OidcConfig, token *oauth2.Token, idToken *oidc.IDToken, rawIDToken string,
) (*authtypes.OidcUserInfo, *authtypes.OidcTokenResponse, error) {
	var claims map[string]any
	if idToken != nil {
		if claimsErr := idToken.Claims(&claims); claimsErr != nil {
			slog.WarnContext(ctx, "HandleCallback: failed to extract claims from ID token", "error", claimsErr)
		}
	}

	// Userinfo claims fill gaps; ID token claims win on conflict.
	var userInfoClaims map[string]any
	var userInfoErr error
	client := s.httpClientFor(cfg.SkipTlsVerify)
	switch {
	case provider != nil:
		userInfo, fetchErr := provider.UserInfo(oidc.ClientContext(ctx, client), oauth2.StaticTokenSource(token))
		if fetchErr != nil {
			userInfoErr = fmt.Errorf("failed to fetch userinfo: %w", fetchErr)
		} else if decodeErr := userInfo.Claims(&userInfoClaims); decodeErr != nil {
			userInfoErr = fmt.Errorf("failed to decode userinfo claims: %w", decodeErr)
		}
	case cfg.UserinfoEndpoint != "" && token.AccessToken == "":
		userInfoErr = errors.New("failed to fetch userinfo: missing access token for userinfo request")
	case cfg.UserinfoEndpoint != "":
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, cfg.UserinfoEndpoint, http.NoBody)
		if requestErr != nil {
			userInfoErr = fmt.Errorf("failed to fetch userinfo: %w", requestErr)
			break
		}
		request.Header.Set("Authorization", cmp.Or(token.TokenType, "Bearer")+" "+token.AccessToken)
		request.Header.Set("Accept", "application/json")
		resp, doErr := client.Do(request)
		if doErr != nil {
			userInfoErr = fmt.Errorf("failed to fetch userinfo: %w", doErr)
			break
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			userInfoErr = fmt.Errorf("failed to fetch userinfo: userinfo endpoint returned status %d", resp.StatusCode)
		} else if decodeErr := json.UnmarshalRead(resp.Body, &userInfoClaims); decodeErr != nil {
			userInfoErr = fmt.Errorf("failed to fetch userinfo: %w", decodeErr)
		}
	case claims == nil:
		userInfoErr = errors.New("userinfo endpoint not configured")
	}
	if userInfoErr != nil {
		slog.DebugContext(ctx, "HandleCallback: userinfo request failed", "error", userInfoErr)
		if claims == nil {
			slog.ErrorContext(ctx, "HandleCallback: failed to fetch claims", "error", userInfoErr)
			return nil, nil, fmt.Errorf("failed to fetch user claims: %w", userInfoErr)
		}
		userInfoClaims = nil
	}
	if merged := maps.Clone(userInfoClaims); merged != nil {
		maps.Copy(merged, claims)
		claims = merged
	}

	subject := jwtclaims.GetStringClaim(claims, "sub")
	if subject == "" {
		slog.ErrorContext(ctx, "HandleCallback: missing required 'sub' claim")
		return nil, nil, errors.New("missing required 'sub' claim in user info")
	}

	userInfoDto := authtypes.OidcUserInfo{
		Subject:           subject,
		Name:              jwtclaims.GetStringClaim(claims, "name"),
		Email:             jwtclaims.GetStringClaim(claims, "email"),
		EmailVerified:     jwtclaims.GetBoolClaim(claims, "email_verified"),
		PreferredUsername: jwtclaims.GetStringClaim(claims, "preferred_username"),
		GivenName:         jwtclaims.GetStringClaim(claims, "given_name"),
		FamilyName:        jwtclaims.GetStringClaim(claims, "family_name"),
		Admin:             jwtclaims.GetBoolClaim(claims, "admin"),
		Roles:             jwtclaims.GetStringSliceClaim(claims, "roles"),
		Groups:            jwtclaims.GetStringSliceClaim(claims, "groups"),
		Extra:             claims,
	}

	tokenResp := &authtypes.OidcTokenResponse{
		AccessToken:  token.AccessToken,
		TokenType:    cmp.Or(token.TokenType, "Bearer"),
		RefreshToken: token.RefreshToken,
		IDToken:      rawIDToken,
	}
	if !token.Expiry.IsZero() {
		tokenResp.ExpiresIn = max(int(time.Until(token.Expiry).Seconds()), 0)
	}

	trace.SpanFromContext(ctx).SetAttributes(attribute.Int("arcane.oidc.groups", len(userInfoDto.Groups)), attribute.Int("arcane.oidc.roles", len(userInfoDto.Roles)))
	slog.InfoContext(ctx, "HandleCallback: authentication successful", "subject", userInfoDto.Subject, "email", userInfoDto.Email)
	return &userInfoDto, tokenResp, nil
}

// postForm sends an OAuth form POST and decodes the JSON response body, along with the HTTP status code.
func postForm(ctx context.Context, client *http.Client, endpoint string, values url.Values) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	decodeErr := json.UnmarshalRead(resp.Body, &body)
	return body, resp.StatusCode, decodeErr
}

// InitiateDeviceAuth initiates the OIDC device authorization flow.
func (s *OidcService) InitiateDeviceAuth(ctx context.Context) (_ *authtypes.OidcDeviceAuthResponse, err error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "oidc.device_authorize")
	defer func() { tracing.End(span, err) }()

	cfg, err := s.effectiveConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "InitiateDeviceAuth: failed to get OIDC config", "error", err)
		return nil, err
	}
	span.SetAttributes(configAttributes(cfg)...)

	deviceEndpoint := cfg.DeviceAuthorizationEndpoint
	if deviceEndpoint == "" {
		provider, discoverErr := s.getOrDiscoverProvider(ctx, cfg)
		if discoverErr != nil {
			return nil, fmt.Errorf("failed to discover provider: %w", discoverErr)
		}
		var claims struct {
			DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
		}
		if claimsErr := provider.Claims(&claims); claimsErr != nil {
			return nil, fmt.Errorf("failed to get device authorization endpoint from provider: %w", claimsErr)
		}
		if claims.DeviceAuthorizationEndpoint == "" {
			return nil, errors.New("device authorization endpoint not found in provider configuration")
		}
		deviceEndpoint = claims.DeviceAuthorizationEndpoint
	}

	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("scope", strings.Join(requestedScopes(cfg), " "))
	if cfg.ClientSecret != "" {
		values.Set("client_secret", cfg.ClientSecret)
	}

	respData, status, err := postForm(ctx, s.httpClientFor(cfg.SkipTlsVerify), deviceEndpoint, values)
	switch {
	case status == 0:
		slog.ErrorContext(ctx, "InitiateDeviceAuth: request failed", "error", err)
		return nil, fmt.Errorf("device authorization request failed: %w", err)
	case status < 200 || status >= 300:
		if errMsg, ok := respData["error"].(string); ok && err == nil {
			return nil, fmt.Errorf("device authorization failed: %s", errMsg)
		}
		return nil, fmt.Errorf("device authorization endpoint returned status %d", status)
	case err != nil:
		return nil, fmt.Errorf("failed to decode device authorization response: %w", err)
	}

	deviceCode, _ := respData["device_code"].(string)
	if deviceCode == "" {
		return nil, errors.New("invalid device_code in response")
	}
	userCode, _ := respData["user_code"].(string)
	if userCode == "" {
		return nil, errors.New("invalid user_code in response")
	}
	verificationUri, _ := respData["verification_uri"].(string)
	if verificationUri == "" {
		return nil, errors.New("invalid verification_uri in response")
	}
	expiresIn, ok := respData["expires_in"].(float64)
	if !ok {
		return nil, errors.New("invalid expires_in in response")
	}

	response := &authtypes.OidcDeviceAuthResponse{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationUri:         verificationUri,
		VerificationUriComplete: kit.As(respData["verification_uri_complete"], ""),
		ExpiresIn:               int(expiresIn),
		Interval:                int(kit.As(respData["interval"], float64(5))),
	}

	slog.DebugContext(ctx, "InitiateDeviceAuth: device authorization initiated", "userCode", response.UserCode, "expiresIn", response.ExpiresIn)
	return response, nil
}

// ExchangeDeviceToken exchanges a device code for tokens.
func (s *OidcService) ExchangeDeviceToken(ctx context.Context, deviceCode string) (_ *authtypes.OidcUserInfo, _ *authtypes.OidcTokenResponse, err error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "oidc.device_token")
	defer func() {
		// Pending and slow_down are normal polling outcomes, not failures.
		pending := err != nil && (err.Error() == "authorization_pending" || err.Error() == "slow_down")
		tracing.End(span, kit.Ternary(pending, nil, err))
	}()

	cfg, err := s.effectiveConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "ExchangeDeviceToken: failed to get OIDC config", "error", err)
		return nil, nil, err
	}
	span.SetAttributes(configAttributes(cfg)...)

	var provider *oidc.Provider
	if !hasManualEndpoints(cfg) {
		if provider, err = s.getOrDiscoverProvider(ctx, cfg); err != nil {
			return nil, nil, err
		}
	}
	tokenEndpoint := cfg.TokenEndpoint
	if tokenEndpoint == "" {
		endpointProvider := provider
		if endpointProvider == nil {
			if endpointProvider, err = s.getOrDiscoverProvider(ctx, cfg); err != nil {
				return nil, nil, err
			}
		}
		tokenEndpoint = endpointProvider.Endpoint().TokenURL
	}

	values := url.Values{}
	values.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	values.Set("device_code", deviceCode)
	values.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		values.Set("client_secret", cfg.ClientSecret)
	}

	tokenResp, status, err := postForm(ctx, s.httpClientFor(cfg.SkipTlsVerify), tokenEndpoint, values)
	if status == 0 {
		slog.ErrorContext(ctx, "ExchangeDeviceToken: token request failed", "error", err)
		return nil, nil, fmt.Errorf("token request failed: %w", err)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	if status < 200 || status >= 300 {
		errMsg, ok := tokenResp["error"].(string)
		switch {
		case !ok:
			return nil, nil, fmt.Errorf("token endpoint returned status %d", status)
		case errMsg == "authorization_pending", errMsg == "slow_down", errMsg == "expired_token", errMsg == "access_denied":
			return nil, nil, errors.New(errMsg)
		default:
			return nil, nil, fmt.Errorf("token exchange failed: %s", errMsg)
		}
	}

	accessToken, _ := tokenResp["access_token"].(string)
	if accessToken == "" {
		return nil, nil, errors.New("invalid access_token in response")
	}

	token := &oauth2.Token{
		AccessToken:  accessToken,
		TokenType:    kit.As(tokenResp["token_type"], "Bearer"),
		RefreshToken: kit.As(tokenResp["refresh_token"], ""),
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
