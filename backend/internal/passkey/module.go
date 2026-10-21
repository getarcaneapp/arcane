package passkey

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
)

func RegisterPasskeys(api huma.API, passkeyService *PasskeyService, authService *auth.AuthService, userService *user.UserService) {
	h := &PasskeyHandler{passkeyService: passkeyService, authService: authService, userService: userService}
	public := []map[string][]string{}
	interactive := []map[string][]string{{"BearerAuth": {}}}

	huma.Register(api, huma.Operation{
		OperationID: "get-passkey-login-availability",
		Method:      http.MethodGet,
		Path:        "/auth/passkey/login/availability",
		Summary:     "Get passkey login availability",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    public,
		Middlewares: huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
			ctx.SetHeader("Cache-Control", "no-store")
			next(ctx)
		}},
	}, h.getLoginAvailability)

	huma.Register(api, huma.Operation{
		OperationID: "begin-passkey-login",
		Method:      http.MethodPost,
		Path:        "/auth/passkey/login/begin",
		Summary:     "Begin passkey login",
		Description: "Begin a discoverable WebAuthn passkey login ceremony",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    public,
	}, h.BeginPasskeyLogin)
	huma.Register(api, huma.Operation{
		OperationID: "finish-passkey-login",
		Method:      http.MethodPost,
		Path:        "/auth/passkey/login/finish",
		Summary:     "Finish passkey login",
		Description: "Validate a discoverable WebAuthn assertion and create a session",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    public,
	}, h.FinishPasskeyLogin)
	huma.Register(api, huma.Operation{
		OperationID: "finish-mobile-passkey-login",
		Method:      http.MethodPost,
		Path:        "/auth/passkey/mobile/finish",
		Summary:     "Finish mobile passkey login",
		Description: "Validate a browser assertion and create a one-time mobile exchange",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    public,
	}, h.FinishMobilePasskeyLogin)
	huma.Register(api, huma.Operation{
		OperationID: "exchange-mobile-passkey-login",
		Method:      http.MethodPost,
		Path:        "/auth/passkey/mobile/exchange",
		Summary:     "Exchange mobile passkey login",
		Description: "Consume a verifier-bound mobile passkey transaction and create a session",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    public,
	}, h.ExchangeMobilePasskeyLogin)

	huma.Register(api, huma.Operation{
		OperationID: "begin-passkey-mfa",
		Method:      http.MethodPost,
		Path:        "/auth/mfa/passkey/begin",
		Summary:     "Begin passkey MFA",
		Description: "Begin a WebAuthn assertion for a pending MFA transaction",
		Tags:        []string{"Auth", "MFA"},
		Security:    public,
	}, h.BeginMFA)
	huma.Register(api, huma.Operation{
		OperationID: "finish-passkey-mfa",
		Method:      http.MethodPost,
		Path:        "/auth/mfa/passkey/finish",
		Summary:     "Finish passkey MFA",
		Description: "Validate a WebAuthn MFA assertion and create the authenticated session",
		Tags:        []string{"Auth", "MFA"},
		Security:    public,
	}, h.FinishMFA)
	huma.Register(api, huma.Operation{
		OperationID: "use-passkey-recovery-code",
		Method:      http.MethodPost,
		Path:        "/auth/mfa/recovery",
		Summary:     "Use an MFA recovery code",
		Description: "Consume one recovery code for a pending MFA transaction",
		Tags:        []string{"Auth", "MFA"},
		Security:    public,
	}, h.UseRecoveryCode)

	huma.Register(api, huma.Operation{
		OperationID: "list-my-passkeys",
		Method:      http.MethodGet,
		Path:        "/auth/me/passkeys",
		Summary:     "List my passkeys",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.ListMyPasskeys)
	huma.Register(api, huma.Operation{
		OperationID: "get-passkey-capabilities",
		Method:      http.MethodGet,
		Path:        "/auth/me/passkeys/capabilities",
		Summary:     "Get passkey capabilities",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.GetCapabilities)
	huma.Register(api, huma.Operation{
		OperationID: "begin-passkey-registration",
		Method:      http.MethodPost,
		Path:        "/auth/me/passkeys/register/begin",
		Summary:     "Begin passkey registration",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.BeginRegistration)
	huma.Register(api, huma.Operation{
		OperationID: "finish-passkey-registration",
		Method:      http.MethodPost,
		Path:        "/auth/me/passkeys/register/finish",
		Summary:     "Finish passkey registration",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.FinishRegistration)
	huma.Register(api, huma.Operation{
		OperationID: "rename-my-passkey",
		Method:      http.MethodPatch,
		Path:        "/auth/me/passkeys/{id}",
		Summary:     "Rename my passkey",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.RenamePasskey)
	huma.Register(api, huma.Operation{
		OperationID: "delete-my-passkey",
		Method:      http.MethodDelete,
		Path:        "/auth/me/passkeys/{id}",
		Summary:     "Delete my passkey",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.DeletePasskey)
	huma.Register(api, huma.Operation{
		OperationID: "begin-passkey-step-up",
		Method:      http.MethodPost,
		Path:        "/auth/me/passkeys/reauth/begin",
		Summary:     "Begin passkey step-up",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.BeginStepUp)
	huma.Register(api, huma.Operation{
		OperationID: "finish-passkey-step-up",
		Method:      http.MethodPost,
		Path:        "/auth/me/passkeys/reauth/finish",
		Summary:     "Finish passkey step-up",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.FinishStepUp)
	huma.Register(api, huma.Operation{
		OperationID: "password-step-up",
		Method:      http.MethodPost,
		Path:        "/auth/me/passkeys/reauth/password",
		Summary:     "Reauthenticate with password",
		Tags:        []string{"Auth", "Passkeys"},
		Security:    interactive,
	}, h.PasswordStepUp)

	huma.Register(api, huma.Operation{
		OperationID: "get-passkey-mfa-status",
		Method:      http.MethodGet,
		Path:        "/auth/me/mfa",
		Summary:     "Get passkey MFA status",
		Tags:        []string{"Auth", "MFA"},
		Security:    interactive,
	}, h.GetMFAStatus)
	huma.Register(api, huma.Operation{
		OperationID: "enable-passkey-mfa",
		Method:      http.MethodPost,
		Path:        "/auth/me/mfa/enable",
		Summary:     "Enable passkey MFA",
		Tags:        []string{"Auth", "MFA"},
		Security:    interactive,
	}, h.EnableMFA)
	huma.Register(api, huma.Operation{
		OperationID: "disable-passkey-mfa",
		Method:      http.MethodPost,
		Path:        "/auth/me/mfa/disable",
		Summary:     "Disable passkey MFA",
		Tags:        []string{"Auth", "MFA"},
		Security:    interactive,
	}, h.DisableMFA)
	huma.Register(api, huma.Operation{
		OperationID: "regenerate-passkey-recovery-codes",
		Method:      http.MethodPost,
		Path:        "/auth/me/mfa/recovery",
		Summary:     "Regenerate MFA recovery codes",
		Tags:        []string{"Auth", "MFA"},
		Security:    interactive,
	}, h.RegenerateRecoveryCodes)
}
