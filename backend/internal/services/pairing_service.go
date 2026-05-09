package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// PairingCodeTTL is how long a freshly issued pairing code is valid.
	// Hardcoded for Phase 1; Phase 2 makes it configurable.
	PairingCodeTTL = 5 * time.Minute

	// shortCodeLength is the number of characters in the manual-entry code.
	// Encoded in custom base32 minus ambiguous characters; ~32^8 ≈ 1.1T combos.
	shortCodeLength = 8

	// qrTokenBytes is the length in bytes of the random QR token before hex
	// encoding. 24 bytes → 48 hex chars; sufficient for cryptographic
	// unguessability.
	qrTokenBytes = 24

	// pairingRedeemRateLimitPerMinute caps RedeemCode attempts per IP per
	// minute.
	pairingRedeemRateLimitPerMinute = 10
)

// shortCodeAlphabet is base32 minus visually ambiguous characters
// (I, O, 0, 1). 32 distinct symbols.
const shortCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var (
	// ErrPairingCodeNotFound is returned when neither short_code nor qr_token
	// matches the supplied input.
	ErrPairingCodeNotFound = errors.New("pairing code not found")
	// ErrPairingCodeExpired is returned for codes whose expires_at < now.
	ErrPairingCodeExpired = errors.New("pairing code has expired")
	// ErrPairingCodeRedeemed is returned for codes already redeemed.
	ErrPairingCodeRedeemed = errors.New("pairing code already redeemed")
	// ErrPairingRateLimited is returned when the per-IP rate limit is exceeded.
	ErrPairingRateLimited = errors.New("too many pairing attempts; try again later")
)

// PairingResult is what RedeemPairingCode returns: the raw token the iOS
// client must store, the resulting device row, and the user it's bound to.
type PairingResult struct {
	RawToken string
	Device   models.Device
	User     models.User
}

// PairingService manages pairing_sessions and orchestrates redemption.
type PairingService struct {
	db            *database.DB
	apiKeyService *ApiKeyService
	deviceService *DeviceService
	userService   *UserService

	// rate limiter — per-IP token bucket reset every minute.
	rateMu     sync.Mutex
	rateBucket map[string]rateEntry
}

type rateEntry struct {
	count int
	reset time.Time
}

func NewPairingService(db *database.DB, apiKeyService *ApiKeyService, deviceService *DeviceService, userService *UserService) *PairingService {
	return &PairingService{
		db:            db,
		apiKeyService: apiKeyService,
		deviceService: deviceService,
		userService:   userService,
		rateBucket:    make(map[string]rateEntry),
	}
}

// IssueCodeResult is the output of IssueCode.
type IssueCodeResult struct {
	ID        string
	ShortCode string
	QrToken   string
	ExpiresAt time.Time
}

// IssueCode generates a fresh pairing code bound to the given user. Either
// the short code or the QR token can be used to redeem it. The code is
// valid for PairingCodeTTL.
func (s *PairingService) IssueCode(ctx context.Context, userID string) (IssueCodeResult, error) {
	if userID == "" {
		return IssueCodeResult{}, fmt.Errorf("user id required to issue pairing code")
	}

	shortCode, err := generateShortCode()
	if err != nil {
		return IssueCodeResult{}, err
	}
	qrToken, err := generateQRToken()
	if err != nil {
		return IssueCodeResult{}, err
	}
	expiresAt := time.Now().Add(PairingCodeTTL)

	session := &models.PairingSession{
		UserID:    userID,
		ShortCode: shortCode,
		QrToken:   qrToken,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return IssueCodeResult{}, fmt.Errorf("failed to create pairing session: %w", err)
	}
	return IssueCodeResult{
		ID:        session.ID,
		ShortCode: shortCode,
		QrToken:   qrToken,
		ExpiresAt: expiresAt,
	}, nil
}

// PairingSessionStatus is the public view of a pairing session, used by the
// web UI's polling endpoint.
type PairingSessionStatus struct {
	Status     string // "pending" | "redeemed" | "expired"
	DeviceID   *string
	DeviceName *string
	ExpiresAt  time.Time
	RedeemedAt *time.Time
}

const (
	PairingStatusPending  = "pending"
	PairingStatusRedeemed = "redeemed"
	PairingStatusExpired  = "expired"
)

// GetSessionStatus returns the public status of a pairing session by its ID.
// Used by the web UI to poll for redemption.
func (s *PairingService) GetSessionStatus(ctx context.Context, sessionID string) (PairingSessionStatus, error) {
	var session models.PairingSession
	if err := s.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PairingSessionStatus{}, ErrPairingCodeNotFound
		}
		return PairingSessionStatus{}, fmt.Errorf("failed to load pairing session: %w", err)
	}

	out := PairingSessionStatus{
		ExpiresAt:  session.ExpiresAt,
		RedeemedAt: session.RedeemedAt,
	}

	switch {
	case session.RedeemedAt != nil:
		out.Status = PairingStatusRedeemed
	case session.ExpiresAt.Before(time.Now()):
		out.Status = PairingStatusExpired
	default:
		out.Status = PairingStatusPending
	}

	if session.RedeemedDeviceID != nil {
		var device models.Device
		if err := s.db.WithContext(ctx).Where("id = ?", *session.RedeemedDeviceID).First(&device).Error; err == nil {
			out.DeviceID = &device.ID
			out.DeviceName = &device.Name
		}
	}
	return out, nil
}

// RedeemPairingCodeInput carries the data RedeemPairingCode needs to upsert
// the device row.
type RedeemPairingCodeInput struct {
	Code        string // either short_code or qr_token
	DeviceID    string
	DeviceName  string
	AppVersion  string
	OsVersion   string
	DeviceModel string
	RemoteAddr  string // for rate limiting
}

// RedeemPairingCode is the orchestration entry point invoked by the gRPC
// PairingService.RedeemCode handler. It runs everything in a transaction:
//
//  1. Locks and validates the pairing session row.
//  2. Creates a new ApiKey for the device.
//  3. Upserts the Device row.
//  4. Marks the pairing session redeemed.
func (s *PairingService) RedeemPairingCode(ctx context.Context, in RedeemPairingCodeInput) (PairingResult, error) {
	if err := s.checkRateLimit(in.RemoteAddr); err != nil {
		return PairingResult{}, err
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		return PairingResult{}, ErrPairingCodeNotFound
	}

	var result PairingResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		session, err := s.lockSessionByCodeTx(tx, code)
		if err != nil {
			return err
		}

		if session.RedeemedAt != nil {
			return ErrPairingCodeRedeemed
		}
		if session.ExpiresAt.Before(time.Now()) {
			return ErrPairingCodeExpired
		}

		// 1. Create an ApiKey for the device.
		keyResult, err := s.apiKeyService.CreateDeviceApiKeyTx(tx, CreateDeviceApiKeyInput{
			UserID: session.UserID,
			Name:   fmt.Sprintf("Mobile: %s", in.DeviceName),
		})
		if err != nil {
			return err
		}

		// 2. Upsert the Device.
		device, err := s.deviceService.UpsertForUserTx(tx, UpsertDeviceInput{
			UserID:      session.UserID,
			DeviceID:    in.DeviceID,
			Name:        in.DeviceName,
			AppVersion:  in.AppVersion,
			OsVersion:   in.OsVersion,
			DeviceModel: in.DeviceModel,
			ApiKeyID:    keyResult.ApiKeyID,
		})
		if err != nil {
			return err
		}

		// 3. Mark the session redeemed.
		now := time.Now()
		deviceID := device.ID
		session.RedeemedAt = &now
		session.RedeemedDeviceID = &deviceID
		if err := tx.Save(&session).Error; err != nil {
			return fmt.Errorf("failed to mark pairing session redeemed: %w", err)
		}

		// 4. Load the user for the response payload.
		user, err := s.userService.GetUserByID(ctx, session.UserID)
		if err != nil {
			return fmt.Errorf("failed to load user: %w", err)
		}

		result = PairingResult{
			RawToken: keyResult.RawKey,
			Device:   device,
			User:     *user,
		}
		return nil
	})

	if err != nil {
		return PairingResult{}, err
	}
	return result, nil
}

// lockSessionByCodeTx loads the pairing session matching either short_code
// or qr_token, locking the row for update so concurrent redemptions can't
// both succeed. SQLite doesn't support FOR UPDATE; the transaction's
// implicit BEGIN IMMEDIATE achieves the same isolation.
func (s *PairingService) lockSessionByCodeTx(tx *gorm.DB, code string) (models.PairingSession, error) {
	var session models.PairingSession
	q := tx.Where("short_code = ? OR qr_token = ?", code, code)
	if tx.Dialector.Name() != "sqlite" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.PairingSession{}, ErrPairingCodeNotFound
		}
		return models.PairingSession{}, fmt.Errorf("failed to load pairing session: %w", err)
	}
	return session, nil
}

func (s *PairingService) checkRateLimit(remoteAddr string) error {
	if remoteAddr == "" {
		return nil
	}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	entry, ok := s.rateBucket[remoteAddr]
	if !ok || now.After(entry.reset) {
		s.rateBucket[remoteAddr] = rateEntry{count: 1, reset: now.Add(time.Minute)}
		return nil
	}
	if entry.count >= pairingRedeemRateLimitPerMinute {
		return ErrPairingRateLimited
	}
	entry.count++
	s.rateBucket[remoteAddr] = entry
	return nil
}

// generateShortCode produces a base32 (custom alphabet) string of length
// shortCodeLength using crypto/rand.
func generateShortCode() (string, error) {
	out := make([]byte, shortCodeLength)
	buf := make([]byte, shortCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate short code: %w", err)
	}
	alphabetLen := byte(len(shortCodeAlphabet))
	for i, b := range buf {
		out[i] = shortCodeAlphabet[b%alphabetLen]
	}
	return string(out), nil
}

// generateQRToken returns a hex-encoded random token of qrTokenBytes bytes.
func generateQRToken() (string, error) {
	buf := make([]byte, qrTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate qr token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// FormatShortCodeDisplay formats an 8-char short code as "ABCD-1234" for
// display. Pure presentation helper, used by the REST handler.
func FormatShortCodeDisplay(code string) string {
	if len(code) != shortCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// NormalizeShortCodeInput strips formatting hyphens/spaces and uppercases.
// Accepts both "ABCD-1234" and "abcd1234" forms.
func NormalizeShortCodeInput(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	return strings.NewReplacer("-", "", " ", "").Replace(code)
}
