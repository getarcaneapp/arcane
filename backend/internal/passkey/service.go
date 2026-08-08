package passkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	passkeyCeremonyPurposeLogin        = "login"
	passkeyCeremonyPurposeMFA          = "mfa"
	passkeyCeremonyPurposeRegistration = "registration"
	passkeyCeremonyPurposeStepUp       = "step_up"

	authTransactionKindMFA           = "mfa"
	authTransactionKindStepUp        = "step_up"
	authTransactionKindMobilePasskey = "mobile_passkey"
	authTransactionPending           = "pending"
	authTransactionCompleted         = "completed"

	passkeyCeremonyTTL     = 5 * time.Minute
	passkeyStepUpTTL       = 5 * time.Minute
	maxPasskeyPayloadBytes = 256 * 1024
	maxPasskeyNameRunes    = 128
	recoveryCodeCount      = 10
)

var (
	ErrPasskeyServiceUnavailable = errors.Sentinel("passkey service is unavailable")
	ErrPasskeyCeremony           = errors.Sentinel("invalid or expired passkey ceremony")
	ErrPasskeyTransaction        = errors.Sentinel("invalid or expired authentication transaction")
	ErrPasskeyResponse           = errors.Sentinel("invalid passkey response")
	ErrPasskeyNotFound           = errors.Sentinel("passkey not found")
	ErrPasskeyExists             = errors.Sentinel("passkey already registered")
	ErrPasskeyStepUpRequired     = errors.Sentinel("fresh step-up authentication is required")
	ErrPasskeyMFAEnabled         = errors.Sentinel("passkey MFA must be disabled first")
	ErrPasskeyMFAAlreadyEnabled  = errors.Sentinel("passkey MFA is already enabled")
	ErrPasskeyMFANotEnabled      = errors.Sentinel("passkey MFA is not enabled")
	ErrPasskeyNoCredential       = errors.Sentinel("no passkeys are registered")
	ErrPasskeyLastCredential     = errors.Sentinel("cannot remove the last usable authentication method")
	ErrPasskeyRecoveryCode       = errors.Sentinel("invalid recovery code")
	ErrPasskeyName               = errors.Sentinel("invalid passkey name")

	authenticatorNamesOnce sync.Once
	authenticatorNames     map[string]string
)

// PasskeyChallenge is the public result of a WebAuthn ceremony begin.
// The matching SessionData remains in passkey_ceremonies.
type PasskeyChallenge struct {
	CeremonyID    string
	TransactionID string
	Options       any
	ExpiresAt     time.Time
}

// PasskeySummary is the safe representation of a stored credential.
type PasskeySummary struct {
	ID                      string     `json:"id"`
	Name                    string     `json:"name"`
	RPID                    string     `json:"rpId"`
	AAGUID                  string     `json:"aaguid,omitempty"`
	Transports              []string   `json:"transports,omitempty"`
	BackupEligible          bool       `json:"backupEligible"`
	BackupState             bool       `json:"backupState"`
	CloneWarning            bool       `json:"cloneWarning"`
	AuthenticatorAttachment string     `json:"authenticatorAttachment,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               *time.Time `json:"updatedAt,omitempty"`
	LastUsedAt              *time.Time `json:"lastUsedAt,omitempty"`
}

// PasskeyCapabilities describes the server-derived authentication choices for
// the current account. It deliberately does not trust client-supplied flags.
type PasskeyCapabilities struct {
	PasskeyMFAEnabled          bool `json:"passkeyMfaEnabled"`
	PasskeyCount               int  `json:"passkeyCount"`
	HasLocalPassword           bool `json:"hasLocalPassword"`
	HasOIDCFallback            bool `json:"hasOidcFallback"`
	CanEnrollWithActiveSession bool `json:"canEnrollWithActiveSession"`
	CanDeleteLastPasskey       bool `json:"canDeleteLastPasskey"`
	RequiresStepUp             bool `json:"requiresStepUp"`
}

// MFAStatus is the account's passkey MFA state. Recovery codes are counted,
// but their plaintext values are never returned after generation.
type MFAStatus struct {
	Enabled                bool `json:"enabled"`
	PasskeyCount           int  `json:"passkeyCount"`
	RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
}

// StepUpGrant is returned after a fresh password or passkey assertion. The
// token is hashed in the database and stays usable, for the issuing session
// only, until ExpiresAt.
type StepUpGrant struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// AuthenticationCompletion carries the original primary-auth metadata across
// an MFA transaction so the eventual session cannot be assigned client-chosen
// source or network metadata.
type AuthenticationCompletion struct {
	User   *models.User
	Meta   auth.SessionMeta
	Source string
}

type passkeyService struct {
	db       *database.DB
	webAuthn *webauthn.WebAuthn
	rpID     string
	initErr  error
}

// PasskeyService owns WebAuthn ceremonies, passkey persistence, MFA
// transactions, recovery codes, and step-up grants. It intentionally does not
// issue JWTs; auth.AuthService remains the single token/session issuer.
type PasskeyService = passkeyService

func NewPasskeyService(db *database.DB, cfg *config.Config) *PasskeyService {
	s := &passkeyService{db: db}

	appURL := "http://localhost:3552"
	if cfg != nil {
		appURL = cfg.GetAppURL()
	}
	parsedURL, err := url.Parse(strings.TrimSpace(appURL))
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		s.initErr = errors.WrapIf(err, "invalid APP_URL for WebAuthn")
		if err == nil {
			s.initErr = errors.New("invalid APP_URL for WebAuthn")
		}
		return s
	}

	origin := parsedURL.Scheme + "://" + parsedURL.Host
	s.rpID = parsedURL.Hostname()
	if s.rpID == "" {
		s.initErr = errors.New("APP_URL has no WebAuthn relying-party host")
		return s
	}

	s.webAuthn, s.initErr = webauthn.New(&webauthn.Config{
		RPID:          s.rpID,
		RPDisplayName: "Arcane",
		RPOrigins:     []string{origin},
		RPTopOrigins:  []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTL, TimeoutUVD: passkeyCeremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTL, TimeoutUVD: passkeyCeremonyTTL},
		},
	})

	return s
}

func (s *passkeyService) readyInternal() error {
	if s == nil || s.db == nil || s.webAuthn == nil || s.initErr != nil {
		return ErrPasskeyServiceUnavailable
	}
	return nil
}

type webAuthnUser struct {
	model              models.User
	credentials        []webauthn.Credential
	credentialModelIDs map[string]string
}

func (u *webAuthnUser) WebAuthnID() []byte {
	return []byte(u.model.ID)
}

func (u *webAuthnUser) WebAuthnName() string {
	return u.model.Username
}

func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u.model.DisplayName != nil && strings.TrimSpace(*u.model.DisplayName) != "" {
		return *u.model.DisplayName
	}
	return u.model.Username
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

func (s *passkeyService) loadWebAuthnUserInternal(ctx context.Context, userID string) (*webAuthnUser, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrPasskeyTransaction
	}

	var user models.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyTransaction
		}
		return nil, errors.WrapIf(err, "failed to load passkey user")
	}

	var rows []models.Passkey
	if err := s.db.WithContext(ctx).Where("user_id = ? AND rp_id = ?", user.ID, s.rpID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to load passkeys")
	}

	credentials := make([]webauthn.Credential, len(rows))
	credentialModelIDs := make(map[string]string, len(rows))
	for i := range rows {
		credentials[i] = credentialFromModelInternal(rows[i])
		credentialModelIDs[string(rows[i].CredentialID)] = rows[i].ID
	}

	return &webAuthnUser{model: user, credentials: credentials, credentialModelIDs: credentialModelIDs}, nil
}

func credentialFromModelInternal(row models.Passkey) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(row.Transports))
	for i, transport := range row.Transports {
		transports[i] = protocol.AuthenticatorTransport(transport)
	}

	flags := webauthn.NewCredentialFlags(0)
	flags.UserPresent = true
	flags.UserVerified = true
	flags.BackupEligible = row.BackupEligible
	flags.BackupState = row.BackupState

	return webauthn.Credential{
		ID:                append([]byte(nil), row.CredentialID...),
		PublicKey:         append([]byte(nil), row.PublicKey...),
		AttestationType:   row.AttestationType,
		AttestationFormat: row.AttestationFormat,
		Transport:         transports,
		Flags:             flags,
		Authenticator: webauthn.Authenticator{
			AAGUID:       append([]byte(nil), row.AAGUID...),
			SignCount:    row.SignCount,
			CloneWarning: row.CloneWarning,
			Attachment:   protocol.AuthenticatorAttachment(row.AuthenticatorAttachment),
		},
		Attestation: webauthn.CredentialAttestation{
			ClientDataJSON:     append([]byte(nil), row.AttestationClientDataJSON...),
			ClientDataHash:     append([]byte(nil), row.AttestationClientDataHash...),
			AuthenticatorData:  append([]byte(nil), row.AttestationAuthenticatorData...),
			PublicKeyAlgorithm: row.AttestationPublicKeyAlgorithm,
			Object:             append([]byte(nil), row.AttestationObject...),
		},
	}
}

func (s *passkeyService) BeginPasskeyLogin(ctx context.Context) (*PasskeyChallenge, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremoniesInternal(ctx); err != nil {
		return nil, err
	}

	assertion, session, err := s.webAuthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, errors.WrapIf(err, "failed to begin passkey login")
	}

	ceremony, err := s.createCeremonyInternal(ctx, passkeyCeremonyPurposeLogin, nil, nil, nil, session)
	if err != nil {
		return nil, err
	}

	return &PasskeyChallenge{
		CeremonyID: ceremony.ID,
		Options:    assertion.Response,
		ExpiresAt:  ceremony.ExpiresAt,
	}, nil
}

func (s *passkeyService) BeginMFAAuthentication(ctx context.Context, userID string, meta auth.SessionMeta, source string) (*auth.MFAChallenge, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremoniesInternal(ctx); err != nil {
		return nil, err
	}
	adapter, err := s.loadWebAuthnUserInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, ErrPasskeyNoCredential
	}

	transaction := newAuthTransactionInternal(userID, authTransactionKindMFA, source, meta, nil, passkeyStepUpTTL)
	if err := s.db.WithContext(ctx).Create(transaction).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create MFA transaction")
	}
	challenge, err := s.beginMFAForTransactionInternal(ctx, transaction)
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(transaction).Error
		return nil, err
	}
	return challenge, nil
}

// BeginMFAForTransaction starts a passkey assertion for an already-created
// pending MFA transaction. This is useful to clients that separate primary
// authentication from challenge retrieval.
func (s *passkeyService) BeginMFAForTransaction(ctx context.Context, transactionID string) (*auth.MFAChallenge, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremoniesInternal(ctx); err != nil {
		return nil, err
	}
	transaction, err := s.loadPendingTransactionInternal(ctx, transactionID, authTransactionKindMFA)
	if err != nil {
		return nil, err
	}
	return s.beginMFAForTransactionInternal(ctx, transaction)
}

func (s *passkeyService) beginMFAForTransactionInternal(ctx context.Context, transaction *models.AuthTransaction) (*auth.MFAChallenge, error) {
	if transaction == nil {
		return nil, ErrPasskeyTransaction
	}
	adapter, err := s.loadWebAuthnUserInternal(ctx, transaction.UserID)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, ErrPasskeyNoCredential
	}
	// A transaction has one active assertion at a time. Replacing an older
	// pending ceremony prevents a client from accumulating reusable challenges.
	if err := s.db.WithContext(ctx).
		Where("auth_transaction_id = ? AND purpose = ? AND consumed_at IS NULL", transaction.ID, passkeyCeremonyPurposeMFA).
		Delete(&models.PasskeyCeremony{}).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to replace MFA ceremony")
	}

	assertion, session, err := s.webAuthn.BeginLogin(adapter, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, errors.WrapIf(err, "failed to begin MFA passkey ceremony")
	}

	transactionID := transaction.ID
	userIDPointer := transaction.UserID
	ceremony, err := s.createCeremonyInternal(ctx, passkeyCeremonyPurposeMFA, &userIDPointer, nil, &transactionID, session)
	if err != nil {
		return nil, err
	}

	return &auth.MFAChallenge{
		TransactionID: transaction.ID,
		Method:        models.PasskeyMFAMethod,
		Options:       assertion.Response,
		ExpiresAt:     ceremony.ExpiresAt,
	}, nil
}

func (s *passkeyService) BeginStepUp(ctx context.Context, userID, sessionID string, meta auth.SessionMeta) (*PasskeyChallenge, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremoniesInternal(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureActiveSessionInternal(ctx, userID, sessionID); err != nil {
		return nil, err
	}

	adapter, err := s.loadWebAuthnUserInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, ErrPasskeyNoCredential
	}

	transaction := newAuthTransactionInternal(userID, authTransactionKindStepUp, models.UserSessionSourceLocal, meta, &sessionID, passkeyStepUpTTL)
	if err := s.db.WithContext(ctx).Create(transaction).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create step-up transaction")
	}

	assertion, session, err := s.webAuthn.BeginLogin(adapter, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(transaction).Error
		return nil, errors.WrapIf(err, "failed to begin step-up passkey ceremony")
	}

	transactionID := transaction.ID
	userIDPointer := userID
	ceremony, err := s.createCeremonyInternal(ctx, passkeyCeremonyPurposeStepUp, &userIDPointer, &sessionID, &transactionID, session)
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(transaction).Error
		return nil, err
	}

	return &PasskeyChallenge{
		CeremonyID:    ceremony.ID,
		TransactionID: transaction.ID,
		Options:       assertion.Response,
		ExpiresAt:     ceremony.ExpiresAt,
	}, nil
}

func (s *passkeyService) BeginRegistration(ctx context.Context, userID, sessionID, stepUpToken string) (*PasskeyChallenge, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremoniesInternal(ctx); err != nil {
		return nil, err
	}
	if err := s.authorizeManagementInternal(ctx, userID, sessionID, stepUpToken, true); err != nil {
		return nil, err
	}

	adapter, err := s.loadWebAuthnUserInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	exclusions := webauthn.Credentials(adapter.credentials).CredentialDescriptors()
	creation, session, err := s.webAuthn.BeginRegistration(
		adapter,
		webauthn.WithExclusions(exclusions),
		webauthn.WithExtensions(map[string]any{"credProps": true}),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to begin passkey registration")
	}

	userIDPointer := userID
	ceremony, err := s.createCeremonyInternal(ctx, passkeyCeremonyPurposeRegistration, &userIDPointer, &sessionID, nil, session)
	if err != nil {
		return nil, err
	}

	return &PasskeyChallenge{CeremonyID: ceremony.ID, Options: creation.Response, ExpiresAt: ceremony.ExpiresAt}, nil
}

func (s *passkeyService) FinishRegistration(ctx context.Context, userID, sessionID, ceremonyID string, payload []byte, name string) (*PasskeySummary, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.ensureActiveSessionInternal(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxPasskeyPayloadBytes {
		return nil, ErrPasskeyResponse
	}

	ceremony, err := s.consumeCeremonyInternal(ctx, ceremonyID, passkeyCeremonyPurposeRegistration)
	if err != nil {
		return nil, err
	}
	if ceremony.UserID == nil || *ceremony.UserID != userID || ceremony.SessionID == nil || *ceremony.SessionID != sessionID {
		return nil, ErrPasskeyCeremony
	}

	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(ceremony.SessionData), &session); err != nil {
		return nil, ErrPasskeyCeremony
	}
	adapter, err := s.loadWebAuthnUserInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload)
	if err != nil {
		return nil, ErrPasskeyResponse
	}
	credential, err := s.webAuthn.CreateCredential(adapter, session, parsed)
	if err != nil {
		return nil, ErrPasskeyResponse
	}

	if strings.TrimSpace(name) == "" {
		name = defaultPasskeyNameInternal(credential.Authenticator.AAGUID)
	}
	name, err = normalizePasskeyNameInternal(name)
	if err != nil {
		return nil, err
	}
	row, err := s.persistCredentialInternal(ctx, userID, credential, name)
	if err != nil {
		return nil, err
	}
	summary := passkeySummaryInternal(*row)
	return &summary, nil
}

func (s *passkeyService) FinishPasskeyLogin(ctx context.Context, ceremonyID string, payload []byte) (*models.User, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxPasskeyPayloadBytes {
		return nil, ErrPasskeyResponse
	}

	ceremony, err := s.consumeCeremonyInternal(ctx, ceremonyID, passkeyCeremonyPurposeLogin)
	if err != nil {
		return nil, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(ceremony.SessionData), &session); err != nil {
		return nil, ErrPasskeyCeremony
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		slog.WarnContext(ctx, "passkey login validation failed", "stage", "parse", "error", err)
		return nil, ErrPasskeyResponse
	}

	var resolved *webAuthnUser
	user, credential, err := s.webAuthn.ValidatePasskeyLogin(func(_ []byte, userHandle []byte) (webauthn.User, error) {
		adapter, lookupErr := s.loadWebAuthnUserInternal(ctx, string(userHandle))
		if lookupErr != nil {
			return nil, lookupErr
		}
		resolved = adapter
		return adapter, nil
	}, session, parsed)
	if err != nil || user == nil || credential == nil || resolved == nil {
		slog.WarnContext(ctx, "passkey login validation failed", "stage", "assertion", "error", err)
		return nil, ErrPasskeyResponse
	}

	if err := s.updateCredentialAfterAssertionInternal(ctx, resolved, credential); err != nil {
		return nil, err
	}
	return &resolved.model, nil
}

func (s *passkeyService) FinishMobilePasskeyLogin(ctx context.Context, ceremonyID string, payload []byte, codeChallenge string) (*auth.MobilePasskeyCompletion, error) {
	codeChallenge, err := normalizeMobilePasskeyCodeChallengeInternal(codeChallenge)
	if err != nil {
		return nil, err
	}
	user, err := s.FinishPasskeyLogin(ctx, ceremonyID, payload)
	if err != nil {
		return nil, err
	}

	transaction := newAuthTransactionInternal(user.ID, authTransactionKindMobilePasskey, models.UserSessionSourcePasskey, auth.SessionMeta{}, nil, passkeyStepUpTTL)
	transaction.SecretHash = &codeChallenge
	if err := s.db.WithContext(ctx).Create(transaction).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create mobile passkey transaction")
	}
	return &auth.MobilePasskeyCompletion{TransactionID: transaction.ID, ExpiresAt: transaction.ExpiresAt}, nil
}

func (s *passkeyService) ExchangeMobilePasskeyLogin(ctx context.Context, transactionID, codeVerifier string) (*models.User, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	codeChallenge, err := mobilePasskeyCodeChallengeInternal(codeVerifier)
	if err != nil {
		return nil, err
	}

	var transaction models.AuthTransaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND kind = ? AND status = ? AND secret_hash = ? AND expires_at > ?", transactionID, authTransactionKindMobilePasskey, authTransactionPending, codeChallenge, time.Now()).First(&transaction).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPasskeyTransaction
			}
			return errors.WrapIf(err, "failed to load mobile passkey transaction")
		}

		now := time.Now()
		result := tx.Model(&models.AuthTransaction{}).
			Where("id = ? AND status = ? AND expires_at > ?", transaction.ID, authTransactionPending, now).
			Updates(map[string]any{"status": authTransactionCompleted, "completed_at": now, "updated_at": now})
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to complete mobile passkey transaction")
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyTransaction
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.loadUserModelInternal(ctx, transaction.UserID)
}

func (s *passkeyService) FinishMFA(ctx context.Context, transactionID string, payload []byte) (*AuthenticationCompletion, error) {
	transaction, err := s.loadPendingTransactionInternal(ctx, transactionID, authTransactionKindMFA)
	if err != nil {
		return nil, err
	}
	user, err := s.finishKnownUserAssertionInternal(ctx, transaction, passkeyCeremonyPurposeMFA, payload)
	if err != nil {
		return nil, err
	}
	if err := s.completeTransactionInternal(ctx, transaction.ID); err != nil {
		return nil, err
	}

	now := time.Now()
	return &AuthenticationCompletion{
		User:   user,
		Meta:   transactionSessionMetaInternal(transaction, models.PasskeyMFAMethod, &now),
		Source: transaction.Source,
	}, nil
}

func (s *passkeyService) FinishRecoveryCode(ctx context.Context, transactionID, code string) (*AuthenticationCompletion, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if normalized == "" {
		return nil, ErrPasskeyRecoveryCode
	}

	hash := hashSecretInternal(normalized)
	var transaction models.AuthTransaction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND kind = ? AND status = ? AND expires_at > ?", transactionID, authTransactionKindMFA, authTransactionPending, time.Now()).First(&transaction).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPasskeyTransaction
			}
			return errors.WrapIf(err, "failed to lock MFA transaction")
		}
		var recoveryCodes []models.PasskeyRecoveryCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND used_at IS NULL", transaction.UserID).Find(&recoveryCodes).Error; err != nil {
			return errors.WrapIf(err, "failed to load recovery codes")
		}

		var matched *models.PasskeyRecoveryCode
		for i := range recoveryCodes {
			if subtle.ConstantTimeCompare([]byte(recoveryCodes[i].CodeHash), []byte(hash)) == 1 {
				matched = &recoveryCodes[i]
				break
			}
		}
		if matched == nil {
			return ErrPasskeyRecoveryCode
		}
		now := time.Now()
		result := tx.Model(&models.PasskeyRecoveryCode{}).
			Where("id = ? AND used_at IS NULL", matched.ID).
			Updates(map[string]any{"used_at": now, "updated_at": now})
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to consume recovery code")
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyRecoveryCode
		}
		now = time.Now()
		result = tx.Model(&models.AuthTransaction{}).
			Where("id = ? AND status = ? AND expires_at > ?", transaction.ID, authTransactionPending, now).
			Updates(map[string]any{"status": authTransactionCompleted, "completed_at": now, "updated_at": now})
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to complete MFA transaction")
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyTransaction
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	user, err := s.loadUserModelInternal(ctx, transaction.UserID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &AuthenticationCompletion{
		User:   user,
		Meta:   transactionSessionMetaInternal(&transaction, models.RecoveryCodeMFAMethod, &now),
		Source: transaction.Source,
	}, nil
}

func (s *passkeyService) FinishStepUp(ctx context.Context, transactionID, sessionID string, payload []byte) (*StepUpGrant, error) {
	transaction, err := s.loadPendingTransactionInternal(ctx, transactionID, authTransactionKindStepUp)
	if err != nil {
		return nil, err
	}
	if transaction.SessionID == nil || *transaction.SessionID != sessionID {
		return nil, ErrPasskeyTransaction
	}
	if err := s.ensureActiveSessionInternal(ctx, transaction.UserID, sessionID); err != nil {
		return nil, err
	}
	if _, err := s.finishKnownUserAssertionInternal(ctx, transaction, passkeyCeremonyPurposeStepUp, payload); err != nil {
		return nil, err
	}
	return s.issueStepUpGrantInternal(ctx, transaction.ID)
}

func (s *passkeyService) CreatePasswordStepUpGrant(ctx context.Context, userID, sessionID string) (*StepUpGrant, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.ensureActiveSessionInternal(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	transaction := newAuthTransactionInternal(userID, authTransactionKindStepUp, models.UserSessionSourceLocal, auth.SessionMeta{}, &sessionID, passkeyStepUpTTL)
	if err := s.db.WithContext(ctx).Create(transaction).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create password step-up transaction")
	}
	return s.issueStepUpGrantInternal(ctx, transaction.ID)
}

// VerifyStepUpToken accepts a grant repeatedly until it expires, so a single
// re-authentication covers a short run of management actions instead of one.
// The grant stays bound to the issuing user and session.
func (s *passkeyService) VerifyStepUpToken(ctx context.Context, userID, sessionID, token string) error {
	if err := s.readyInternal(); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return ErrPasskeyStepUpRequired
	}
	if err := s.ensureActiveSessionInternal(ctx, userID, sessionID); err != nil {
		return err
	}
	hash := hashSecretInternal(token)
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.AuthTransaction{}).
		Where("user_id = ? AND session_id = ? AND kind = ? AND status = ? AND secret_hash = ? AND expires_at > ?", userID, sessionID, authTransactionKindStepUp, authTransactionCompleted, hash, time.Now()).
		Count(&count).Error; err != nil {
		return errors.WrapIf(err, "failed to verify step-up grant")
	}
	if count != 1 {
		return ErrPasskeyStepUpRequired
	}
	return nil
}

func (s *passkeyService) ListPasskeys(ctx context.Context, userID string) ([]PasskeySummary, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	var rows []models.Passkey
	if err := s.db.WithContext(ctx).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list passkeys")
	}
	result := make([]PasskeySummary, len(rows))
	for i := range rows {
		result[i] = passkeySummaryInternal(rows[i])
	}
	return result, nil
}

func (s *passkeyService) GetCapabilities(ctx context.Context, userID string, oidcEnabled bool) (*PasskeyCapabilities, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	user, err := s.loadUserModelInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	passkeyCount, err := s.countPasskeysInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	hasPassword := strings.TrimSpace(user.PasswordHash) != ""
	hasOIDC := oidcEnabled && user.OidcSubjectId != nil && strings.TrimSpace(*user.OidcSubjectId) != ""
	return &PasskeyCapabilities{
		PasskeyMFAEnabled:          user.PasskeyMFAEnabled,
		PasskeyCount:               passkeyCount,
		HasLocalPassword:           hasPassword,
		HasOIDCFallback:            hasOIDC,
		CanEnrollWithActiveSession: passkeyCount == 0,
		CanDeleteLastPasskey:       passkeyCount == 1 && !user.PasskeyMFAEnabled && (hasPassword || hasOIDC),
		RequiresStepUp:             passkeyCount > 0,
	}, nil
}

func (s *passkeyService) RenamePasskey(ctx context.Context, userID, passkeyID, name, sessionID, stepUpToken string) (*PasskeySummary, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.authorizeManagementInternal(ctx, userID, sessionID, stepUpToken, false); err != nil {
		return nil, err
	}
	name, err := normalizePasskeyNameInternal(name)
	if err != nil {
		return nil, err
	}
	var row models.Passkey
	result := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND rp_id = ?", passkeyID, userID, s.rpID).First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrPasskeyNotFound
	}
	if result.Error != nil {
		return nil, errors.WrapIf(result.Error, "failed to load passkey")
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"name": name, "updated_at": now}).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to rename passkey")
	}
	row.Name = name
	row.UpdatedAt = &now
	summary := passkeySummaryInternal(row)
	return &summary, nil
}

func (s *passkeyService) DeletePasskey(ctx context.Context, userID, passkeyID, sessionID, stepUpToken string, oidcFallbackAllowed bool) error {
	if err := s.readyInternal(); err != nil {
		return err
	}
	if err := s.authorizeManagementInternal(ctx, userID, sessionID, stepUpToken, false); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.WrapIf(err, "failed to lock user for passkey deletion")
		}
		var count int64
		if err := tx.Model(&models.Passkey{}).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Count(&count).Error; err != nil {
			return errors.WrapIf(err, "failed to count passkeys")
		}
		if user.PasskeyMFAEnabled && count <= 1 {
			return ErrPasskeyMFAEnabled
		}
		if count <= 1 {
			hasPassword := strings.TrimSpace(user.PasswordHash) != ""
			hasOIDC := oidcFallbackAllowed && user.OidcSubjectId != nil && strings.TrimSpace(*user.OidcSubjectId) != ""
			if !hasPassword && !hasOIDC {
				return ErrPasskeyLastCredential
			}
		}
		result := tx.Where("id = ? AND user_id = ? AND rp_id = ?", passkeyID, userID, s.rpID).Delete(&models.Passkey{})
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to delete passkey")
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyNotFound
		}
		return nil
	})
}

func (s *passkeyService) GetMFAStatus(ctx context.Context, userID string) (*MFAStatus, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	user, err := s.loadUserModelInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	passkeyCount, err := s.countPasskeysInternal(ctx, userID)
	if err != nil {
		return nil, err
	}
	var recoveryCodes int64
	if err := s.db.WithContext(ctx).Model(&models.PasskeyRecoveryCode{}).Where("user_id = ? AND used_at IS NULL", userID).Count(&recoveryCodes).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to count recovery codes")
	}
	return &MFAStatus{Enabled: user.PasskeyMFAEnabled, PasskeyCount: passkeyCount, RecoveryCodesRemaining: int(recoveryCodes)}, nil
}

func (s *passkeyService) EnableMFA(ctx context.Context, userID, sessionID, stepUpToken string) ([]string, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken); err != nil {
		return nil, err
	}
	codes, rows, err := generateRecoveryCodeRowsInternal(userID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.WrapIf(err, "failed to lock user for MFA enable")
		}
		var count int64
		if err := tx.Model(&models.Passkey{}).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Count(&count).Error; err != nil {
			return errors.WrapIf(err, "failed to count passkeys for MFA enable")
		}
		if count == 0 {
			return ErrPasskeyNoCredential
		}
		if user.PasskeyMFAEnabled {
			return ErrPasskeyMFAAlreadyEnabled
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Update("passkey_mfa_enabled", true).Error; err != nil {
			return errors.WrapIf(err, "failed to enable passkey MFA")
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.PasskeyRecoveryCode{}).Error; err != nil {
			return errors.WrapIf(err, "failed to replace recovery codes")
		}
		if err := tx.Create(&rows).Error; err != nil {
			return errors.WrapIf(err, "failed to create recovery codes")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *passkeyService) DisableMFA(ctx context.Context, userID, sessionID, stepUpToken string) error {
	if err := s.readyInternal(); err != nil {
		return err
	}
	if err := s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken); err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.WrapIf(err, "failed to lock user for MFA disable")
		}
		if !user.PasskeyMFAEnabled {
			return ErrPasskeyMFANotEnabled
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Update("passkey_mfa_enabled", false).Error; err != nil {
			return errors.WrapIf(err, "failed to disable passkey MFA")
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.PasskeyRecoveryCode{}).Error; err != nil {
			return errors.WrapIf(err, "failed to delete recovery codes")
		}
		now := time.Now()
		query := tx.Model(&models.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", userID)
		if strings.TrimSpace(sessionID) != "" {
			query = query.Where("id <> ?", sessionID)
		}
		if err := query.Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
			return errors.WrapIf(err, "failed to revoke sessions after MFA disable")
		}
		return nil
	})
	return err
}

func (s *passkeyService) RegenerateRecoveryCodes(ctx context.Context, userID, sessionID, stepUpToken string) ([]string, error) {
	if err := s.readyInternal(); err != nil {
		return nil, err
	}
	if err := s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken); err != nil {
		return nil, err
	}
	codes, rows, err := generateRecoveryCodeRowsInternal(userID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.WrapIf(err, "failed to load user for recovery code regeneration")
		}
		if !user.PasskeyMFAEnabled {
			return ErrPasskeyMFANotEnabled
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.PasskeyRecoveryCode{}).Error; err != nil {
			return errors.WrapIf(err, "failed to replace recovery codes")
		}
		return errors.WrapIf(tx.Create(&rows).Error, "failed to create recovery codes")
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// ResetMFAForUser is intentionally separate from self-service operations. It
// is used only by the explicitly gated embedded admin recovery command.
func (s *passkeyService) ResetMFAForUser(ctx context.Context, userID string) error {
	if err := s.readyInternal(); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPasskeyTransaction
			}
			return errors.WrapIf(err, "failed to load user for MFA reset")
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Update("passkey_mfa_enabled", false).Error; err != nil {
			return errors.WrapIf(err, "failed to disable passkey MFA")
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.PasskeyRecoveryCode{}).Error; err != nil {
			return errors.WrapIf(err, "failed to delete recovery codes")
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.PasskeyCeremony{}).Error; err != nil {
			return errors.WrapIf(err, "failed to delete passkey ceremonies")
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.AuthTransaction{}).Error; err != nil {
			return errors.WrapIf(err, "failed to delete authentication transactions")
		}
		now := time.Now()
		if err := tx.Model(&models.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", userID).Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
			return errors.WrapIf(err, "failed to revoke sessions after MFA reset")
		}
		return nil
	})
}

func (s *passkeyService) createCeremonyInternal(ctx context.Context, purpose string, userID, sessionID, transactionID *string, session *webauthn.SessionData) (*models.PasskeyCeremony, error) {
	if session == nil {
		return nil, ErrPasskeyCeremony
	}
	serialized, err := json.Marshal(session)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to serialize WebAuthn session")
	}
	expiresAt := session.Expires
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(passkeyCeremonyTTL)
	}
	ceremony := &models.PasskeyCeremony{
		BaseModel:         models.BaseModel{ID: uuid.NewString()},
		Purpose:           purpose,
		UserID:            userID,
		SessionID:         sessionID,
		AuthTransactionID: transactionID,
		RPID:              s.rpID,
		SessionData:       string(serialized),
		ExpiresAt:         expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(ceremony).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to store passkey ceremony")
	}
	return ceremony, nil
}

func (s *passkeyService) sweepCeremoniesInternal(ctx context.Context) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).
		Where("expires_at <= ? OR consumed_at IS NOT NULL", now).
		Delete(&models.PasskeyCeremony{}).Error; err != nil {
		return errors.WrapIf(err, "failed to sweep passkey ceremonies")
	}
	return nil
}

func (s *passkeyService) consumeCeremonyInternal(ctx context.Context, ceremonyID, purpose string) (*models.PasskeyCeremony, error) {
	if strings.TrimSpace(ceremonyID) == "" {
		return nil, ErrPasskeyCeremony
	}
	var ceremony models.PasskeyCeremony
	now := time.Now()
	if err := s.db.WithContext(ctx).Where("id = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?", ceremonyID, purpose, now).First(&ceremony).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyCeremony
		}
		return nil, errors.WrapIf(err, "failed to load passkey ceremony")
	}
	result := s.db.WithContext(ctx).Model(&models.PasskeyCeremony{}).
		Where("id = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?", ceremonyID, purpose, now).
		Updates(map[string]any{"consumed_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, errors.WrapIf(result.Error, "failed to consume passkey ceremony")
	}
	if result.RowsAffected != 1 {
		return nil, ErrPasskeyCeremony
	}
	ceremony.ConsumedAt = &now
	return &ceremony, nil
}

func (s *passkeyService) loadPendingTransactionInternal(ctx context.Context, transactionID, kind string) (*models.AuthTransaction, error) {
	if strings.TrimSpace(transactionID) == "" {
		return nil, ErrPasskeyTransaction
	}
	var transaction models.AuthTransaction
	if err := s.db.WithContext(ctx).Where("id = ? AND kind = ? AND status = ? AND expires_at > ?", transactionID, kind, authTransactionPending, time.Now()).First(&transaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyTransaction
		}
		return nil, errors.WrapIf(err, "failed to load authentication transaction")
	}
	return &transaction, nil
}

func (s *passkeyService) finishKnownUserAssertionInternal(ctx context.Context, transaction *models.AuthTransaction, purpose string, payload []byte) (*models.User, error) {
	if transaction == nil || len(payload) == 0 || len(payload) > maxPasskeyPayloadBytes {
		return nil, ErrPasskeyResponse
	}
	ceremony, err := s.consumeCeremonyInternal(ctx, ceremonyIDForTransactionInternal(ctx, s.db, transaction.ID, purpose), purpose)
	if err != nil {
		return nil, err
	}
	if ceremony.AuthTransactionID == nil || *ceremony.AuthTransactionID != transaction.ID || ceremony.UserID == nil || *ceremony.UserID != transaction.UserID {
		return nil, ErrPasskeyCeremony
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(ceremony.SessionData), &session); err != nil {
		return nil, ErrPasskeyCeremony
	}
	adapter, err := s.loadWebAuthnUserInternal(ctx, transaction.UserID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		return nil, ErrPasskeyResponse
	}
	credential, err := s.webAuthn.ValidateLogin(adapter, session, parsed)
	if err != nil || credential == nil {
		return nil, ErrPasskeyResponse
	}
	if err := s.updateCredentialAfterAssertionInternal(ctx, adapter, credential); err != nil {
		return nil, err
	}
	return &adapter.model, nil
}

func ceremonyIDForTransactionInternal(ctx context.Context, db *database.DB, transactionID, purpose string) string {
	var ceremony models.PasskeyCeremony
	if db == nil || db.WithContext(ctx).Where("auth_transaction_id = ? AND purpose = ? AND consumed_at IS NULL", transactionID, purpose).Order("created_at DESC").First(&ceremony).Error != nil {
		return ""
	}
	return ceremony.ID
}

func (s *passkeyService) completeTransactionInternal(ctx context.Context, transactionID string) error {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&models.AuthTransaction{}).
		Where("id = ? AND status = ? AND expires_at > ?", transactionID, authTransactionPending, now).
		Updates(map[string]any{"status": authTransactionCompleted, "completed_at": now, "updated_at": now})
	if result.Error != nil {
		return errors.WrapIf(result.Error, "failed to complete authentication transaction")
	}
	if result.RowsAffected != 1 {
		return ErrPasskeyTransaction
	}
	return nil
}

func (s *passkeyService) issueStepUpGrantInternal(ctx context.Context, transactionID string) (*StepUpGrant, error) {
	token, err := randomSecretInternal()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create step-up grant")
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&models.AuthTransaction{}).
		Where("id = ? AND kind = ? AND status = ? AND expires_at > ?", transactionID, authTransactionKindStepUp, authTransactionPending, now).
		Updates(map[string]any{
			"status":       authTransactionCompleted,
			"secret_hash":  hashSecretInternal(token),
			"completed_at": now,
			"updated_at":   now,
		})
	if result.Error != nil {
		return nil, errors.WrapIf(result.Error, "failed to store step-up grant")
	}
	if result.RowsAffected != 1 {
		return nil, ErrPasskeyTransaction
	}
	var transaction models.AuthTransaction
	if err := s.db.WithContext(ctx).Where("id = ?", transactionID).First(&transaction).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to load step-up grant")
	}
	return &StepUpGrant{Token: token, ExpiresAt: transaction.ExpiresAt}, nil
}

func (s *passkeyService) authorizeManagementInternal(ctx context.Context, userID, sessionID, stepUpToken string, allowFirstEnrollment bool) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrPasskeyStepUpRequired
	}
	count, err := s.countPasskeysInternal(ctx, userID)
	if err != nil {
		return err
	}
	if allowFirstEnrollment && count == 0 {
		return s.ensureActiveSessionInternal(ctx, userID, sessionID)
	}
	return s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken)
}

func (s *passkeyService) loadUserModelInternal(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyTransaction
		}
		return nil, errors.WrapIf(err, "failed to load user")
	}
	return &user, nil
}

func (s *passkeyService) ensureActiveSessionInternal(ctx context.Context, userID, sessionID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return ErrPasskeyStepUpRequired
	}
	var session models.UserSession
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", sessionID, userID, time.Now()).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPasskeyStepUpRequired
		}
		return errors.WrapIf(err, "failed to validate active session")
	}
	return nil
}

func (s *passkeyService) countPasskeysInternal(ctx context.Context, userID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Passkey{}).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Count(&count).Error; err != nil {
		return 0, errors.WrapIf(err, "failed to count passkeys")
	}
	return int(count), nil
}

func (s *passkeyService) persistCredentialInternal(ctx context.Context, userID string, credential *webauthn.Credential, name string) (*models.Passkey, error) {
	if credential == nil || len(credential.ID) == 0 || len(credential.PublicKey) == 0 {
		return nil, ErrPasskeyResponse
	}
	var existing models.Passkey
	result := s.db.WithContext(ctx).Where("rp_id = ? AND credential_id = ?", s.rpID, credential.ID).First(&existing)
	if result.Error == nil {
		return nil, ErrPasskeyExists
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, errors.WrapIf(result.Error, "failed to check existing passkey")
	}
	transports := make(models.StringSlice, len(credential.Transport))
	for i, transport := range credential.Transport {
		transports[i] = string(transport)
	}
	row := &models.Passkey{
		BaseModel:                     models.BaseModel{ID: uuid.NewString()},
		UserID:                        userID,
		RPID:                          s.rpID,
		CredentialID:                  append([]byte(nil), credential.ID...),
		PublicKey:                     append([]byte(nil), credential.PublicKey...),
		AttestationType:               credential.AttestationType,
		AttestationFormat:             credential.AttestationFormat,
		Transports:                    transports,
		AAGUID:                        append([]byte(nil), credential.Authenticator.AAGUID...),
		SignCount:                     credential.Authenticator.SignCount,
		BackupEligible:                credential.Flags.BackupEligible,
		BackupState:                   credential.Flags.BackupState,
		CloneWarning:                  credential.Authenticator.CloneWarning,
		AuthenticatorAttachment:       string(credential.Authenticator.Attachment),
		AttestationClientDataJSON:     append([]byte(nil), credential.Attestation.ClientDataJSON...),
		AttestationClientDataHash:     append([]byte(nil), credential.Attestation.ClientDataHash...),
		AttestationAuthenticatorData:  append([]byte(nil), credential.Attestation.AuthenticatorData...),
		AttestationPublicKeyAlgorithm: credential.Attestation.PublicKeyAlgorithm,
		AttestationObject:             append([]byte(nil), credential.Attestation.Object...),
		Name:                          name,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrPasskeyExists
		}
		return nil, errors.WrapIf(err, "failed to store passkey")
	}
	return row, nil
}

func (s *passkeyService) updateCredentialAfterAssertionInternal(ctx context.Context, adapter *webAuthnUser, credential *webauthn.Credential) error {
	if adapter == nil || credential == nil || len(credential.ID) == 0 {
		return ErrPasskeyResponse
	}
	credentialModelID, ok := adapter.credentialModelIDs[string(credential.ID)]
	if !ok || credentialModelID == "" {
		return ErrPasskeyResponse
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&models.Passkey{}).
		Where("id = ? AND user_id = ? AND rp_id = ?", credentialModelID, adapter.model.ID, s.rpID).
		Updates(map[string]any{
			"sign_count":      credential.Authenticator.SignCount,
			"backup_eligible": credential.Flags.BackupEligible,
			"backup_state":    credential.Flags.BackupState,
			"clone_warning":   credential.Authenticator.CloneWarning,
			"last_used_at":    now,
			"updated_at":      now,
		})
	if result.Error != nil {
		return errors.WrapIf(result.Error, "failed to update passkey counter")
	}
	if result.RowsAffected != 1 {
		return ErrPasskeyResponse
	}
	return nil
}

func newAuthTransactionInternal(userID, kind, source string, meta auth.SessionMeta, sessionID *string, ttl time.Duration) *models.AuthTransaction {
	if strings.TrimSpace(source) == "" {
		source = models.UserSessionSourceLocal
	}
	return &models.AuthTransaction{
		BaseModel: models.BaseModel{ID: uuid.NewString()},
		Kind:      kind,
		UserID:    userID,
		SessionID: sessionID,
		Source:    source,
		UserAgent: optionalStringInternal(meta.UserAgent),
		IPAddress: optionalStringInternal(meta.IPAddress),
		Status:    authTransactionPending,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func transactionSessionMetaInternal(transaction *models.AuthTransaction, method string, verifiedAt *time.Time) auth.SessionMeta {
	meta := auth.SessionMeta{Source: transaction.Source, MFAMethod: method, MFAVerifiedAt: verifiedAt}
	if transaction.UserAgent != nil {
		meta.UserAgent = *transaction.UserAgent
	}
	if transaction.IPAddress != nil {
		meta.IPAddress = *transaction.IPAddress
	}
	return meta
}

func passkeySummaryInternal(row models.Passkey) PasskeySummary {
	transports := append([]string(nil), row.Transports...)
	return PasskeySummary{
		ID:                      row.ID,
		Name:                    row.Name,
		RPID:                    row.RPID,
		AAGUID:                  formatAAGUIDInternal(row.AAGUID),
		Transports:              transports,
		BackupEligible:          row.BackupEligible,
		BackupState:             row.BackupState,
		CloneWarning:            row.CloneWarning,
		AuthenticatorAttachment: row.AuthenticatorAttachment,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
		LastUsedAt:              row.LastUsedAt,
	}
}

func normalizePasskeyNameInternal(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrPasskeyName
	}
	if utf8.RuneCountInString(name) > maxPasskeyNameRunes {
		return "", errors.WrapIf(ErrPasskeyName, "passkey name is too long")
	}
	return name, nil
}

func defaultPasskeyNameInternal(aaguid []byte) string {
	formatted := formatAAGUIDInternal(aaguid)
	if formatted != "" {
		authenticatorNamesOnce.Do(loadAuthenticatorNamesInternal)
		if name := strings.TrimSpace(authenticatorNames[formatted]); name != "" {
			return name + " Passkey"
		}
	}
	return "New Passkey"
}

func formatAAGUIDInternal(aaguid []byte) string {
	if len(aaguid) == 0 {
		return ""
	}
	if len(aaguid) == 16 {
		value, err := uuid.FromBytes(aaguid)
		if err == nil {
			return value.String()
		}
	}
	return hex.EncodeToString(aaguid)
}

func loadAuthenticatorNamesInternal() {
	// This community catalog is display-only and must never influence
	// authentication decisions. Source: pocket-id/passkey-aaguids.
	authenticatorNames = map[string]string{}
	data, err := resources.FS.ReadFile("aaguids.json")
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &authenticatorNames); err != nil {
		authenticatorNames = map[string]string{}
	}
}

func generateRecoveryCodeRowsInternal(userID string) ([]string, []models.PasskeyRecoveryCode, error) {
	codes := make([]string, recoveryCodeCount)
	rows := make([]models.PasskeyRecoveryCode, recoveryCodeCount)
	for i := range codes {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, errors.WrapIf(err, "failed to generate recovery code")
		}
		encoded := strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), "=")
		codes[i] = groupRecoveryCodeInternal(encoded)
		hash := hashSecretInternal(strings.ToUpper(strings.ReplaceAll(codes[i], "-", "")))
		rows[i] = models.PasskeyRecoveryCode{BaseModel: models.BaseModel{ID: uuid.NewString()}, UserID: userID, CodeHash: hash}
	}
	return codes, rows, nil
}

func groupRecoveryCodeInternal(code string) string {
	var groups []string
	for len(code) > 4 {
		groups = append(groups, code[:4])
		code = code[4:]
	}
	if code != "" {
		groups = append(groups, code)
	}
	return strings.Join(groups, "-")
}

func hashSecretInternal(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func normalizeMobilePasskeyCodeChallengeInternal(codeChallenge string) (string, error) {
	codeChallenge = strings.TrimSpace(codeChallenge)
	decoded, err := base64.RawURLEncoding.DecodeString(codeChallenge)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != codeChallenge {
		return "", ErrPasskeyResponse
	}
	return codeChallenge, nil
}

func mobilePasskeyCodeChallengeInternal(codeVerifier string) (string, error) {
	codeVerifier = strings.TrimSpace(codeVerifier)
	decoded, err := base64.RawURLEncoding.DecodeString(codeVerifier)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != codeVerifier {
		return "", ErrPasskeyTransaction
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomSecretInternal() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func optionalStringInternal(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
