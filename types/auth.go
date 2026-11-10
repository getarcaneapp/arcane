package types

import (
	"time"
)

type (
	AuthLogin struct {
		Username string `json:"username" minLength:"1" maxLength:"255" doc:"Username of the user" example:"admin"`
		Password string `json:"password" writeOnly:"true" minLength:"1" doc:"Password of the user"`
	}

	AuthRefresh struct {
		RefreshToken string `json:"refreshToken,omitempty" writeOnly:"true" doc:"Refresh token used to obtain a new access token; browsers send it as a cookie instead"`
	}

	AuthLogout struct {
		RefreshToken string `json:"refreshToken,omitempty" writeOnly:"true" doc:"Refresh token whose session is revoked even when the access token has expired"`
	}

	PasswordChange struct {
		CurrentPassword string `json:"currentPassword,omitempty" writeOnly:"true" doc:"Current password of the user (required for non-OIDC users)"`
		NewPassword     string `json:"newPassword" writeOnly:"true" minLength:"8" doc:"New password for the user"`
	}

	SessionMeta struct {
		MFAVerifiedAt *time.Time
		UserAgent     string
		IPAddress     string
		Source        string
		MFAMethod     string
	}

	AuthenticationStatus string

	MFAChallenge struct {
		ExpiresAt     time.Time `json:"expiresAt" doc:"MFA transaction expiration time"`
		Options       any       `json:"options" doc:"WebAuthn assertion options"`
		TransactionID string    `json:"transactionId" doc:"Opaque MFA transaction identifier"`
		Method        string    `json:"method" doc:"MFA method"`
	}

	AuthenticationResponse struct {
		ExpiresAt    *time.Time           `json:"expiresAt,omitempty" doc:"Expiration time of the access token"`
		User         *User                `json:"user,omitempty" doc:"Authenticated user information"`
		MFA          *MFAChallenge        `json:"mfa,omitempty" doc:"Pending MFA challenge"`
		Status       AuthenticationStatus `json:"status" doc:"Authentication state"`
		Token        string               `json:"token,omitempty" doc:"JWT access token"`
		RefreshToken string               `json:"refreshToken,omitempty" doc:"Refresh token for obtaining new access tokens"`
		Success      bool                 `json:"success" doc:"Whether the authentication request was accepted"`
	}

	MobilePasskeyCompletion struct {
		ExpiresAt     time.Time `json:"expiresAt" doc:"Mobile passkey transaction expiration time"`
		TransactionID string    `json:"transactionId" doc:"Opaque mobile passkey transaction identifier"`
	}

	TokenRefreshResponse struct {
		ExpiresAt    time.Time `json:"expiresAt" doc:"Expiration time of the new access token"`
		Token        string    `json:"token" doc:"New JWT access token"`
		RefreshToken string    `json:"refreshToken" doc:"New refresh token"`
	}

	// Password is intentionally excluded from this response.
	AutoLoginConfig struct {
		Username string `json:"username" doc:"Username for auto-login (only returned if enabled)"`
		Enabled  bool   `json:"enabled" doc:"Whether auto-login is enabled"`
	}

	PasskeyLoginAvailability struct {
		Available bool `json:"available" doc:"Whether a passkey is registered for the current WebAuthn domain"`
	}

	OidcUserInfo struct {
		Extra             map[string]any `json:"-"`
		Subject           string         `json:"sub"`
		Name              string         `json:"name,omitempty" unorm:"nfc" trim:"true"`
		Email             string         `json:"email,omitempty"`
		PreferredUsername string         `json:"preferred_username,omitempty"`
		GivenName         string         `json:"given_name,omitempty" unorm:"nfc" trim:"true"`
		FamilyName        string         `json:"family_name,omitempty" unorm:"nfc" trim:"true"`
		Roles             []string       `json:"roles,omitempty"`
		Groups            []string       `json:"groups,omitempty"`
		EmailVerified     bool           `json:"email_verified,omitempty"`
		Admin             bool           `json:"admin,omitempty"`
	}

	OidcTokenResponse struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token,omitempty"`
		IDToken      string `json:"id_token,omitempty"`

		ExpiresIn int `json:"expires_in,omitempty"`
	}

	OidcStatusInfo struct {
		ProviderName    string `json:"providerName,omitempty"`
		ProviderLogoUrl string `json:"providerLogoUrl,omitempty"`
		MergeAccounts   bool   `json:"mergeAccounts"`
	}

	OidcAuthUrlRequest struct {
		RedirectUri string `json:"redirectUri"`

		MobileRedirectUri string `json:"mobileRedirectUri,omitempty"`
	}

	OidcAuthUrlResponse struct {
		AuthUrl string `json:"authUrl"`
	}

	OidcConfigResponse struct {
		ClientID                    string `json:"clientId"`
		RedirectUri                 string `json:"redirectUri"`
		IssuerUrl                   string `json:"issuerUrl"`
		AuthorizationEndpoint       string `json:"authorizationEndpoint"`
		TokenEndpoint               string `json:"tokenEndpoint"`
		UserinfoEndpoint            string `json:"userinfoEndpoint"`
		DeviceAuthorizationEndpoint string `json:"deviceAuthorizationEndpoint,omitempty"`
		Scopes                      string `json:"scopes"`
	}

	OidcCallbackRequest struct {
		Code  string `json:"code"`
		State string `json:"state"`

		MobileRedirectUri string `json:"mobileRedirectUri,omitempty"`
	}

	OidcDeviceAuthRequest struct {
		RedirectUri string `json:"redirectUri,omitempty"`
	}

	OidcDeviceAuthResponse struct {
		DeviceCode              string `json:"deviceCode"`
		UserCode                string `json:"userCode"`
		VerificationUri         string `json:"verificationUri"`
		VerificationUriComplete string `json:"verificationUriComplete,omitempty"`

		ExpiresIn int `json:"expiresIn"`

		Interval int `json:"interval,omitempty"`
	}

	OidcDeviceTokenRequest struct {
		DeviceCode string `json:"deviceCode"`
	}

	SessionBootstrap struct {
		User         User                   `json:"user" doc:"Authenticated user"`
		Version      VersionInfo            `json:"version" doc:"Application version information"`
		Environments Paginated[Environment] `json:"environments" doc:"Environments the user can access"`
	}
)

const (
	AuthenticationStatusAuthenticated AuthenticationStatus = "authenticated"
	AuthenticationStatusMFARequired   AuthenticationStatus = "mfa_required"
)
