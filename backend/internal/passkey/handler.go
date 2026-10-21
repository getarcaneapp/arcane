package passkey

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type PasskeyHandler struct {
	passkeyService *PasskeyService
	authService    *auth.AuthService
	userService    *user.UserService
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
	Password string `json:"password" writeOnly:"true" minLength:"1"`
}

type PasskeyLoginFinishInput struct {
	UserAgent string `header:"User-Agent"`
	Body      passkeyCredentialBody
}

type PasskeyLoginFinishOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      types.ApiResponse[types.AuthenticationResponse]
}

type MobilePasskeyFinishInput struct {
	Body mobilePasskeyFinishBody
}

type MobilePasskeyExchangeInput struct {
	UserAgent string `header:"User-Agent"`
	Body      mobilePasskeyExchangeBody
}

type MobilePasskeyExchangeOutput struct {
	Body types.ApiResponse[types.AuthenticationResponse]
}

type PasskeyMFAStartInput struct {
	Body mfaBeginBody
}

type PasskeyMFAFinishInput struct {
	Body mfaFinishBody
}

type PasskeyMFAFinishOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      types.ApiResponse[types.AuthenticationResponse]
}

type PasskeyRecoveryInput struct {
	Body recoveryBody
}

type PasskeyRecoveryOutput struct {
	SetCookie []string `header:"Set-Cookie" doc:"Session cookie"`
	Body      types.ApiResponse[types.AuthenticationResponse]
}

type passkeyBeginResponse struct {
	CeremonyID    string    `json:"ceremonyId"`
	TransactionID string    `json:"transactionId,omitempty"`
	Options       any       `json:"options"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type BeginPasskeyRegistrationInput struct {
	StepUpToken string `header:"X-Step-Up-Token"`
}

type FinishPasskeyRegistrationInput struct {
	UserAgent string `header:"User-Agent"`
	Body      passkeyCredentialBody
}

type RenamePasskeyInput struct {
	ID          string `path:"id"`
	StepUpToken string `header:"X-Step-Up-Token"`
	Body        renamePasskeyBody
}

type DeletePasskeyInput struct {
	ID          string `path:"id"`
	StepUpToken string `header:"X-Step-Up-Token"`
}

type BeginStepUpInput struct{}

type FinishStepUpInput struct {
	Body stepUpFinishBody
}

type PasswordStepUpInput struct {
	Body passwordReauthBody
}

type MFASettingsInput struct {
	StepUpToken string `header:"X-Step-Up-Token"`
}

type MFARecoveryActionInput struct {
	StepUpToken string `header:"X-Step-Up-Token"`
	Action      string `query:"action" required:"true" enum:"regenerate" doc:"Recovery code action"`
}

type RecoveryCodesResponse struct {
	Codes []string `json:"codes" doc:"Recovery codes; shown only once"`
}

func (h *PasskeyHandler) getLoginAvailability(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.PasskeyLoginAvailability], error) {
	available, err := h.passkeyService.LoginAvailable(ctx)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[types.PasskeyLoginAvailability]{
		Body: types.ApiResponse[types.PasskeyLoginAvailability]{
			Success: true,
			Data: types.PasskeyLoginAvailability{
				Available: available,
			},
		},
	}, nil
}

func (h *PasskeyHandler) BeginPasskeyLogin(ctx context.Context, _ *struct{}) (*handlerutil.Out[passkeyBeginResponse], error) {
	challenge, err := h.passkeyService.BeginPasskeyLogin(ctx)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[passkeyBeginResponse]{Body: types.ApiResponse[passkeyBeginResponse]{Success: true, Data: passkeyBeginResponseFromChallenge(challenge)}}, nil
}

func (h *PasskeyHandler) FinishPasskeyLogin(ctx context.Context, input *PasskeyLoginFinishInput) (*PasskeyLoginFinishOutput, error) {
	payload, err := marshalCredential(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	userModel, err := h.passkeyService.FinishPasskeyLogin(ctx, input.Body.CeremonyID, payload)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	meta := types.SessionMeta{UserAgent: input.UserAgent, IPAddress: middleware.GetRemoteAddrFromContext(ctx)}
	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, session.UserSessionSourcePasskey, "")
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponse(ctx, userModel, tokenPair)
	if err != nil {
		return nil, err
	}
	return &PasskeyLoginFinishOutput{
		SetCookie: tokenPair.CookieHeaders(cookie.SecureCookieFromContext(ctx)),
		Body:      types.ApiResponse[types.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) FinishMobilePasskeyLogin(ctx context.Context, input *MobilePasskeyFinishInput) (*handlerutil.Out[types.MobilePasskeyCompletion], error) {
	payload, err := marshalCredential(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	completion, err := h.passkeyService.FinishMobilePasskeyLogin(ctx, input.Body.CeremonyID, payload, input.Body.CodeChallenge)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[types.MobilePasskeyCompletion]{Body: types.ApiResponse[types.MobilePasskeyCompletion]{Success: true, Data: *completion}}, nil
}

func (h *PasskeyHandler) ExchangeMobilePasskeyLogin(ctx context.Context, input *MobilePasskeyExchangeInput) (*MobilePasskeyExchangeOutput, error) {
	userModel, err := h.passkeyService.ExchangeMobilePasskeyLogin(ctx, input.Body.TransactionID, input.Body.CodeVerifier)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	meta := types.SessionMeta{UserAgent: input.UserAgent, IPAddress: middleware.GetRemoteAddrFromContext(ctx)}
	tokenPair, err := h.authService.CompleteLogin(ctx, userModel, meta, session.UserSessionSourcePasskey, "")
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponse(ctx, userModel, tokenPair)
	if err != nil {
		return nil, err
	}
	return &MobilePasskeyExchangeOutput{
		Body: types.ApiResponse[types.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) BeginMFA(ctx context.Context, input *PasskeyMFAStartInput) (*handlerutil.Out[types.MFAChallenge], error) {
	challenge, err := h.passkeyService.BeginMFAForTransaction(ctx, input.Body.TransactionID)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[types.MFAChallenge]{Body: types.ApiResponse[types.MFAChallenge]{Success: true, Data: *challenge}}, nil
}

func (h *PasskeyHandler) FinishMFA(ctx context.Context, input *PasskeyMFAFinishInput) (*PasskeyMFAFinishOutput, error) {
	payload, err := marshalCredential(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	completion, err := h.passkeyService.FinishMFA(ctx, input.Body.TransactionID, payload)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	tokenPair, err := h.authService.CompleteLogin(ctx, completion.User, completion.Meta, completion.Source, completion.Meta.MFAMethod)
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponse(ctx, completion.User, tokenPair)
	if err != nil {
		return nil, err
	}
	return &PasskeyMFAFinishOutput{
		SetCookie: tokenPair.CookieHeaders(cookie.SecureCookieFromContext(ctx)),
		Body:      types.ApiResponse[types.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) UseRecoveryCode(ctx context.Context, input *PasskeyRecoveryInput) (*PasskeyRecoveryOutput, error) {
	completion, err := h.passkeyService.FinishRecoveryCode(ctx, input.Body.TransactionID, input.Body.Code)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	tokenPair, err := h.authService.CompleteLogin(ctx, completion.User, completion.Meta, completion.Source, completion.Meta.MFAMethod)
	if err != nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	response, err := h.authenticationResponse(ctx, completion.User, tokenPair)
	if err != nil {
		return nil, err
	}
	return &PasskeyRecoveryOutput{
		SetCookie: tokenPair.CookieHeaders(cookie.SecureCookieFromContext(ctx)),
		Body:      types.ApiResponse[types.AuthenticationResponse]{Success: true, Data: *response},
	}, nil
}

func (h *PasskeyHandler) ListMyPasskeys(ctx context.Context, _ *struct{}) (*handlerutil.Out[[]PasskeySummary], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	_ = sessionID
	rows, err := h.passkeyService.ListPasskeys(ctx, userModel.ID)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[[]PasskeySummary]{Body: types.ApiResponse[[]PasskeySummary]{Success: true, Data: rows}}, nil
}

func (h *PasskeyHandler) GetCapabilities(ctx context.Context, _ *struct{}) (*handlerutil.Out[PasskeyCapabilities], error) {
	userModel, _, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	oidcEnabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to check OIDC status")
	}
	capabilities, err := h.passkeyService.GetCapabilities(ctx, userModel.ID, oidcEnabled)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[PasskeyCapabilities]{Body: types.ApiResponse[PasskeyCapabilities]{Success: true, Data: *capabilities}}, nil
}

func (h *PasskeyHandler) BeginRegistration(ctx context.Context, input *BeginPasskeyRegistrationInput) (*handlerutil.Out[passkeyBeginResponse], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	challenge, err := h.passkeyService.BeginRegistration(ctx, userModel.ID, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[passkeyBeginResponse]{Body: types.ApiResponse[passkeyBeginResponse]{Success: true, Data: passkeyBeginResponseFromChallenge(challenge)}}, nil
}

func (h *PasskeyHandler) FinishRegistration(ctx context.Context, input *FinishPasskeyRegistrationInput) (*handlerutil.Out[PasskeySummary], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := marshalCredential(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	row, err := h.passkeyService.FinishRegistration(ctx, userModel.ID, sessionID, input.Body.CeremonyID, payload, input.Body.Name)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[PasskeySummary]{Body: types.ApiResponse[PasskeySummary]{Success: true, Data: *row}}, nil
}

func (h *PasskeyHandler) RenamePasskey(ctx context.Context, input *RenamePasskeyInput) (*handlerutil.Out[PasskeySummary], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	row, err := h.passkeyService.RenamePasskey(ctx, userModel.ID, input.ID, input.Body.Name, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[PasskeySummary]{Body: types.ApiResponse[PasskeySummary]{Success: true, Data: *row}}, nil
}

func (h *PasskeyHandler) DeletePasskey(ctx context.Context, input *DeletePasskeyInput) (*handlerutil.Out[types.MessageResponse], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	oidcEnabled, err := h.authService.IsOidcEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to check OIDC status")
	}
	if deletePasskeyErr := h.passkeyService.DeletePasskey(ctx, userModel.ID, input.ID, sessionID, input.StepUpToken, oidcEnabled); deletePasskeyErr != nil {
		return nil, passkeyHTTPError(deletePasskeyErr)
	}
	return handlerutil.MessageOutput("Passkey deleted", ""), nil
}

func (h *PasskeyHandler) BeginStepUp(ctx context.Context, _ *BeginStepUpInput) (*handlerutil.Out[passkeyBeginResponse], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	challenge, err := h.passkeyService.BeginStepUp(ctx, userModel.ID, sessionID, types.SessionMeta{IPAddress: middleware.GetRemoteAddrFromContext(ctx)})
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[passkeyBeginResponse]{Body: types.ApiResponse[passkeyBeginResponse]{Success: true, Data: passkeyBeginResponseFromChallenge(challenge)}}, nil
}

func (h *PasskeyHandler) FinishStepUp(ctx context.Context, input *FinishStepUpInput) (*handlerutil.Out[StepUpGrant], error) {
	_, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := marshalCredential(input.Body.Credential)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid passkey response")
	}
	grant, err := h.passkeyService.FinishStepUp(ctx, input.Body.TransactionID, sessionID, payload)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[StepUpGrant]{Body: types.ApiResponse[StepUpGrant]{Success: true, Data: *grant}}, nil
}

func (h *PasskeyHandler) PasswordStepUp(ctx context.Context, input *PasswordStepUpInput) (*handlerutil.Out[StepUpGrant], error) {
	actor, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	userModel, exists := user.CurrentUserFromContext(ctx)
	if !exists || userModel == nil {
		return nil, huma.Error401Unauthorized("Not authenticated")
	}
	if strings.TrimSpace(userModel.PasswordHash) == "" {
		return nil, huma.Error400BadRequest("this account has no local password")
	}
	if validatePasswordErr := h.userService.ValidatePassword(ctx, userModel.PasswordHash, input.Body.Password); validatePasswordErr != nil {
		return nil, huma.Error401Unauthorized("password is incorrect")
	}
	grant, err := h.passkeyService.CreatePasswordStepUpGrant(ctx, actor.ID, sessionID)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[StepUpGrant]{Body: types.ApiResponse[StepUpGrant]{Success: true, Data: *grant}}, nil
}

func (h *PasskeyHandler) GetMFAStatus(ctx context.Context, _ *struct{}) (*handlerutil.Out[MFAStatus], error) {
	userModel, _, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	status, err := h.passkeyService.GetMFAStatus(ctx, userModel.ID)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[MFAStatus]{Body: types.ApiResponse[MFAStatus]{Success: true, Data: *status}}, nil
}

func (h *PasskeyHandler) EnableMFA(ctx context.Context, input *MFASettingsInput) (*handlerutil.Out[RecoveryCodesResponse], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.passkeyService.EnableMFA(ctx, userModel.ID, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[RecoveryCodesResponse]{Body: types.ApiResponse[RecoveryCodesResponse]{Success: true, Data: RecoveryCodesResponse{Codes: codes}}}, nil
}

func (h *PasskeyHandler) DisableMFA(ctx context.Context, input *MFASettingsInput) (*handlerutil.Out[types.MessageResponse], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	if disableMFAErr := h.passkeyService.DisableMFA(ctx, userModel.ID, sessionID, input.StepUpToken); disableMFAErr != nil {
		return nil, passkeyHTTPError(disableMFAErr)
	}
	h.authService.InvalidateUserTokenCache(userModel.ID)
	return handlerutil.MessageOutput("Passkey MFA disabled", ""), nil
}

func (h *PasskeyHandler) RegenerateRecoveryCodes(ctx context.Context, input *MFARecoveryActionInput) (*handlerutil.Out[RecoveryCodesResponse], error) {
	userModel, sessionID, err := requireInteractiveSession(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.passkeyService.RegenerateRecoveryCodes(ctx, userModel.ID, sessionID, input.StepUpToken)
	if err != nil {
		return nil, passkeyHTTPError(err)
	}
	return &handlerutil.Out[RecoveryCodesResponse]{Body: types.ApiResponse[RecoveryCodesResponse]{Success: true, Data: RecoveryCodesResponse{Codes: codes}}}, nil
}

func (h *PasskeyHandler) authenticationResponse(ctx context.Context, userModel *user.User, tokenPair *auth.TokenPair) (*types.AuthenticationResponse, error) {
	if userModel == nil || tokenPair == nil {
		return nil, huma.Error500InternalServerError("authentication failed")
	}
	userDTO, err := h.userService.ToUserResponseDto(ctx, *userModel)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to map user")
	}
	expiresAt := tokenPair.ExpiresAt
	return &types.AuthenticationResponse{
		Success:      true,
		Status:       types.AuthenticationStatusAuthenticated,
		Token:        tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    &expiresAt,
		User:         &userDTO,
	}, nil
}

func requireInteractiveSession(ctx context.Context) (*types.Actor, string, error) {
	userModel, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, "", err
	}
	sessionID, ok := middleware.GetCurrentSessionIDFromContext(ctx)
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, "", huma.Error403Forbidden("passkey management requires an interactive session")
	}
	return userModel, sessionID, nil
}

func passkeyBeginResponseFromChallenge(challenge *PasskeyChallenge) passkeyBeginResponse {
	return passkeyBeginResponse{
		CeremonyID:    challenge.CeremonyID,
		TransactionID: challenge.TransactionID,
		Options:       challenge.Options,
		ExpiresAt:     challenge.ExpiresAt,
	}
}

func marshalCredential(credential map[string]any) ([]byte, error) {
	if len(credential) == 0 {
		return nil, errors.New("credential is required")
	}
	return json.Marshal(credential)
}

func passkeyHTTPError(err error) error {
	switch {
	case errors.Is(err, ErrPasskeyNotFound):
		return huma.Error404NotFound("passkey not found")
	case errors.Is(err, ErrPasskeyExists):
		return huma.Error409Conflict("passkey is already registered")
	case errors.Is(err, ErrPasskeyMFAEnabled):
		return huma.Error409Conflict("disable passkey MFA before deleting the last passkey")
	case errors.Is(err, ErrPasskeyLastCredential):
		return huma.Error409Conflict("another usable authentication method is required")
	case errors.Is(err, ErrPasskeyStepUpRequired):
		return huma.Error401Unauthorized("fresh step-up authentication is required")
	case errors.Is(err, ErrPasskeyNoCredential):
		return huma.Error409Conflict("no passkeys are registered")
	case errors.Is(err, ErrPasskeyServiceUnavailable):
		return huma.Error500InternalServerError("passkey service is unavailable")
	case errors.Is(err, ErrPasskeyRecoveryCode):
		return huma.Error401Unauthorized("invalid recovery code")
	case errors.Is(err, ErrPasskeyName):
		return huma.Error400BadRequest("invalid passkey name")
	case errors.Is(err, ErrPasskeyMFAAlreadyEnabled):
		return huma.Error409Conflict("passkey MFA is already enabled")
	case errors.Is(err, ErrPasskeyMFANotEnabled):
		return huma.Error409Conflict("passkey MFA is not enabled")
	case errors.Is(err, ErrPasskeyCeremony), errors.Is(err, ErrPasskeyTransaction), errors.Is(err, ErrPasskeyResponse):
		return huma.Error400BadRequest("invalid or expired passkey authentication attempt")
	default:
		return huma.Error500InternalServerError("passkey authentication failed")
	}
}
