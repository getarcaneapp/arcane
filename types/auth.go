package types

import (
	"time"
)

type (
	// AuthLogin represents the login request body.
	AuthLogin struct {
		Username string `json:"username" minLength:"1" maxLength:"255" doc:"Username of the user" example:"admin"`
		Password string `json:"password" writeOnly:"true" minLength:"1" doc:"Password of the user"`
	}

	// AuthRefresh represents the token refresh request body.
	AuthRefresh struct {
		RefreshToken string `json:"refreshToken,omitempty" writeOnly:"true" doc:"Refresh token used to obtain a new access token; browsers send it as a cookie instead"`
	}

	AuthLogout struct {
		RefreshToken string `json:"refreshToken,omitempty" writeOnly:"true" doc:"Refresh token whose session is revoked even when the access token has expired"`
	}

	// PasswordChange represents the password change request body.
	PasswordChange struct {
		CurrentPassword string `json:"currentPassword,omitempty" writeOnly:"true" doc:"Current password of the user (required for non-OIDC users)"`
		NewPassword     string `json:"newPassword" writeOnly:"true" minLength:"8" doc:"New password for the user"`
	}

	// SessionMeta captures request metadata associated with an authenticated session.
	SessionMeta struct {
		MFAVerifiedAt *time.Time
		UserAgent     string
		IPAddress     string
		Source        string
		MFAMethod     string
	}

	AuthenticationStatus string

	// MFAChallenge describes a pending server-side second-factor transaction.
	// Options contains the WebAuthn public-key request options and is safe to
	// expose; the matching SessionData is retained by the backend.
	MFAChallenge struct {
		ExpiresAt     time.Time `json:"expiresAt" doc:"MFA transaction expiration time"`
		Options       any       `json:"options" doc:"WebAuthn assertion options"`
		TransactionID string    `json:"transactionId" doc:"Opaque MFA transaction identifier"`
		Method        string    `json:"method" doc:"MFA method"`
	}

	// AuthenticationResponse is returned by local, OIDC, and passkey login
	// endpoints. An authenticated response contains tokens and user data; an MFA
	// response contains only status and a pending challenge.
	AuthenticationResponse struct {
		ExpiresAt    *time.Time           `json:"expiresAt,omitempty" doc:"Expiration time of the access token"`
		User         *User                `json:"user,omitempty" doc:"Authenticated user information"`
		MFA          *MFAChallenge        `json:"mfa,omitempty" doc:"Pending MFA challenge"`
		Status       AuthenticationStatus `json:"status" doc:"Authentication state"`
		Token        string               `json:"token,omitempty" doc:"JWT access token"`
		RefreshToken string               `json:"refreshToken,omitempty" doc:"Refresh token for obtaining new access tokens"`
		Success      bool                 `json:"success" doc:"Whether the authentication request was accepted"`
	}

	// MobilePasskeyCompletion identifies a validated passkey login waiting for
	// its initiating mobile client to perform the one-time verifier exchange.
	MobilePasskeyCompletion struct {
		ExpiresAt     time.Time `json:"expiresAt" doc:"Mobile passkey transaction expiration time"`
		TransactionID string    `json:"transactionId" doc:"Opaque mobile passkey transaction identifier"`
	}

	// TokenRefreshResponse represents the successful token refresh response data.
	TokenRefreshResponse struct {
		ExpiresAt    time.Time `json:"expiresAt" doc:"Expiration time of the new access token"`
		Token        string    `json:"token" doc:"New JWT access token"`
		RefreshToken string    `json:"refreshToken" doc:"New refresh token"`
	}

	// AutoLoginConfig represents the auto-login configuration for the frontend.
	// Password is intentionally excluded from this response.
	AutoLoginConfig struct {
		Username string `json:"username" doc:"Username for auto-login (only returned if enabled)"`
		Enabled  bool   `json:"enabled" doc:"Whether auto-login is enabled"`
	}

	// PasskeyLoginAvailability reports whether the instance has a usable passkey.
	PasskeyLoginAvailability struct {
		Available bool `json:"available" doc:"Whether a passkey is registered for the current WebAuthn domain"`
	}

	// OidcUserInfo represents user information retrieved from an OIDC provider.
	OidcUserInfo struct {
		// Extra contains additional claims from the userinfo endpoint that are not
		// part of the standard OIDC claims. This field is not serialized to JSON.
		//
		// Required: false
		Extra map[string]any `json:"-"`

		// Subject is the unique identifier for the user at the OIDC provider.
		//
		// Required: true
		Subject string `json:"sub"`

		// Name is the full name of the user.
		//
		// Required: false
		Name string `json:"name,omitempty" unorm:"nfc" trim:"true"`

		// Email is the email address of the user.
		//
		// Required: false
		Email string `json:"email,omitempty"`

		// PreferredUsername is the user's preferred username.
		//
		// Required: false
		PreferredUsername string `json:"preferred_username,omitempty"`

		// GivenName is the user's given name (first name).
		//
		// Required: false
		GivenName string `json:"given_name,omitempty" unorm:"nfc" trim:"true"`

		// FamilyName is the user's family name (last name).
		//
		// Required: false
		FamilyName string `json:"family_name,omitempty" unorm:"nfc" trim:"true"`

		// Roles is a list of roles assigned to the user.
		//
		// Required: false
		Roles []string `json:"roles,omitempty"`

		// Groups is a list of groups the user belongs to.
		//
		// Required: false
		Groups []string `json:"groups,omitempty"`

		// EmailVerified indicates if the user's email has been verified.
		//
		// Required: false
		EmailVerified bool `json:"email_verified,omitempty"`

		// Admin indicates if the user is an administrator.
		//
		// Required: false
		Admin bool `json:"admin,omitempty"`
	}

	// OidcTokenResponse represents the response from an OIDC token endpoint.
	OidcTokenResponse struct {
		// AccessToken is the OAuth 2.0 access token.
		//
		// Required: true
		AccessToken string `json:"access_token"`

		// TokenType specifies the type of the access token (typically "Bearer").
		//
		// Required: true
		TokenType string `json:"token_type"`

		// RefreshToken is the OAuth 2.0 refresh token.
		//
		// Required: false
		RefreshToken string `json:"refresh_token,omitempty"`

		// IDToken is the OpenID Connect ID token.
		//
		// Required: false
		IDToken string `json:"id_token,omitempty"`

		// ExpiresIn is the lifetime of the access token in seconds.
		//
		// Required: false
		ExpiresIn int `json:"expires_in,omitempty"`
	}

	// OidcStatusInfo represents the status of OIDC configuration and usage.
	OidcStatusInfo struct {
		// ProviderName is the custom display name for the OIDC provider.
		//
		// Required: false
		ProviderName string `json:"providerName,omitempty"`

		// ProviderLogoUrl is the custom logo URL for the OIDC provider.
		//
		// Required: false
		ProviderLogoUrl string `json:"providerLogoUrl,omitempty"`
		// MergeAccounts indicates if accounts should be merged when using OIDC.
		//
		// Required: true
		MergeAccounts bool `json:"mergeAccounts"`
	}

	// OidcAuthUrlRequest is used to request an OIDC authorization URL.
	OidcAuthUrlRequest struct {
		// RedirectUri is the URI to redirect to after successful authentication.
		//
		// Required: true
		RedirectUri string `json:"redirectUri"`

		// MobileRedirectUri is the OAuth redirect URI for native mobile clients
		// (e.g., "arcane-mobile://oidc-callback"). When provided, it must match an
		// entry in the server's configured mobile redirect allowlist and is used
		// as the OAuth redirect_uri sent to the OIDC provider.
		//
		// Required: false
		MobileRedirectUri string `json:"mobileRedirectUri,omitempty"`
	}

	// OidcAuthUrlResponse contains the generated OIDC authorization URL.
	OidcAuthUrlResponse struct {
		// AuthUrl is the URL to redirect the user to for OIDC authentication.
		//
		// Required: true
		AuthUrl string `json:"authUrl"`
	}

	// OidcConfigResponse contains the OIDC client configuration.
	OidcConfigResponse struct {
		// ClientID is the OAuth 2.0 client identifier.
		//
		// Required: true
		ClientID string `json:"clientId"`

		// RedirectUri is the URI to redirect to after authentication.
		//
		// Required: true
		RedirectUri string `json:"redirectUri"`

		// IssuerUrl is the OIDC provider's issuer URL.
		//
		// Required: true
		IssuerUrl string `json:"issuerUrl"`

		// AuthorizationEndpoint is the URL of the authorization endpoint.
		//
		// Required: true
		AuthorizationEndpoint string `json:"authorizationEndpoint"`

		// TokenEndpoint is the URL of the token endpoint.
		//
		// Required: true
		TokenEndpoint string `json:"tokenEndpoint"`

		// UserinfoEndpoint is the URL of the userinfo endpoint.
		//
		// Required: true
		UserinfoEndpoint string `json:"userinfoEndpoint"`

		// DeviceAuthorizationEndpoint is the URL of the device authorization endpoint.
		//
		// Required: false
		DeviceAuthorizationEndpoint string `json:"deviceAuthorizationEndpoint,omitempty"`

		// Scopes is the space-separated list of OAuth scopes requested.
		//
		// Required: true
		Scopes string `json:"scopes"`
	}

	// OidcCallbackRequest contains the OIDC callback parameters.
	OidcCallbackRequest struct {
		// Code is the authorization code from the OIDC provider.
		//
		// Required: true
		Code string `json:"code"`

		// State is the state parameter from the OIDC provider for CSRF protection.
		//
		// Required: true
		State string `json:"state"`

		// MobileRedirectUri must match the value sent with the original
		// OidcAuthUrlRequest. OAuth requires the redirect_uri to be identical
		// across the auth code request and the token exchange.
		//
		// Required: false
		MobileRedirectUri string `json:"mobileRedirectUri,omitempty"`
	}

	// OidcDeviceAuthRequest is used to request a device authorization code.
	OidcDeviceAuthRequest struct {
		// RedirectUri is optional and kept for consistency with other auth flows.
		//
		// Required: false
		RedirectUri string `json:"redirectUri,omitempty"`
	}

	// OidcDeviceAuthResponse contains the device authorization response.
	OidcDeviceAuthResponse struct {
		// DeviceCode is the device verification code.
		//
		// Required: true
		DeviceCode string `json:"deviceCode"`

		// UserCode is the end-user verification code.
		//
		// Required: true
		UserCode string `json:"userCode"`

		// VerificationUri is the end-user verification URI.
		//
		// Required: true
		VerificationUri string `json:"verificationUri"`

		// VerificationUriComplete is the end-user verification URI with user code included.
		//
		// Required: false
		VerificationUriComplete string `json:"verificationUriComplete,omitempty"`

		// ExpiresIn is the lifetime of the device_code and user_code in seconds.
		//
		// Required: true
		ExpiresIn int `json:"expiresIn"`

		// Interval is the minimum polling interval in seconds.
		//
		// Required: false
		Interval int `json:"interval,omitempty"`
	}

	// OidcDeviceTokenRequest is used to exchange a device code for tokens.
	OidcDeviceTokenRequest struct {
		// DeviceCode is the device verification code from the authorization response.
		//
		// Required: true
		DeviceCode string `json:"deviceCode"`
	}

	SessionBootstrap struct {
		PermissionsManifest PermissionsManifest    `json:"permissionsManifest" doc:"Permission manifest used by navigation gates"`
		User                User                   `json:"user" doc:"Authenticated user"`
		Version             VersionInfo            `json:"version" doc:"Application version information"`
		Environments        Paginated[Environment] `json:"environments" doc:"Environments the user can access"`
	}
)

const (
	AuthenticationStatusAuthenticated AuthenticationStatus = "authenticated"
	AuthenticationStatusMFARequired   AuthenticationStatus = "mfa_required"
)
