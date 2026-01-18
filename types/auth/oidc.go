package auth

import (
	"time"

	"github.com/getarcaneapp/arcane/types/user"
)

// OidcUserInfo represents user information retrieved from an OIDC provider.
type OidcUserInfo struct {
	// Subject is the unique identifier for the user at the OIDC provider.
	//
	// Required: true
	Subject string `json:"sub"`

	// Name is the full name of the user.
	//
	// Required: false
	Name string `json:"name,omitempty"`

	// Email is the email address of the user.
	//
	// Required: false
	Email string `json:"email,omitempty"`

	// EmailVerified indicates if the user's email has been verified.
	//
	// Required: false
	EmailVerified bool `json:"email_verified,omitempty"`

	// PreferredUsername is the user's preferred username.
	//
	// Required: false
	PreferredUsername string `json:"preferred_username,omitempty"`

	// GivenName is the user's given name (first name).
	//
	// Required: false
	GivenName string `json:"given_name,omitempty"`

	// FamilyName is the user's family name (last name).
	//
	// Required: false
	FamilyName string `json:"family_name,omitempty"`

	// Admin indicates if the user is an administrator.
	//
	// Required: false
	Admin bool `json:"admin,omitempty"`

	// Roles is a list of roles assigned to the user.
	//
	// Required: false
	Roles []string `json:"roles,omitempty"`

	// Groups is a list of groups the user belongs to.
	//
	// Required: false
	Groups []string `json:"groups,omitempty"`

	// Extra contains additional claims from the userinfo endpoint that are not
	// part of the standard OIDC claims. This field is not serialized to JSON.
	//
	// Required: false
	Extra map[string]any `json:"-"`
}

// OidcTokenResponse represents the response from an OIDC token endpoint.
type OidcTokenResponse struct {
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

	// ExpiresIn is the lifetime of the access token in seconds.
	//
	// Required: false
	ExpiresIn int `json:"expires_in,omitempty"`

	// IDToken is the OpenID Connect ID token.
	//
	// Required: false
	IDToken string `json:"id_token,omitempty"`
}

// OidcStatusInfo represents the status of OIDC configuration and usage.
type OidcStatusInfo struct {
	// EnvForced indicates if OIDC is forced via environment configuration.
	//
	// Required: true
	EnvForced bool `json:"envForced"`

	// EnvConfigured indicates if OIDC is configured via environment variables.
	//
	// Required: true
	EnvConfigured bool `json:"envConfigured"`

	// MergeAccounts indicates if accounts should be merged when using OIDC.
	//
	// Required: true
	MergeAccounts bool `json:"mergeAccounts"`
}

// OidcAuthUrlRequest is used to request an OIDC authorization URL.
type OidcAuthUrlRequest struct {
	// RedirectUri is the URI to redirect to after successful authentication.
	//
	// Required: true
	RedirectUri string `json:"redirectUri"`
}

// OidcAuthUrlResponse contains the generated OIDC authorization URL.
type OidcAuthUrlResponse struct {
	// AuthUrl is the URL to redirect the user to for OIDC authentication.
	//
	// Required: true
	AuthUrl string `json:"authUrl"`
}

// OidcConfigResponse contains the OIDC client configuration.
type OidcConfigResponse struct {
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
type OidcCallbackRequest struct {
	// Code is the authorization code from the OIDC provider.
	//
	// Required: true
	Code string `json:"code"`

	// State is the state parameter from the OIDC provider for CSRF protection.
	//
	// Required: true
	State string `json:"state"`
}

// OidcCallbackResponse contains the response from OIDC callback processing.
type OidcCallbackResponse struct {
	// Success indicates if the authentication was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Token is the JWT access token.
	//
	// Required: true
	Token string `json:"token"`

	// RefreshToken is the refresh token for obtaining new access tokens.
	//
	// Required: true
	RefreshToken string `json:"refreshToken"`

	// ExpiresAt is the expiration time of the access token.
	//
	// Required: true
	ExpiresAt time.Time `json:"expiresAt"`

	// User contains the authenticated user information.
	//
	// Required: true
	User user.User `json:"user"`
}

// OidcDeviceAuthRequest is used to request a device authorization code.
type OidcDeviceAuthRequest struct {
	// RedirectUri is optional and kept for consistency with other auth flows.
	//
	// Required: false
	RedirectUri string `json:"redirectUri,omitempty"`
}

// OidcDeviceAuthResponse contains the device authorization response.
type OidcDeviceAuthResponse struct {
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
type OidcDeviceTokenRequest struct {
	// DeviceCode is the device verification code from the authorization response.
	//
	// Required: true
	DeviceCode string `json:"deviceCode"`
}

// OidcDeviceTokenResponse contains the response from device token exchange.
type OidcDeviceTokenResponse struct {
	// Success indicates if the authentication was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Token is the JWT access token.
	//
	// Required: true
	Token string `json:"token"`

	// RefreshToken is the refresh token for obtaining new access tokens.
	//
	// Required: true
	RefreshToken string `json:"refreshToken"`

	// ExpiresAt is the expiration time of the access token.
	//
	// Required: true
	ExpiresAt time.Time `json:"expiresAt"`

	// User contains the authenticated user information.
	//
	// Required: true
	User user.User `json:"user"`
}
