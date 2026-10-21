package passkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	userdomain "github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
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
	ErrPasskeyServiceUnavailable = errors.New("passkey service is unavailable")
	ErrPasskeyCeremony           = errors.New("invalid or expired passkey ceremony")
	ErrPasskeyTransaction        = errors.New("invalid or expired authentication transaction")
	ErrPasskeyResponse           = errors.New("invalid passkey response")
	ErrPasskeyNotFound           = errors.New("passkey not found")
	ErrPasskeyExists             = errors.New("passkey already registered")
	ErrPasskeyStepUpRequired     = errors.New("fresh step-up authentication is required")
	ErrPasskeyMFAEnabled         = errors.New("passkey MFA must be disabled first")
	ErrPasskeyMFAAlreadyEnabled  = errors.New("passkey MFA is already enabled")
	ErrPasskeyMFANotEnabled      = errors.New("passkey MFA is not enabled")
	ErrPasskeyNoCredential       = errors.New("no passkeys are registered")
	ErrPasskeyLastCredential     = errors.New("cannot remove the last usable authentication method")
	ErrPasskeyRecoveryCode       = errors.New("invalid recovery code")
	ErrPasskeyName               = errors.New("invalid passkey name")

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
	User   *userdomain.User
	Meta   types.SessionMeta
	Source string
}

// PasskeyService owns WebAuthn ceremonies, passkey persistence, MFA
// transactions, recovery codes, and step-up grants. It intentionally does not
// issue JWTs; auth.AuthService remains the single token/session issuer.
type PasskeyService struct {
	db       *database.DB
	webAuthn *webauthn.WebAuthn
	rpID     string
	initErr  error
}

func NewPasskeyService(db *database.DB, cfg *config.Config) *PasskeyService {
	s := &PasskeyService{db: db}

	parsedURL, err := url.Parse(strings.TrimSpace(cfg.GetAppURL()))
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		if err != nil {
			s.initErr = fmt.Errorf("invalid APP_URL for WebAuthn: %w", err)
		} else {
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

// ready reports whether WebAuthn initialised; every constructor failure sets initErr.
func (s *PasskeyService) ready() error {
	return kit.Ternary[error](s.initErr != nil, ErrPasskeyServiceUnavailable, nil)
}

type webAuthnUser struct {
	model              userdomain.User
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

func (s *PasskeyService) loadWebAuthnUser(ctx context.Context, userID string) (*webAuthnUser, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrPasskeyTransaction
	}

	var user userdomain.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyTransaction
		}
		return nil, fmt.Errorf("failed to load passkey user: %w", err)
	}

	var rows []Passkey
	if err := s.db.WithContext(ctx).Where("user_id = ? AND rp_id = ?", user.ID, s.rpID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load passkeys: %w", err)
	}

	credentials := make([]webauthn.Credential, len(rows))
	credentialModelIDs := make(map[string]string, len(rows))
	for i := range rows {
		credentials[i] = credentialFromModel(rows[i])
		credentialModelIDs[string(rows[i].CredentialID)] = rows[i].ID
	}

	return &webAuthnUser{model: user, credentials: credentials, credentialModelIDs: credentialModelIDs}, nil
}

func credentialFromModel(row Passkey) webauthn.Credential {
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

// LoginAvailable reports whether a registered passkey can be used on this instance.
func (s *PasskeyService) LoginAvailable(ctx context.Context) (bool, error) {
	if errors.Is(s.ready(), ErrPasskeyServiceUnavailable) {
		return false, nil
	}

	credentials := s.db.WithContext(ctx).Model(&Passkey{}).Select("1").
		Joins("JOIN users ON users.id = passkeys.user_id").
		Where("passkeys.rp_id = ?", s.rpID)
	var available bool
	if err := s.db.WithContext(ctx).Raw("SELECT EXISTS (?)", credentials).Scan(&available).Error; err != nil {
		return false, fmt.Errorf("failed to check passkey login availability: %w", err)
	}
	return available, nil
}

func (s *PasskeyService) BeginPasskeyLogin(ctx context.Context) (*PasskeyChallenge, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremonies(ctx); err != nil {
		return nil, err
	}

	assertion, localSession, err := s.webAuthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, fmt.Errorf("failed to begin passkey login: %w", err)
	}

	ceremony, err := s.createCeremony(ctx, passkeyCeremonyPurposeLogin, nil, nil, nil, localSession)
	if err != nil {
		return nil, err
	}

	return &PasskeyChallenge{
		CeremonyID: ceremony.ID,
		Options:    assertion.Response,
		ExpiresAt:  ceremony.ExpiresAt,
	}, nil
}

func (s *PasskeyService) BeginMFAAuthentication(ctx context.Context, userID string, meta types.SessionMeta, source string) (*types.MFAChallenge, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremonies(ctx); err != nil {
		return nil, err
	}
	adapter, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, ErrPasskeyNoCredential
	}

	transaction := newAuthTransaction(userID, authTransactionKindMFA, source, meta, nil)
	if createMFATransactionErr := s.db.WithContext(ctx).Create(transaction).Error; createMFATransactionErr != nil {
		return nil, fmt.Errorf("failed to create MFA transaction: %w", createMFATransactionErr)
	}
	challenge, err := s.beginMFAForTransaction(ctx, transaction)
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(transaction).Error
		return nil, err
	}
	return challenge, nil
}

// BeginMFAForTransaction starts a passkey assertion for an already-created
// pending MFA transaction. This is useful to clients that separate primary
// authentication from challenge retrieval.
func (s *PasskeyService) BeginMFAForTransaction(ctx context.Context, transactionID string) (*types.MFAChallenge, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremonies(ctx); err != nil {
		return nil, err
	}
	transaction, err := s.loadPendingTransaction(ctx, transactionID, authTransactionKindMFA)
	if err != nil {
		return nil, err
	}
	return s.beginMFAForTransaction(ctx, transaction)
}

func (s *PasskeyService) beginMFAForTransaction(ctx context.Context, transaction *AuthTransaction) (*types.MFAChallenge, error) {
	if transaction == nil {
		return nil, ErrPasskeyTransaction
	}
	adapter, err := s.loadWebAuthnUser(ctx, transaction.UserID)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, ErrPasskeyNoCredential
	}
	// A transaction has one active assertion at a time. Replacing an older
	// pending ceremony prevents a client from accumulating reusable challenges.
	if invalidateMFACeremoniesErr := s.db.WithContext(ctx).
		Where("auth_transaction_id = ? AND purpose = ? AND consumed_at IS NULL", transaction.ID, passkeyCeremonyPurposeMFA).
		Delete(&PasskeyCeremony{}).Error; invalidateMFACeremoniesErr != nil {
		return nil, fmt.Errorf("failed to replace MFA ceremony: %w", invalidateMFACeremoniesErr)
	}

	assertion, webSession, err := s.webAuthn.BeginLogin(adapter, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, fmt.Errorf("failed to begin MFA passkey ceremony: %w", err)
	}

	transactionID := transaction.ID
	userIDPointer := transaction.UserID
	ceremony, err := s.createCeremony(ctx, passkeyCeremonyPurposeMFA, &userIDPointer, nil, &transactionID, webSession)
	if err != nil {
		return nil, err
	}

	return &types.MFAChallenge{
		TransactionID: transaction.ID,
		Method:        session.PasskeyMFAMethod,
		Options:       assertion.Response,
		ExpiresAt:     ceremony.ExpiresAt,
	}, nil
}

func (s *PasskeyService) BeginStepUp(ctx context.Context, userID, sessionID string, meta types.SessionMeta) (*PasskeyChallenge, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremonies(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureActiveSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}

	adapter, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, ErrPasskeyNoCredential
	}

	transaction := newAuthTransaction(userID, authTransactionKindStepUp, session.UserSessionSourceLocal, meta, &sessionID)
	if createStepUpTransactionErr := s.db.WithContext(ctx).Create(transaction).Error; createStepUpTransactionErr != nil {
		return nil, fmt.Errorf("failed to create step-up transaction: %w", createStepUpTransactionErr)
	}

	assertion, localSession, err := s.webAuthn.BeginLogin(adapter, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(transaction).Error
		return nil, fmt.Errorf("failed to begin step-up passkey ceremony: %w", err)
	}

	transactionID := transaction.ID
	userIDPointer := userID
	ceremony, err := s.createCeremony(ctx, passkeyCeremonyPurposeStepUp, &userIDPointer, &sessionID, &transactionID, localSession)
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

func (s *PasskeyService) BeginRegistration(ctx context.Context, userID, sessionID, stepUpToken string) (*PasskeyChallenge, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.sweepCeremonies(ctx); err != nil {
		return nil, err
	}
	if err := s.authorizeManagement(ctx, userID, sessionID, stepUpToken, true); err != nil {
		return nil, err
	}

	adapter, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	exclusions := webauthn.Credentials(adapter.credentials).CredentialDescriptors()
	creation, localSession, err := s.webAuthn.BeginRegistration(
		adapter,
		webauthn.WithCredentialParameters(webauthn.CredentialParametersPQCRecommendedL3()),
		webauthn.WithExclusions(exclusions),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to begin passkey registration: %w", err)
	}

	userIDPointer := userID
	ceremony, err := s.createCeremony(ctx, passkeyCeremonyPurposeRegistration, &userIDPointer, &sessionID, nil, localSession)
	if err != nil {
		return nil, err
	}

	return &PasskeyChallenge{CeremonyID: ceremony.ID, Options: creation.Response, ExpiresAt: ceremony.ExpiresAt}, nil
}

func (s *PasskeyService) FinishRegistration(ctx context.Context, userID, sessionID, ceremonyID string, payload []byte, name string) (*PasskeySummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.ensureActiveSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxPasskeyPayloadBytes {
		return nil, ErrPasskeyResponse
	}

	ceremony, err := s.consumeCeremony(ctx, ceremonyID, passkeyCeremonyPurposeRegistration)
	if err != nil {
		return nil, err
	}
	if ceremony.UserID == nil || *ceremony.UserID != userID || ceremony.SessionID == nil || *ceremony.SessionID != sessionID {
		return nil, ErrPasskeyCeremony
	}

	var localSession webauthn.SessionData
	if unmarshalErr := json.Unmarshal([]byte(ceremony.SessionData), &localSession); unmarshalErr != nil {
		return nil, ErrPasskeyCeremony
	}
	adapter, err := s.loadWebAuthnUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload)
	if err != nil {
		return nil, ErrPasskeyResponse
	}
	credential, err := s.webAuthn.CreateCredential(adapter, localSession, parsed)
	if err != nil {
		return nil, ErrPasskeyResponse
	}

	if strings.TrimSpace(name) == "" {
		name = defaultPasskeyName(credential.Authenticator.AAGUID)
	}
	name, err = normalizePasskeyName(name)
	if err != nil {
		return nil, err
	}
	row, err := s.persistCredential(ctx, userID, credential, name)
	if err != nil {
		return nil, err
	}
	summary := passkeySummary(*row)
	return &summary, nil
}

func (s *PasskeyService) FinishPasskeyLogin(ctx context.Context, ceremonyID string, payload []byte) (*userdomain.User, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxPasskeyPayloadBytes {
		return nil, ErrPasskeyResponse
	}

	ceremony, err := s.consumeCeremony(ctx, ceremonyID, passkeyCeremonyPurposeLogin)
	if err != nil {
		return nil, err
	}
	var localSession webauthn.SessionData
	if unmarshalErr := json.Unmarshal([]byte(ceremony.SessionData), &localSession); unmarshalErr != nil {
		return nil, ErrPasskeyCeremony
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		slog.WarnContext(ctx, "passkey login validation failed", "stage", "parse", "error", err)
		return nil, ErrPasskeyResponse
	}
	utils.NormalizePasskeyAssertionExtensions(localSession.Extensions, parsed)

	var resolved *webAuthnUser
	user, credential, err := s.webAuthn.ValidatePasskeyLogin(func(_, userHandle []byte) (webauthn.User, error) {
		adapter, lookupErr := s.loadWebAuthnUser(ctx, string(userHandle))
		if lookupErr != nil {
			return nil, lookupErr
		}
		resolved = adapter
		return adapter, nil
	}, localSession, parsed)
	if err != nil || user == nil || credential == nil || resolved == nil {
		slog.WarnContext(ctx, "passkey login validation failed", "stage", "assertion", "error", err)
		return nil, ErrPasskeyResponse
	}

	if updateCredentialAfterAssertionErr := s.updateCredentialAfterAssertion(ctx, resolved, credential); updateCredentialAfterAssertionErr != nil {
		return nil, updateCredentialAfterAssertionErr
	}
	return &resolved.model, nil
}

func (s *PasskeyService) FinishMobilePasskeyLogin(ctx context.Context, ceremonyID string, payload []byte, codeChallenge string) (*types.MobilePasskeyCompletion, error) {
	codeChallenge, err := normalizeMobilePasskeyCodeChallenge(codeChallenge)
	if err != nil {
		return nil, err
	}
	user, err := s.FinishPasskeyLogin(ctx, ceremonyID, payload)
	if err != nil {
		return nil, err
	}

	transaction := newAuthTransaction(user.ID, authTransactionKindMobilePasskey, session.UserSessionSourcePasskey, types.SessionMeta{}, nil)
	transaction.SecretHash = &codeChallenge
	if createMobileTransactionErr := s.db.WithContext(ctx).Create(transaction).Error; createMobileTransactionErr != nil {
		return nil, fmt.Errorf("failed to create mobile passkey transaction: %w", createMobileTransactionErr)
	}
	return &types.MobilePasskeyCompletion{TransactionID: transaction.ID, ExpiresAt: transaction.ExpiresAt}, nil
}

func (s *PasskeyService) ExchangeMobilePasskeyLogin(ctx context.Context, transactionID, codeVerifier string) (*userdomain.User, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	codeChallenge, err := mobilePasskeyCodeChallenge(codeVerifier)
	if err != nil {
		return nil, err
	}

	var transaction AuthTransaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if loadMobileTransactionErr := tx.Where(
			"id = ? AND kind = ? AND status = ? AND secret_hash = ? AND expires_at > ?",
			transactionID,
			authTransactionKindMobilePasskey,
			authTransactionPending,
			codeChallenge,
			time.Now(),
		).First(
			&transaction,
		).Error; loadMobileTransactionErr != nil {
			if errors.Is(loadMobileTransactionErr, gorm.ErrRecordNotFound) {
				return ErrPasskeyTransaction
			}
			return fmt.Errorf("failed to load mobile passkey transaction: %w", loadMobileTransactionErr)
		}

		now := time.Now()
		result := tx.Model(&AuthTransaction{}).
			Where("id = ? AND status = ? AND expires_at > ?", transaction.ID, authTransactionPending, now).
			Updates(map[string]any{"status": authTransactionCompleted, "completed_at": now, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("failed to complete mobile passkey transaction: %w", result.Error)
		}
		return kit.Ternary[error](result.RowsAffected != 1, ErrPasskeyTransaction, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.loadUserModel(ctx, transaction.UserID)
}

func (s *PasskeyService) FinishMFA(ctx context.Context, transactionID string, payload []byte) (*AuthenticationCompletion, error) {
	transaction, err := s.loadPendingTransaction(ctx, transactionID, authTransactionKindMFA)
	if err != nil {
		return nil, err
	}
	user, err := s.finishKnownUserAssertion(ctx, transaction, passkeyCeremonyPurposeMFA, payload)
	if err != nil {
		return nil, err
	}
	if completeTransactionErr := s.completeTransaction(ctx, transaction.ID); completeTransactionErr != nil {
		return nil, completeTransactionErr
	}

	now := time.Now()
	return &AuthenticationCompletion{
		User:   user,
		Meta:   transactionSessionMeta(transaction, session.PasskeyMFAMethod, &now),
		Source: transaction.Source,
	}, nil
}

func (s *PasskeyService) FinishRecoveryCode(ctx context.Context, transactionID, code string) (*AuthenticationCompletion, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if normalized == "" {
		return nil, ErrPasskeyRecoveryCode
	}

	hash := kit.SHA256Hex(normalized)
	var transaction AuthTransaction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(
			clause.Locking{
				Strength: "UPDATE",
			},
		).Where(
			"id = ? AND kind = ? AND status = ? AND expires_at > ?",
			transactionID,
			authTransactionKindMFA,
			authTransactionPending,
			time.Now(),
		).First(
			&transaction,
		).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPasskeyTransaction
			}
			return fmt.Errorf("failed to lock MFA transaction: %w", err)
		}
		var recoveryCodes []PasskeyRecoveryCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND used_at IS NULL", transaction.UserID).Find(&recoveryCodes).Error; err != nil {
			return fmt.Errorf("failed to load recovery codes: %w", err)
		}

		var matched *PasskeyRecoveryCode
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
		result := tx.Model(&PasskeyRecoveryCode{}).
			Where("id = ? AND used_at IS NULL", matched.ID).
			Updates(map[string]any{"used_at": now, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("failed to consume recovery code: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrPasskeyRecoveryCode
		}
		now = time.Now()
		result = tx.Model(&AuthTransaction{}).
			Where("id = ? AND status = ? AND expires_at > ?", transaction.ID, authTransactionPending, now).
			Updates(map[string]any{"status": authTransactionCompleted, "completed_at": now, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("failed to complete MFA transaction: %w", result.Error)
		}
		return kit.Ternary[error](result.RowsAffected != 1, ErrPasskeyTransaction, nil)
	})
	if err != nil {
		return nil, err
	}
	user, err := s.loadUserModel(ctx, transaction.UserID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &AuthenticationCompletion{
		User:   user,
		Meta:   transactionSessionMeta(&transaction, session.RecoveryCodeMFAMethod, &now),
		Source: transaction.Source,
	}, nil
}

func (s *PasskeyService) FinishStepUp(ctx context.Context, transactionID, sessionID string, payload []byte) (*StepUpGrant, error) {
	transaction, err := s.loadPendingTransaction(ctx, transactionID, authTransactionKindStepUp)
	if err != nil {
		return nil, err
	}
	if transaction.SessionID == nil || *transaction.SessionID != sessionID {
		return nil, ErrPasskeyTransaction
	}
	if ensureActiveSessionErr := s.ensureActiveSession(ctx, transaction.UserID, sessionID); ensureActiveSessionErr != nil {
		return nil, ensureActiveSessionErr
	}
	if _, finishKnownUserAssertionErr := s.finishKnownUserAssertion(ctx, transaction, passkeyCeremonyPurposeStepUp, payload); finishKnownUserAssertionErr != nil {
		return nil, finishKnownUserAssertionErr
	}
	return s.issueStepUpGrant(ctx, transaction.ID)
}

func (s *PasskeyService) CreatePasswordStepUpGrant(ctx context.Context, userID, sessionID string) (*StepUpGrant, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.ensureActiveSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	transaction := newAuthTransaction(userID, authTransactionKindStepUp, session.UserSessionSourceLocal, types.SessionMeta{}, &sessionID)
	if err := s.db.WithContext(ctx).Create(transaction).Error; err != nil {
		return nil, fmt.Errorf("failed to create password step-up transaction: %w", err)
	}
	return s.issueStepUpGrant(ctx, transaction.ID)
}

// VerifyStepUpToken accepts a grant repeatedly until it expires, so a single
// re-authentication covers a short run of management actions instead of one.
// The grant stays bound to the issuing user and session.
func (s *PasskeyService) VerifyStepUpToken(ctx context.Context, userID, sessionID, token string) error {
	if err := s.ready(); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return ErrPasskeyStepUpRequired
	}
	if err := s.ensureActiveSession(ctx, userID, sessionID); err != nil {
		return err
	}
	hash := kit.SHA256Hex(token)
	var count int64
	if err := s.db.WithContext(ctx).Model(&AuthTransaction{}).
		Where("user_id = ? AND session_id = ? AND kind = ? AND status = ? AND secret_hash = ? AND expires_at > ?", userID, sessionID, authTransactionKindStepUp, authTransactionCompleted, hash, time.Now()).
		Count(&count).Error; err != nil {
		return fmt.Errorf("failed to verify step-up grant: %w", err)
	}
	return kit.Ternary[error](count != 1, ErrPasskeyStepUpRequired, nil)
}

func (s *PasskeyService) ListPasskeys(ctx context.Context, userID string) ([]PasskeySummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var rows []Passkey
	if err := s.db.WithContext(ctx).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list passkeys: %w", err)
	}
	result := make([]PasskeySummary, len(rows))
	for i := range rows {
		result[i] = passkeySummary(rows[i])
	}
	return result, nil
}

func (s *PasskeyService) GetCapabilities(ctx context.Context, userID string, oidcEnabled bool) (*PasskeyCapabilities, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	user, err := s.loadUserModel(ctx, userID)
	if err != nil {
		return nil, err
	}
	passkeyCount, err := s.countPasskeys(ctx, userID)
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

func (s *PasskeyService) RenamePasskey(ctx context.Context, userID, passkeyID, name, sessionID, stepUpToken string) (*PasskeySummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.authorizeManagement(ctx, userID, sessionID, stepUpToken, false); err != nil {
		return nil, err
	}
	name, err := normalizePasskeyName(name)
	if err != nil {
		return nil, err
	}
	var row Passkey
	result := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND rp_id = ?", passkeyID, userID, s.rpID).First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrPasskeyNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to load passkey: %w", result.Error)
	}
	now := time.Now()
	if renamePasskeyErr := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"name": name, "updated_at": now}).Error; renamePasskeyErr != nil {
		return nil, fmt.Errorf("failed to rename passkey: %w", renamePasskeyErr)
	}
	row.Name = name
	row.UpdatedAt = &now
	summary := passkeySummary(row)
	return &summary, nil
}

func (s *PasskeyService) DeletePasskey(ctx context.Context, userID, passkeyID, sessionID, stepUpToken string, oidcFallbackAllowed bool) error {
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.authorizeManagement(ctx, userID, sessionID, stepUpToken, false); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user userdomain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return fmt.Errorf("failed to lock user for passkey deletion: %w", err)
		}
		var count int64
		if err := tx.Model(&Passkey{}).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Count(&count).Error; err != nil {
			return fmt.Errorf("failed to count passkeys: %w", err)
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
		result := tx.Where("id = ? AND user_id = ? AND rp_id = ?", passkeyID, userID, s.rpID).Delete(&Passkey{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete passkey: %w", result.Error)
		}
		return kit.Ternary[error](result.RowsAffected != 1, ErrPasskeyNotFound, nil)
	})
}

func (s *PasskeyService) GetMFAStatus(ctx context.Context, userID string) (*MFAStatus, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	user, err := s.loadUserModel(ctx, userID)
	if err != nil {
		return nil, err
	}
	passkeyCount, err := s.countPasskeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	var recoveryCodes int64
	if countRecoveryCodesErr := s.db.WithContext(ctx).Model(&PasskeyRecoveryCode{}).Where("user_id = ? AND used_at IS NULL", userID).Count(&recoveryCodes).Error; countRecoveryCodesErr != nil {
		return nil, fmt.Errorf("failed to count recovery codes: %w", countRecoveryCodesErr)
	}
	return &MFAStatus{Enabled: user.PasskeyMFAEnabled, PasskeyCount: passkeyCount, RecoveryCodesRemaining: int(recoveryCodes)}, nil
}

func (s *PasskeyService) EnableMFA(ctx context.Context, userID, sessionID, stepUpToken string) ([]string, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken); err != nil {
		return nil, err
	}
	codes, rows, err := generateRecoveryCodeRows(userID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user userdomain.User
		if lockMFAUserErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; lockMFAUserErr != nil {
			return fmt.Errorf("failed to lock user for MFA enable: %w", lockMFAUserErr)
		}
		var count int64
		if countPasskeysErr := tx.Model(&Passkey{}).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Count(&count).Error; countPasskeysErr != nil {
			return fmt.Errorf("failed to count passkeys for MFA enable: %w", countPasskeysErr)
		}
		if count == 0 {
			return ErrPasskeyNoCredential
		}
		if user.PasskeyMFAEnabled {
			return ErrPasskeyMFAAlreadyEnabled
		}
		if enableMFAErr := tx.Model(&userdomain.User{}).Where("id = ?", userID).Update("passkey_mfa_enabled", true).Error; enableMFAErr != nil {
			return fmt.Errorf("failed to enable passkey MFA: %w", enableMFAErr)
		}
		if clearRecoveryCodesErr := tx.Where("user_id = ?", userID).Delete(&PasskeyRecoveryCode{}).Error; clearRecoveryCodesErr != nil {
			return fmt.Errorf("failed to replace recovery codes: %w", clearRecoveryCodesErr)
		}
		if createRecoveryCodesErr := tx.Create(&rows).Error; createRecoveryCodesErr != nil {
			return fmt.Errorf("failed to create recovery codes: %w", createRecoveryCodesErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *PasskeyService) DisableMFA(ctx context.Context, userID, sessionID, stepUpToken string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken); err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user userdomain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return fmt.Errorf("failed to lock user for MFA disable: %w", err)
		}
		if !user.PasskeyMFAEnabled {
			return ErrPasskeyMFANotEnabled
		}
		if err := tx.Model(&userdomain.User{}).Where("id = ?", userID).Update("passkey_mfa_enabled", false).Error; err != nil {
			return fmt.Errorf("failed to disable passkey MFA: %w", err)
		}
		if err := tx.Where("user_id = ?", userID).Delete(&PasskeyRecoveryCode{}).Error; err != nil {
			return fmt.Errorf("failed to delete recovery codes: %w", err)
		}
		now := time.Now()
		query := tx.Model(&session.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", userID)
		if strings.TrimSpace(sessionID) != "" {
			query = query.Where("id <> ?", sessionID)
		}
		if err := query.Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("failed to revoke sessions after MFA disable: %w", err)
		}
		return nil
	})
	return err
}

func (s *PasskeyService) RegenerateRecoveryCodes(ctx context.Context, userID, sessionID, stepUpToken string) ([]string, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken); err != nil {
		return nil, err
	}
	codes, rows, err := generateRecoveryCodeRows(userID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user userdomain.User
		if loadRecoveryUserErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; loadRecoveryUserErr != nil {
			return fmt.Errorf("failed to load user for recovery code regeneration: %w", loadRecoveryUserErr)
		}
		if !user.PasskeyMFAEnabled {
			return ErrPasskeyMFANotEnabled
		}
		if clearRecoveryCodesErr := tx.Where("user_id = ?", userID).Delete(&PasskeyRecoveryCode{}).Error; clearRecoveryCodesErr != nil {
			return fmt.Errorf("failed to replace recovery codes: %w", clearRecoveryCodesErr)
		}
		if createRecoveryCodesErr := tx.Create(&rows).Error; createRecoveryCodesErr != nil {
			return fmt.Errorf("failed to create recovery codes: %w", createRecoveryCodesErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// ResetMFAForUser is intentionally separate from self-service operations. It
// is used only by the explicitly gated embedded admin recovery command.
func (s *PasskeyService) ResetMFAForUser(ctx context.Context, userID string) error {
	if err := s.ready(); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user userdomain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPasskeyTransaction
			}
			return fmt.Errorf("failed to load user for MFA reset: %w", err)
		}
		if err := tx.Model(&userdomain.User{}).Where("id = ?", userID).Update("passkey_mfa_enabled", false).Error; err != nil {
			return fmt.Errorf("failed to disable passkey MFA: %w", err)
		}
		if err := tx.Where("user_id = ?", userID).Delete(&PasskeyRecoveryCode{}).Error; err != nil {
			return fmt.Errorf("failed to delete recovery codes: %w", err)
		}
		if err := tx.Where("user_id = ?", userID).Delete(&PasskeyCeremony{}).Error; err != nil {
			return fmt.Errorf("failed to delete passkey ceremonies: %w", err)
		}
		if err := tx.Where("user_id = ?", userID).Delete(&AuthTransaction{}).Error; err != nil {
			return fmt.Errorf("failed to delete authentication transactions: %w", err)
		}
		now := time.Now()
		if err := tx.Model(&session.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", userID).Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("failed to revoke sessions after MFA reset: %w", err)
		}
		return nil
	})
}

func (s *PasskeyService) createCeremony(ctx context.Context, purpose string, userID, sessionID, transactionID *string, localSession *webauthn.SessionData) (*PasskeyCeremony, error) {
	if localSession == nil {
		return nil, ErrPasskeyCeremony
	}
	serialized, err := json.Marshal(localSession)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize WebAuthn session: %w", err)
	}
	expiresAt := localSession.Expires
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(passkeyCeremonyTTL)
	}
	ceremony := &PasskeyCeremony{
		ID:                uuid.New().String(),
		Purpose:           purpose,
		UserID:            userID,
		SessionID:         sessionID,
		AuthTransactionID: transactionID,
		RPID:              s.rpID,
		SessionData:       string(serialized),
		ExpiresAt:         expiresAt,
	}
	if createCeremonyErr := s.db.WithContext(ctx).Create(ceremony).Error; createCeremonyErr != nil {
		return nil, fmt.Errorf("failed to store passkey ceremony: %w", createCeremonyErr)
	}
	return ceremony, nil
}

func (s *PasskeyService) sweepCeremonies(ctx context.Context) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).
		Where("expires_at <= ? OR consumed_at IS NOT NULL", now).
		Delete(&PasskeyCeremony{}).Error; err != nil {
		return fmt.Errorf("failed to sweep passkey ceremonies: %w", err)
	}
	if err := s.db.WithContext(ctx).
		Where("expires_at <= ?", now.Add(-24*time.Hour)).
		Delete(&AuthTransaction{}).Error; err != nil {
		return fmt.Errorf("failed to sweep authentication transactions: %w", err)
	}
	return nil
}

func (s *PasskeyService) consumeCeremony(ctx context.Context, ceremonyID, purpose string) (*PasskeyCeremony, error) {
	if strings.TrimSpace(ceremonyID) == "" {
		return nil, ErrPasskeyCeremony
	}
	var ceremony PasskeyCeremony
	now := time.Now()
	if err := s.db.WithContext(ctx).Where("id = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?", ceremonyID, purpose, now).First(&ceremony).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyCeremony
		}
		return nil, fmt.Errorf("failed to load passkey ceremony: %w", err)
	}
	result := s.db.WithContext(ctx).Model(&PasskeyCeremony{}).
		Where("id = ? AND purpose = ? AND consumed_at IS NULL AND expires_at > ?", ceremonyID, purpose, now).
		Updates(map[string]any{"consumed_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, fmt.Errorf("failed to consume passkey ceremony: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrPasskeyCeremony
	}
	ceremony.ConsumedAt = &now
	return &ceremony, nil
}

func (s *PasskeyService) loadPendingTransaction(ctx context.Context, transactionID, kind string) (*AuthTransaction, error) {
	if strings.TrimSpace(transactionID) == "" {
		return nil, ErrPasskeyTransaction
	}
	var transaction AuthTransaction
	if err := s.db.WithContext(ctx).Where("id = ? AND kind = ? AND status = ? AND expires_at > ?", transactionID, kind, authTransactionPending, time.Now()).First(&transaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyTransaction
		}
		return nil, fmt.Errorf("failed to load authentication transaction: %w", err)
	}
	return &transaction, nil
}

func (s *PasskeyService) finishKnownUserAssertion(ctx context.Context, transaction *AuthTransaction, purpose string, payload []byte) (*userdomain.User, error) {
	if transaction == nil || len(payload) == 0 || len(payload) > maxPasskeyPayloadBytes {
		return nil, ErrPasskeyResponse
	}
	ceremony, err := s.consumeCeremony(ctx, s.ceremonyIDForTransaction(ctx, transaction.ID, purpose), purpose)
	if err != nil {
		return nil, err
	}
	if ceremony.AuthTransactionID == nil || *ceremony.AuthTransactionID != transaction.ID || ceremony.UserID == nil || *ceremony.UserID != transaction.UserID {
		return nil, ErrPasskeyCeremony
	}
	var localSession webauthn.SessionData
	if unmarshalErr := json.Unmarshal([]byte(ceremony.SessionData), &localSession); unmarshalErr != nil {
		return nil, ErrPasskeyCeremony
	}
	adapter, err := s.loadWebAuthnUser(ctx, transaction.UserID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		return nil, ErrPasskeyResponse
	}
	utils.NormalizePasskeyAssertionExtensions(localSession.Extensions, parsed)
	credential, err := s.webAuthn.ValidateLogin(adapter, localSession, parsed)
	if err != nil || credential == nil {
		return nil, ErrPasskeyResponse
	}
	if updateCredentialAfterAssertionErr := s.updateCredentialAfterAssertion(ctx, adapter, credential); updateCredentialAfterAssertionErr != nil {
		return nil, updateCredentialAfterAssertionErr
	}
	return &adapter.model, nil
}

func (s *PasskeyService) ceremonyIDForTransaction(ctx context.Context, transactionID, purpose string) string {
	var ceremony PasskeyCeremony
	err := s.db.WithContext(ctx).
		Where("auth_transaction_id = ? AND purpose = ? AND consumed_at IS NULL", transactionID, purpose).
		Order("created_at DESC").
		First(&ceremony).Error
	return kit.Ternary(err != nil, "", ceremony.ID)
}

func (s *PasskeyService) completeTransaction(ctx context.Context, transactionID string) error {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&AuthTransaction{}).
		Where("id = ? AND status = ? AND expires_at > ?", transactionID, authTransactionPending, now).
		Updates(map[string]any{"status": authTransactionCompleted, "completed_at": now, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("failed to complete authentication transaction: %w", result.Error)
	}
	return kit.Ternary[error](result.RowsAffected != 1, ErrPasskeyTransaction, nil)
}

func (s *PasskeyService) issueStepUpGrant(ctx context.Context, transactionID string) (*StepUpGrant, error) {
	token := kit.RandomString(43)
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&AuthTransaction{}).
		Where("id = ? AND kind = ? AND status = ? AND expires_at > ?", transactionID, authTransactionKindStepUp, authTransactionPending, now).
		Updates(map[string]any{
			"status":       authTransactionCompleted,
			"secret_hash":  kit.SHA256Hex(token),
			"completed_at": now,
			"updated_at":   now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("failed to store step-up grant: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrPasskeyTransaction
	}
	var transaction AuthTransaction
	if err := s.db.WithContext(ctx).Where("id = ?", transactionID).First(&transaction).Error; err != nil {
		return nil, fmt.Errorf("failed to load step-up grant: %w", err)
	}
	return &StepUpGrant{Token: token, ExpiresAt: transaction.ExpiresAt}, nil
}

func (s *PasskeyService) authorizeManagement(ctx context.Context, userID, sessionID, stepUpToken string, allowFirstEnrollment bool) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrPasskeyStepUpRequired
	}
	count, err := s.countPasskeys(ctx, userID)
	if err != nil {
		return err
	}
	if allowFirstEnrollment && count == 0 {
		return s.ensureActiveSession(ctx, userID, sessionID)
	}
	return s.VerifyStepUpToken(ctx, userID, sessionID, stepUpToken)
}

func (s *PasskeyService) loadUserModel(ctx context.Context, userID string) (*userdomain.User, error) {
	var user userdomain.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasskeyTransaction
		}
		return nil, fmt.Errorf("failed to load user: %w", err)
	}
	return &user, nil
}

func (s *PasskeyService) ensureActiveSession(ctx context.Context, userID, sessionID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return ErrPasskeyStepUpRequired
	}
	var localSession session.UserSession
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", sessionID, userID, time.Now()).
		First(&localSession).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPasskeyStepUpRequired
		}
		return fmt.Errorf("failed to validate active session: %w", err)
	}
	return nil
}

func (s *PasskeyService) countPasskeys(ctx context.Context, userID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&Passkey{}).Where("user_id = ? AND rp_id = ?", userID, s.rpID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count passkeys: %w", err)
	}
	return int(count), nil
}

func (s *PasskeyService) persistCredential(ctx context.Context, userID string, credential *webauthn.Credential, name string) (*Passkey, error) {
	if credential == nil || len(credential.ID) == 0 || len(credential.PublicKey) == 0 {
		return nil, ErrPasskeyResponse
	}
	var existing Passkey
	result := s.db.WithContext(ctx).Where("rp_id = ? AND credential_id = ?", s.rpID, credential.ID).First(&existing)
	if result.Error == nil {
		return nil, ErrPasskeyExists
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		if err := result.Error; err != nil {
			return nil, fmt.Errorf("failed to check existing passkey: %w", err)
		}
		return nil, nil
	}
	transports := make(database.StringSlice, len(credential.Transport))
	for i, transport := range credential.Transport {
		transports[i] = string(transport)
	}
	row := &Passkey{
		ID:                            uuid.New().String(),
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
		return nil, fmt.Errorf("failed to store passkey: %w", err)
	}
	return row, nil
}

func (s *PasskeyService) updateCredentialAfterAssertion(ctx context.Context, adapter *webAuthnUser, credential *webauthn.Credential) error {
	if adapter == nil || credential == nil || len(credential.ID) == 0 {
		return ErrPasskeyResponse
	}
	credentialModelID, ok := adapter.credentialModelIDs[string(credential.ID)]
	if !ok || credentialModelID == "" {
		return ErrPasskeyResponse
	}
	now := time.Now()
	if credential.Authenticator.CloneWarning {
		slog.WarnContext(ctx, "passkey assertion rejected: signature counter regressed", "passkeyId", credentialModelID, "userId", adapter.model.ID)
		if err := s.db.WithContext(ctx).Model(&Passkey{}).
			Where("id = ? AND user_id = ? AND rp_id = ?", credentialModelID, adapter.model.ID, s.rpID).
			Updates(map[string]any{
				"clone_warning": true,
				"updated_at":    now,
			}).Error; err != nil {
			slog.WarnContext(ctx, "failed to flag cloned passkey", "passkeyId", credentialModelID, "error", err)
		}
		return ErrPasskeyResponse
	}
	result := s.db.WithContext(ctx).Model(&Passkey{}).
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
		return fmt.Errorf("failed to update passkey counter: %w", result.Error)
	}
	return kit.Ternary[error](result.RowsAffected != 1, ErrPasskeyResponse, nil)
}

func newAuthTransaction(userID, kind, source string, meta types.SessionMeta, sessionID *string) *AuthTransaction {
	if strings.TrimSpace(source) == "" {
		source = session.UserSessionSourceLocal
	}
	return &AuthTransaction{
		ID:        uuid.New().String(),
		Kind:      kind,
		UserID:    userID,
		SessionID: sessionID,
		Source:    source,
		UserAgent: optionalString(meta.UserAgent),
		IPAddress: optionalString(meta.IPAddress),
		Status:    authTransactionPending,
		ExpiresAt: time.Now().Add(passkeyStepUpTTL),
	}
}

func transactionSessionMeta(transaction *AuthTransaction, method string, verifiedAt *time.Time) types.SessionMeta {
	meta := types.SessionMeta{Source: transaction.Source, MFAMethod: method, MFAVerifiedAt: verifiedAt}
	if transaction.UserAgent != nil {
		meta.UserAgent = *transaction.UserAgent
	}
	if transaction.IPAddress != nil {
		meta.IPAddress = *transaction.IPAddress
	}
	return meta
}

func passkeySummary(row Passkey) PasskeySummary {
	transports := append([]string(nil), row.Transports...)
	return PasskeySummary{
		ID:                      row.ID,
		Name:                    row.Name,
		RPID:                    row.RPID,
		AAGUID:                  formatAAGUID(row.AAGUID),
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

func normalizePasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrPasskeyName
	}
	if utf8.RuneCountInString(name) > maxPasskeyNameRunes {
		return "", fmt.Errorf("passkey name is too long: %w", ErrPasskeyName)
	}
	return name, nil
}

func defaultPasskeyName(aaguid []byte) string {
	formatted := formatAAGUID(aaguid)
	if formatted != "" {
		authenticatorNamesOnce.Do(loadAuthenticatorNames)
		if name := strings.TrimSpace(authenticatorNames[formatted]); name != "" {
			return name + " Passkey"
		}
	}
	return "New Passkey"
}

func formatAAGUID(aaguid []byte) string {
	if len(aaguid) == 0 {
		return ""
	}
	if len(aaguid) == 16 {
		return uuid.UUID(aaguid).String()
	}
	return hex.EncodeToString(aaguid)
}

func loadAuthenticatorNames() {
	// This community catalog is display-only and must never influence
	// authentication decisions. Source: pocket-id/passkey-aaguids.
	authenticatorNames = map[string]string{}
	data, err := resources.FS.ReadFile("aaguids.json")
	if err != nil {
		return
	}
	if unmarshalErr := json.Unmarshal(data, &authenticatorNames); unmarshalErr != nil {
		authenticatorNames = map[string]string{}
	}
}

func generateRecoveryCodeRows(userID string) ([]string, []PasskeyRecoveryCode, error) {
	codes := make([]string, recoveryCodeCount)
	rows := make([]PasskeyRecoveryCode, recoveryCodeCount)
	for i := range codes {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("failed to generate recovery code: %w", err)
		}
		encoded := strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), "=")
		codes[i] = groupRecoveryCode(encoded)
		hash := kit.SHA256Hex(strings.ToUpper(strings.ReplaceAll(codes[i], "-", "")))
		rows[i] = PasskeyRecoveryCode{ID: uuid.New().String(), UserID: userID, CodeHash: hash}
	}
	return codes, rows, nil
}

func groupRecoveryCode(code string) string {
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

func normalizeMobilePasskeyCodeChallenge(codeChallenge string) (string, error) {
	codeChallenge = strings.TrimSpace(codeChallenge)
	decoded, err := base64.RawURLEncoding.DecodeString(codeChallenge)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != codeChallenge {
		return "", ErrPasskeyResponse
	}
	return codeChallenge, nil
}

func mobilePasskeyCodeChallenge(codeVerifier string) (string, error) {
	codeVerifier = strings.TrimSpace(codeVerifier)
	decoded, err := base64.RawURLEncoding.DecodeString(codeVerifier)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != codeVerifier {
		return "", ErrPasskeyTransaction
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
