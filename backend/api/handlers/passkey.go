package handlers

import (
	"context"
	stdjson "encoding/json"
	"net/http"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

type PasskeyHandler struct {
	passkeyService *services.PasskeyService
	authService    *services.AuthService
	userService    *services.UserService
}

type passkeyCredentialBody struct {
	CeremonyID string         `json:"ceremonyId,omitempty"`
	Credential map[string]any `json:"credential"`
	Name       string         `json:"name,omitempty"`
}

type mfaBeginBody struct {
	TransactionID string `json:"transactionId"`
}

type mfaFinishBody struct {
	TransactionID string         `json:"transactionId"`
	Credential    map[string]any `json:"credential"`
}

type mobilePasskeyFinishBody struct {
	CeremonyID    string         `json:"ceremonyId"`
	Credential    map[string]any `json:"credential"`
	CodeChallenge string         `json:"codeChallenge" minLength:"43" maxLength:"43"`
}

type mobilePasskeyExchangeBody struct {
	TransactionID string `json:"transactionId"`
	CodeVerifier  string `json:"codeVerifier" minLength:"43" maxLength:"43"`
}

type stepUpFinishBody struct {
	TransactionID string         `json:"transactionId"`
	Credential    map[string]any `json:"credential"`
}

type recoveryBody struct {
	TransactionID string `json:"transactionId"`
	Code          string `json:"code"`
}

type renamePasskeyBody struct {
	Name string `json:"name" minLength:"1" maxLength:"128"`
}

type passwordReauthBody struct {
	Password string `json:"password" minLength:"1"`
}

type PasskeyLoginBeginOutput struct {
	Body base.ApiResponse[passkeyBeginResponse]
}

type PasskeyLoginFinishInput struct {
	UserAgent string `header:"User-Agent"`
	Body      passkeyCredentialBody
}

type PasskeyLoginFinishOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.AuthenticationResponse]
}

type MobilePasskeyFinishInput struct {
	Body mobilePasskeyFinishBody
}

type MobilePasskeyFinishOutput struct {
	Body base.ApiResponse[auth.MobilePasskeyCompletion]
}

type MobilePasskeyExchangeInput struct {
	UserAgent string `header:"User-Agent"`
	Body      mobilePasskeyExchangeBody
}

type MobilePasskeyExchangeOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.AuthenticationResponse]
}

type PasskeyMFAStartInput struct {
	Body mfaBeginBody
}

type PasskeyMFAStartOutput struct {
	Body base.ApiResponse[auth.MFAChallenge]
}

type PasskeyMFAFinishInput struct {
	Body mfaFinishBody
}

type PasskeyMFAFinishOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.AuthenticationResponse]
}

type PasskeyRecoveryInput struct {
	Body recoveryBody
}

type PasskeyRecoveryOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      base.ApiResponse[auth.AuthenticationResponse]
}

type passkeyBeginResponse struct {
	CeremonyID    string    `json:"ceremonyId"`
	TransactionID string    `json:"transactionId,omitempty"`
	Options       any       `json:"options"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type ListMyPasskeysOutput struct {
	Body base.ApiResponse[[]services.PasskeySummary]
}

type GetPasskeyCapabilitiesOutput struct {
	Body base.ApiResponse[services.PasskeyCapabilities]
}

type BeginPasskeyRegistrationInput struct {
	StepUpToken string `header:"X-Step-Up-Token"`
}

type BeginPasskeyRegistrationOutput struct {
	Body base.ApiResponse[passkeyBeginResponse]
}

type FinishPasskeyRegistrationInput struct {
	UserAgent string `header:"User-Agent"`
	Body      passkeyCredentialBody
}

type FinishPasskeyRegistrationOutput struct {
	Body base.ApiResponse[services.PasskeySummary]
}

type RenamePasskeyInput struct {
	ID          string `path:"id"`
	StepUpToken string `header:"X-Step-Up-Token"`
	Body        renamePasskeyBody
}

type RenamePasskeyOutput struct {
	Body base.ApiResponse[services.PasskeySummary]
}

type DeletePasskeyInput struct {
	ID          string `path:"id"`
	StepUpToken string `header:"X-Step-Up-Token"`
}

type DeletePasskeyOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type BeginStepUpInput struct{}

type BeginStepUpOutput struct {
	Body base.ApiResponse[passkeyBeginResponse]
}

type FinishStepUpInput struct {
	Body stepUpFinishBody
}

type FinishStepUpOutput struct {
	Body base.ApiResponse[services.StepUpGrant]
}

type PasswordStepUpInput struct {
	Body passwordReauthBody
}

type PasswordStepUpOutput struct {
	Body base.ApiResponse[services.StepUpGrant]
}

type GetMFAStatusOutput struct {
	Body base.ApiResponse[services.MFAStatus]
}

type MFASettingsInput struct {
	StepUpToken string `header:"X-Step-Up-Token"`
}

type RecoveryCodesResponse struct {
	Codes []string `json:"codes" doc:"Recovery codes; shown only once"`
}

type MFARecoveryCodesOutput struct {
	Body base.ApiResponse[RecoveryCodesResponse]
}

func RegisterPasskeys(api huma.API, passkeyService *services.PasskeyService, authService *services.AuthService, userService *services.UserService) {
	h := &PasskeyHandler{passkeyService: passkeyService, authService: authService, userService: userService}
	public := []map[string][]string{}
	interactive := []map[string][]string{{"BearerAuth": {}}}

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
		Method:      http.MethodPut,
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
		Path:        "/auth/me/mfa/recovery-codes/regenerate",
		Summary:     "Regenerate MFA recovery codes",
		Tags:        []string{"Auth", "MFA"},
		Security:    interactive,
	}, h.RegenerateRecoveryCodes)
}

func (h *PasskeyHandler) BeginPasskeyLogin(ctx context.Context, _ *struct{}) (*PasskeyLoginBeginOutput, error) {
	challenge, err := h.passkeyService.BeginPasskeyLogin(ctx)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &PasskeyLoginBeginOutput{Body: base.ApiResponse[passkeyBeginResponse]{Success: true, Data: passkeyBeginResponseFromChallengeInternal(challenge)}}, nil
}

func (h *PasskeyHandler) FinishPasskeyLogin(ctx context.Context, input *PasskeyLoginFinishInput) (*PasskeyLoginFinishOutput, error) {
	payload, err := marshalCredentialInternal(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	userModel, err := h.passkeyService.FinishPasskeyLogin(ctx, input.Body.CeremonyID, payload)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	meta := auth.SessionMeta{UserAgent: input.UserAgent, IPAddress: humamw.GetRemoteAddrFromContext(ctx)}
	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, models.UserSessionSourcePasskey, "")
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponseInternal(ctx, userModel, tokenPair)
	if err != nil {
		return nil, err
	}
	return &PasskeyLoginFinishOutput{
		SetCookie: tokenCookieInternal(ctx, tokenPair),
		Body:      base.ApiResponse[auth.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) FinishMobilePasskeyLogin(ctx context.Context, input *MobilePasskeyFinishInput) (*MobilePasskeyFinishOutput, error) {
	payload, err := marshalCredentialInternal(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	completion, err := h.passkeyService.FinishMobilePasskeyLogin(ctx, input.Body.CeremonyID, payload, input.Body.CodeChallenge)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &MobilePasskeyFinishOutput{Body: base.ApiResponse[auth.MobilePasskeyCompletion]{Success: true, Data: *completion}}, nil
}

func (h *PasskeyHandler) ExchangeMobilePasskeyLogin(ctx context.Context, input *MobilePasskeyExchangeInput) (*MobilePasskeyExchangeOutput, error) {
	userModel, err := h.passkeyService.ExchangeMobilePasskeyLogin(ctx, input.Body.TransactionID, input.Body.CodeVerifier)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	meta := auth.SessionMeta{UserAgent: input.UserAgent, IPAddress: humamw.GetRemoteAddrFromContext(ctx)}
	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, models.UserSessionSourcePasskey, "")
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponseInternal(ctx, userModel, tokenPair)
	if err != nil {
		return nil, err
	}
	return &MobilePasskeyExchangeOutput{
		SetCookie: tokenCookieInternal(ctx, tokenPair),
		Body:      base.ApiResponse[auth.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) BeginMFA(ctx context.Context, input *PasskeyMFAStartInput) (*PasskeyMFAStartOutput, error) {
	challenge, err := h.passkeyService.BeginMFAForTransaction(ctx, input.Body.TransactionID)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &PasskeyMFAStartOutput{Body: base.ApiResponse[auth.MFAChallenge]{Success: true, Data: *challenge}}, nil
}

func (h *PasskeyHandler) FinishMFA(ctx context.Context, input *PasskeyMFAFinishInput) (*PasskeyMFAFinishOutput, error) {
	payload, err := marshalCredentialInternal(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	completion, err := h.passkeyService.FinishMFA(ctx, input.Body.TransactionID, payload)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	tokenPair, err := h.authService.CompleteLogin(ctx, completion.User, completion.Meta, completion.Source, completion.Meta.MFAMethod)
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponseInternal(ctx, completion.User, tokenPair)
	if err != nil {
		return nil, err
	}
	return &PasskeyMFAFinishOutput{
		SetCookie: tokenCookieInternal(ctx, tokenPair),
		Body:      base.ApiResponse[auth.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) UseRecoveryCode(ctx context.Context, input *PasskeyRecoveryInput) (*PasskeyRecoveryOutput, error) {
	completion, err := h.passkeyService.FinishRecoveryCode(ctx, input.Body.TransactionID, input.Body.Code)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	tokenPair, err := h.authService.CompleteLogin(ctx, completion.User, completion.Meta, completion.Source, completion.Meta.MFAMethod)
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponseInternal(ctx, completion.User, tokenPair)
	if err != nil {
		return nil, err
	}
	return &PasskeyRecoveryOutput{
		SetCookie: tokenCookieInternal(ctx, tokenPair),
		Body:      base.ApiResponse[auth.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) ListMyPasskeys(ctx context.Context, _ *struct{}) (*ListMyPasskeysOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	_ = sessionID
	rows, err := h.passkeyService.ListPasskeys(ctx, userModel.ID)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &ListMyPasskeysOutput{Body: base.ApiResponse[[]services.PasskeySummary]{Success: true, Data: rows}}, nil
}

func (h *PasskeyHandler) GetCapabilities(ctx context.Context, _ *struct{}) (*GetPasskeyCapabilitiesOutput, error) {
	userModel, _, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	oidcEnabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to check OIDC status")
	}
	capabilities, err := h.passkeyService.GetCapabilities(ctx, userModel.ID, oidcEnabled)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &GetPasskeyCapabilitiesOutput{Body: base.ApiResponse[services.PasskeyCapabilities]{Success: true, Data: *capabilities}}, nil
}

func (h *PasskeyHandler) BeginRegistration(ctx context.Context, input *BeginPasskeyRegistrationInput) (*BeginPasskeyRegistrationOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	challenge, err := h.passkeyService.BeginRegistration(ctx, userModel.ID, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &BeginPasskeyRegistrationOutput{Body: base.ApiResponse[passkeyBeginResponse]{Success: true, Data: passkeyBeginResponseFromChallengeInternal(challenge)}}, nil
}

func (h *PasskeyHandler) FinishRegistration(ctx context.Context, input *FinishPasskeyRegistrationInput) (*FinishPasskeyRegistrationOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := marshalCredentialInternal(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	row, err := h.passkeyService.FinishRegistration(ctx, userModel.ID, sessionID, input.Body.CeremonyID, payload, input.Body.Name)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &FinishPasskeyRegistrationOutput{Body: base.ApiResponse[services.PasskeySummary]{Success: true, Data: *row}}, nil
}

func (h *PasskeyHandler) RenamePasskey(ctx context.Context, input *RenamePasskeyInput) (*RenamePasskeyOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	row, err := h.passkeyService.RenamePasskey(ctx, userModel.ID, input.ID, input.Body.Name, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &RenamePasskeyOutput{Body: base.ApiResponse[services.PasskeySummary]{Success: true, Data: *row}}, nil
}

func (h *PasskeyHandler) DeletePasskey(ctx context.Context, input *DeletePasskeyInput) (*DeletePasskeyOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	oidcEnabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to check OIDC status")
	}
	if err := h.passkeyService.DeletePasskey(ctx, userModel.ID, input.ID, sessionID, input.StepUpToken, oidcEnabled); err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &DeletePasskeyOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Passkey deleted"}}}, nil
}

func (h *PasskeyHandler) BeginStepUp(ctx context.Context, _ *BeginStepUpInput) (*BeginStepUpOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	challenge, err := h.passkeyService.BeginStepUp(ctx, userModel.ID, sessionID, auth.SessionMeta{IPAddress: humamw.GetRemoteAddrFromContext(ctx)})
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &BeginStepUpOutput{Body: base.ApiResponse[passkeyBeginResponse]{Success: true, Data: passkeyBeginResponseFromChallengeInternal(challenge)}}, nil
}

func (h *PasskeyHandler) FinishStepUp(ctx context.Context, input *FinishStepUpInput) (*FinishStepUpOutput, error) {
	_, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := marshalCredentialInternal(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	grant, err := h.passkeyService.FinishStepUp(ctx, input.Body.TransactionID, sessionID, payload)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &FinishStepUpOutput{Body: base.ApiResponse[services.StepUpGrant]{Success: true, Data: *grant}}, nil
}

func (h *PasskeyHandler) PasswordStepUp(ctx context.Context, input *PasswordStepUpInput) (*PasswordStepUpOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(userModel.PasswordHash) == "" {
		return nil, huma.Error400BadRequest("this account has no local password")
	}
	if err := h.userService.ValidatePassword(userModel.PasswordHash, input.Body.Password); err != nil {
		return nil, huma.Error401Unauthorized("password is incorrect")
	}
	grant, err := h.passkeyService.CreatePasswordStepUpGrant(ctx, userModel.ID, sessionID)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &PasswordStepUpOutput{Body: base.ApiResponse[services.StepUpGrant]{Success: true, Data: *grant}}, nil
}

func (h *PasskeyHandler) GetMFAStatus(ctx context.Context, _ *struct{}) (*GetMFAStatusOutput, error) {
	userModel, _, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	status, err := h.passkeyService.GetMFAStatus(ctx, userModel.ID)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &GetMFAStatusOutput{Body: base.ApiResponse[services.MFAStatus]{Success: true, Data: *status}}, nil
}

func (h *PasskeyHandler) EnableMFA(ctx context.Context, input *MFASettingsInput) (*MFARecoveryCodesOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.passkeyService.EnableMFA(ctx, userModel.ID, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &MFARecoveryCodesOutput{Body: base.ApiResponse[RecoveryCodesResponse]{Success: true, Data: RecoveryCodesResponse{Codes: codes}}}, nil
}

func (h *PasskeyHandler) DisableMFA(ctx context.Context, input *MFASettingsInput) (*DeletePasskeyOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.passkeyService.DisableMFA(ctx, userModel.ID, sessionID, input.StepUpToken); err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &DeletePasskeyOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Passkey MFA disabled"}}}, nil
}

func (h *PasskeyHandler) RegenerateRecoveryCodes(ctx context.Context, input *MFASettingsInput) (*MFARecoveryCodesOutput, error) {
	userModel, sessionID, err := requireInteractiveSessionInternal(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.passkeyService.RegenerateRecoveryCodes(ctx, userModel.ID, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPErrorInternal(err)
	}
	return &MFARecoveryCodesOutput{Body: base.ApiResponse[RecoveryCodesResponse]{Success: true, Data: RecoveryCodesResponse{Codes: codes}}}, nil
}

func (h *PasskeyHandler) authenticationResponseInternal(ctx context.Context, userModel *models.User, tokenPair *services.TokenPair) (*auth.AuthenticationResponse, error) {
	if userModel == nil || tokenPair == nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	userDTO, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to map user")
	}
	expiresAt := tokenPair.ExpiresAt
	return &auth.AuthenticationResponse{
		Success:      true,
		Status:       auth.AuthenticationStatusAuthenticated,
		Token:        tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    &expiresAt,
		User:         &userDTO,
	}, nil
}

func requireInteractiveSessionInternal(ctx context.Context) (*models.User, string, error) {
	userModel, err := requireUserInternal(ctx)
	if err != nil {
		return nil, "", err
	}
	sessionID, ok := humamw.GetCurrentSessionIDFromContext(ctx)
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, "", huma.Error403Forbidden("passkey management requires an interactive session")
	}
	return userModel, sessionID, nil
}

func passkeyBeginResponseFromChallengeInternal(challenge *services.PasskeyChallenge) passkeyBeginResponse {
	return passkeyBeginResponse{
		CeremonyID:    challenge.CeremonyID,
		TransactionID: challenge.TransactionID,
		Options:       challenge.Options,
		ExpiresAt:     challenge.ExpiresAt,
	}
}

func marshalCredentialInternal(credential map[string]any) ([]byte, error) {
	if len(credential) == 0 {
		return nil, errors.New("credential is required")
	}
	return stdjson.Marshal(credential)
}

func tokenCookieInternal(ctx context.Context, tokenPair *services.TokenPair) []string {
	maxAge := max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0) + 60
	return []string{cookie.BuildTokenCookieStringFor(maxAge, tokenPair.AccessToken, cookie.SecureCookieFromContext(ctx))}
}

func passkeyHTTPErrorInternal(err error) error {
	switch {
	case errors.Is(err, services.ErrPasskeyNotFound):
		return huma.Error404NotFound("passkey not found")
	case errors.Is(err, services.ErrPasskeyExists):
		return huma.Error409Conflict("passkey is already registered")
	case errors.Is(err, services.ErrPasskeyMFAEnabled):
		return huma.Error409Conflict("disable passkey MFA before deleting the last passkey")
	case errors.Is(err, services.ErrPasskeyLastCredential):
		return huma.Error409Conflict("another usable authentication method is required")
	case errors.Is(err, services.ErrPasskeyStepUpRequired):
		return huma.Error401Unauthorized("fresh step-up authentication is required")
	case errors.Is(err, services.ErrPasskeyNoCredential):
		return huma.Error409Conflict("no passkeys are registered")
	case errors.Is(err, services.ErrPasskeyServiceUnavailable):
		return huma.Error500InternalServerError("passkey service is unavailable")
	case errors.Is(err, services.ErrPasskeyRecoveryCode):
		return huma.Error401Unauthorized("invalid recovery code")
	case errors.Is(err, services.ErrPasskeyName):
		return huma.Error400BadRequest("invalid passkey name")
	case errors.Is(err, services.ErrPasskeyMFAAlreadyEnabled):
		return huma.Error409Conflict("passkey MFA is already enabled")
	case errors.Is(err, services.ErrPasskeyMFANotEnabled):
		return huma.Error409Conflict("passkey MFA is not enabled")
	case errors.Is(err, services.ErrPasskeyCeremony), errors.Is(err, services.ErrPasskeyTransaction), errors.Is(err, services.ErrPasskeyResponse):
		return huma.Error400BadRequest("invalid or expired passkey authentication attempt")
	default:
		return huma.Error500InternalServerError("passkey authentication failed")
	}
}
