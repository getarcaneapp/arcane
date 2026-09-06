package apns

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"go.getarcane.app/kit/normalization"

	"emperror.dev/errors"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	apnstypes "github.com/getarcaneapp/arcane/types/v2/apns"
)

const (
	FeatureName          = "mobile-push-v1"
	signatureHeader      = "X-Arcane-Signature"
	pairingFormatV1      = "arcane-pair-v1"
	pairingTokenLifetime = 5 * time.Minute
	maxOutboxAttempts    = 10
	outboxBatchSize      = 50
	maxTitleLen          = 120
	maxBodyLen           = 500
)

type ApnsService struct {
	db         *database.DB
	config     *config.Config
	settings   *settings.SettingsService
	roles      *role.RoleService
	events     *event.EventService
	httpClient *http.Client

	drainMu      sync.Mutex
	drainPending atomic.Bool
}

func NewApnsService(db *database.DB, cfg *config.Config, settingsService *settings.SettingsService, roleService *role.RoleService, eventService *event.EventService, httpClient *http.Client) *ApnsService {
	return &ApnsService{db: db, config: cfg, settings: settingsService, roles: roleService, events: eventService, httpClient: httpClient}
}

func (s *ApnsService) Enabled(ctx context.Context) bool {
	return s.config != nil && !s.config.AgentMode && s.settings.GetBoolSetting(ctx, "apnsEnabled", false)
}

type signerInternal struct {
	algorithm string
	sign      func([]byte) ([]byte, error)
	publicKey []byte
}

func (s *ApnsService) signerInternal(ctx context.Context) (*signerInternal, error) {
	stored := s.settings.GetStringSetting(ctx, "apnsSigningKey", "")
	algorithm := strings.ToLower(s.config.ApnsKeyAlgorithm)
	var seed []byte
	if stored != "" {
		decrypted, err := crypto.Decrypt(stored)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to decrypt push signing key")
		}
		alg, encoded, ok := strings.Cut(decrypted, ":")
		if !ok {
			return nil, errors.New("push signing key is malformed")
		}
		seed, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to decode push signing key")
		}
		algorithm = alg
	} else {
		seed = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, seed); err != nil {
			return nil, errors.WrapIf(err, "failed to generate push signing key")
		}
		encrypted, err := crypto.Encrypt(algorithm + ":" + base64.StdEncoding.EncodeToString(seed))
		if err != nil {
			return nil, errors.WrapIf(err, "failed to encrypt push signing key")
		}
		if err := s.settings.UpdateSetting(ctx, "apnsSigningKey", encrypted); err != nil {
			return nil, err
		}
	}

	switch algorithm {
	case "ed25519":
		key := ed25519.NewKeyFromSeed(seed)
		return &signerInternal{
			algorithm: algorithm,
			publicKey: []byte(key[ed25519.SeedSize:]),
			sign:      func(msg []byte) ([]byte, error) { return ed25519.Sign(key, msg), nil },
		}, nil
	case "ml-dsa-87":
		key, err := mldsa.NewPrivateKey(mldsa.MLDSA87(), seed)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to load push signing key")
		}
		return &signerInternal{
			algorithm: algorithm,
			publicKey: key.PublicKey().Bytes(),
			sign:      func(msg []byte) ([]byte, error) { return key.Sign(nil, msg, nil) },
		}, nil
	default:
		return nil, errors.Errorf("unsupported push signing key algorithm %q", algorithm)
	}
}

func (s *ApnsService) relayRequestInternal(ctx context.Context, method, path string, body []byte, signer *signerInternal) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.config.ApnsRelayUrl+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.WrapIf(err, "failed to build push relay request")
	}
	req.Header.Set("Content-Type", "application/json")
	if signer != nil {
		sig, err := signer.sign(body)
		if err != nil {
			return 0, nil, errors.WrapIf(err, "failed to sign push relay request")
		}
		req.Header.Set(signatureHeader, base64.StdEncoding.EncodeToString(sig))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, nil, errors.WrapIf(common.ErrApnsRelay, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return 0, nil, errors.WrapIf(common.ErrApnsRelay, err.Error())
	}
	return resp.StatusCode, respBody, nil
}

func (s *ApnsService) EnsureChannel(ctx context.Context) (string, error) {
	if !s.Enabled(ctx) {
		return "", common.ErrApnsDisabled
	}
	if channelID := s.settings.GetStringSetting(ctx, "apnsChannelId", ""); channelID != "" {
		return channelID, nil
	}
	signer, err := s.signerInternal(ctx)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]string{"algorithm": signer.algorithm, "publicKey": base64.StdEncoding.EncodeToString(signer.publicKey)})
	if err != nil {
		return "", errors.WrapIf(err, "failed to marshal channel registration")
	}
	status, respBody, err := s.relayRequestInternal(ctx, http.MethodPost, "/v1/channels", body, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return "", errors.WrapIff(common.ErrApnsRelay, "channel registration returned %d: %s", status, strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		ChannelID string `json:"channelId"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || parsed.ChannelID == "" {
		return "", errors.WrapIf(common.ErrApnsRelay, "channel registration returned no channel id")
	}
	if err := s.settings.UpdateSetting(ctx, "apnsChannelId", parsed.ChannelID); err != nil {
		return "", err
	}
	slog.InfoContext(ctx, "Registered push relay channel", "channelId", parsed.ChannelID, "algorithm", signer.algorithm)
	return parsed.ChannelID, nil
}

func (s *ApnsService) ApplyEnabledUpdates(ctx context.Context, updates []libarcane.SettingUpdate, rescheduleOutbox func() error) {
	if s == nil {
		return
	}
	for _, update := range updates {
		if update.Value == "true" {
			if _, err := s.EnsureChannel(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to register push relay channel", "error", err)
			}
			continue
		}
		if err := s.RevokeChannel(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to revoke push relay channel", "error", err)
		}
	}
	if err := rescheduleOutbox(); err != nil {
		slog.WarnContext(ctx, "Failed to reschedule push outbox job", "error", err)
	}
}

func (s *ApnsService) RevokeChannel(ctx context.Context) error {
	channelID := s.settings.GetStringSetting(ctx, "apnsChannelId", "")
	if channelID != "" {
		signer, err := s.signerInternal(ctx)
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]any{"channelId": channelID, "issuedAt": time.Now().UTC(), "nonce": uuid.New().String()})
		if err != nil {
			return errors.WrapIf(err, "failed to marshal channel revocation")
		}
		status, respBody, err := s.relayRequestInternal(ctx, http.MethodDelete, "/v1/channels/"+channelID, body, signer)
		if err != nil {
			return err
		}
		if status != http.StatusNoContent && status != http.StatusNotFound && status != http.StatusGone {
			return errors.WrapIff(common.ErrApnsRelay, "channel revocation returned %d: %s", status, strings.TrimSpace(string(respBody)))
		}
		if err := s.settings.UpdateSetting(ctx, "apnsChannelId", ""); err != nil {
			return err
		}
		slog.InfoContext(ctx, "Revoked push relay channel", "channelId", channelID)
	}
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&Device{}).Error; err != nil {
		return errors.WrapIf(err, "failed to delete push devices")
	}
	return errors.WrapIf(s.db.WithContext(ctx).Where("1 = 1").Delete(&OutboxEntry{}).Error, "failed to clear push outbox")
}

func (s *ApnsService) IssuePairingToken(ctx context.Context) (apnstypes.PairingToken, error) {
	channelID, err := s.EnsureChannel(ctx)
	if err != nil {
		return apnstypes.PairingToken{}, err
	}
	signer, err := s.signerInternal(ctx)
	if err != nil {
		return apnstypes.PairingToken{}, err
	}
	now := time.Now()
	expiresAt := now.Add(pairingTokenLifetime)
	payload, err := json.Marshal(struct {
		ChannelID string `json:"channelId"`
		Subject   string `json:"sub"`
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Nonce     string `json:"nonce"`
	}{channelID, uuid.New().String(), now.Unix(), expiresAt.Unix(), uuid.New().String()})
	if err != nil {
		return apnstypes.PairingToken{}, errors.WrapIf(err, "failed to marshal pairing token")
	}
	sig, err := signer.sign(payload)
	if err != nil {
		return apnstypes.PairingToken{}, errors.WrapIf(err, "failed to sign pairing token")
	}
	token := pairingFormatV1 + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return apnstypes.PairingToken{Token: token, ChannelID: channelID, ExpiresAt: expiresAt}, nil
}

func deviceDTOInternal(d Device) apnstypes.Device {
	events := d.Events
	if events == nil {
		events = map[string]bool{}
	}
	envs := []string(d.EnvironmentIDs)
	if envs == nil {
		envs = []string{}
	}
	return apnstypes.Device{ID: d.ID, Label: d.Label, Events: events, EnvironmentIDs: envs, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt}
}

func (s *ApnsService) Status(ctx context.Context, userID string) (apnstypes.Status, error) {
	status := apnstypes.Status{Enabled: s.Enabled(ctx), RelayURL: s.config.ApnsRelayUrl, Devices: []apnstypes.Device{}}
	if status.Enabled {
		status.ChannelID = s.settings.GetStringSetting(ctx, "apnsChannelId", "")
	}
	var devices []Device
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at ASC").Find(&devices).Error; err != nil {
		return status, errors.WrapIf(err, "failed to list push devices")
	}
	for _, d := range devices {
		status.Devices = append(status.Devices, deviceDTOInternal(d))
	}
	return status, nil
}

func (s *ApnsService) RegisterDevice(ctx context.Context, userID string, req apnstypes.RegisterDeviceRequest) (apnstypes.Device, error) {
	if err := normalization.Normalize(&req); err != nil {
		return apnstypes.Device{}, err
	}
	if !s.Enabled(ctx) {
		return apnstypes.Device{}, common.ErrApnsDisabled
	}
	events := req.Events
	if events == nil {
		events = map[string]bool{}
		for _, t := range []notifications.NotificationEventType{notifications.NotificationEventImageUpdate, notifications.NotificationEventContainerUpdate, notifications.NotificationEventVulnerabilityFound, notifications.NotificationEventPruneReport, notifications.NotificationEventAutoHeal} {
			events[string(t)] = true
		}
	}
	environmentIDs := database.StringSlice(req.EnvironmentIDs)
	if environmentIDs == nil {
		environmentIDs = database.StringSlice{}
	}
	now := time.Now()
	var existing Device
	err := s.db.WithContext(ctx).Where("recipient_id = ?", req.RecipientID).First(&existing).Error
	switch {
	case err == nil && existing.UserID != userID:
		return apnstypes.Device{}, common.ErrApnsDeviceConflict
	case err == nil:
		existing.Label = req.Label
		existing.Events = events
		existing.EnvironmentIDs = environmentIDs
		existing.LastSeenAt = &now
		if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return apnstypes.Device{}, errors.WrapIf(err, "failed to update push device")
		}
		return deviceDTOInternal(existing), nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return apnstypes.Device{}, errors.WrapIf(err, "failed to load push device")
	}
	device := Device{UserID: userID, RecipientID: req.RecipientID, Label: req.Label, Events: events, EnvironmentIDs: environmentIDs, LastSeenAt: &now}
	if err := s.db.WithContext(ctx).Create(&device).Error; err != nil {
		return apnstypes.Device{}, errors.WrapIf(err, "failed to register push device")
	}
	return deviceDTOInternal(device), nil
}

func (s *ApnsService) deviceInternal(ctx context.Context, userID, id string) (*Device, error) {
	var device Device
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrApnsDeviceNotFound
		}
		return nil, errors.WrapIf(err, "failed to load push device")
	}
	return &device, nil
}

func (s *ApnsService) UpdateDevice(ctx context.Context, userID, id string, req apnstypes.UpdateDeviceRequest) (apnstypes.Device, error) {
	if err := normalization.Normalize(&req); err != nil {
		return apnstypes.Device{}, err
	}
	device, err := s.deviceInternal(ctx, userID, id)
	if err != nil {
		return apnstypes.Device{}, err
	}
	if req.Label != nil {
		device.Label = *req.Label
	}
	if req.Events != nil {
		device.Events = *req.Events
	}
	if req.EnvironmentIDs != nil {
		device.EnvironmentIDs = database.StringSlice(*req.EnvironmentIDs)
		if device.EnvironmentIDs == nil {
			device.EnvironmentIDs = database.StringSlice{}
		}
	}
	device.LastSeenAt = new(time.Now())
	if err := s.db.WithContext(ctx).Save(device).Error; err != nil {
		return apnstypes.Device{}, errors.WrapIf(err, "failed to update push device")
	}
	return deviceDTOInternal(*device), nil
}

func (s *ApnsService) DeleteDevice(ctx context.Context, userID, id string) error {
	device, err := s.deviceInternal(ctx, userID, id)
	if err != nil {
		return err
	}
	return errors.WrapIf(s.db.WithContext(ctx).Delete(device).Error, "failed to delete push device")
}

func (s *ApnsService) TestDevice(ctx context.Context, userID, id string) error {
	device, err := s.deviceInternal(ctx, userID, id)
	if err != nil {
		return err
	}
	channelID, err := s.EnsureChannel(ctx)
	if err != nil {
		return err
	}
	signer, err := s.signerInternal(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"channelId":   channelID,
		"issuedAt":    time.Now().UTC(),
		"nonce":       strings.ReplaceAll(uuid.New().String(), "-", ""),
		"recipientId": device.RecipientID,
	})
	if err != nil {
		return errors.WrapIf(err, "failed to marshal test push")
	}
	status, respBody, err := s.relayRequestInternal(ctx, http.MethodPost, "/v1/test", body, signer)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return errors.WrapIff(common.ErrApnsRelay, "test push returned %d: %s", status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func truncateInternal(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return string(r[:limit-1]) + "…"
}

func (s *ApnsService) Enqueue(ctx context.Context, environmentID, environmentName string, eventType notifications.NotificationEventType, subject string, metadata database.JSON) error {
	if !s.Enabled(ctx) {
		return nil
	}
	recipients, err := s.recipientsInternal(ctx, environmentID, eventType)
	if err != nil || len(recipients) == 0 {
		return err
	}

	batchCount := 0
	if batch, _ := metadata["batch"].(bool); batch {
		switch n := metadata["updateCount"].(type) {
		case int:
			batchCount = n
		case float64:
			batchCount = int(n)
		}
	}
	title, body, severity := "Arcane notification", subject, "info"
	route := apnstypes.Route{Kind: "environment", EnvironmentID: environmentID}
	switch eventType {
	case notifications.NotificationEventImageUpdate:
		title, body = "Image update available", fmt.Sprintf("%s in %s", subject, environmentName)
		if batchCount > 1 {
			title = fmt.Sprintf("%d image updates available", batchCount)
		}
	case notifications.NotificationEventContainerUpdate:
		title, body = "Container updated", fmt.Sprintf("%s in %s", subject, environmentName)
		if batchCount > 1 {
			title = fmt.Sprintf("%d containers updated", batchCount)
		}
	case notifications.NotificationEventVulnerabilityFound:
		title, severity = "Vulnerability found", "warning"
		body = fmt.Sprintf("%v in %s (%s)", metadata["cveId"], subject, environmentName)
	case notifications.NotificationEventPruneReport:
		title, body = "Prune completed", "System prune finished in "+environmentName
	case notifications.NotificationEventAutoHeal:
		title, severity = "Container restarted", "warning"
		body = fmt.Sprintf("%s was restarted by auto-heal in %s", subject, environmentName)
		if containerID, _ := metadata["containerID"].(string); containerID != "" {
			route = apnstypes.Route{Kind: "container", EnvironmentID: environmentID, ID: containerID}
		}
	}
	envelope := apnstypes.Envelope{
		Version:         1,
		EventID:         uuid.New().String(),
		OccurredAt:      time.Now().UTC(),
		Type:            string(eventType),
		Severity:        severity,
		EnvironmentID:   environmentID,
		EnvironmentName: truncateInternal(environmentName, 100),
		Title:           truncateInternal(title, maxTitleLen),
		Body:            truncateInternal(body, maxBodyLen),
		Route:           route,
		RecipientIDs:    recipients,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return errors.WrapIf(err, "failed to marshal push envelope")
	}
	entry := OutboxEntry{EventID: envelope.EventID, Envelope: string(raw), NextAttemptAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(&entry).Error; err != nil {
		return errors.WrapIf(err, "failed to enqueue push notification")
	}
	go func() {
		drainCtx := context.WithoutCancel(ctx)
		if err := s.DrainOutbox(drainCtx); err != nil {
			slog.WarnContext(drainCtx, "Push outbox drain failed", "error", err)
		}
	}()
	return nil
}

func (s *ApnsService) recipientsInternal(ctx context.Context, environmentID string, eventType notifications.NotificationEventType) ([]string, error) {
	var devices []Device
	if err := s.db.WithContext(ctx).Find(&devices).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list push devices")
	}
	permissions := map[string]*authz.PermissionSet{}
	var recipients []string
	for _, d := range devices {
		if !d.Events[string(eventType)] {
			continue
		}
		if len(d.EnvironmentIDs) > 0 && !slices.Contains(d.EnvironmentIDs, environmentID) {
			continue
		}
		ps, ok := permissions[d.UserID]
		if !ok {
			resolved, err := s.roles.ResolveUserPermissionsInDB(ctx, s.db.WithContext(ctx), d.UserID)
			if err != nil {
				slog.WarnContext(ctx, "Skipping push device: cannot resolve permissions", "userId", d.UserID, "error", err)
				continue
			}
			ps = resolved
			permissions[d.UserID] = ps
		}
		if !ps.Allows(authz.PermContainersList, environmentID) {
			continue
		}
		recipients = append(recipients, d.RecipientID)
	}
	return recipients, nil
}

func (s *ApnsService) DrainOutbox(ctx context.Context) error {
	s.drainPending.Store(true)
	if !s.drainMu.TryLock() {
		return nil
	}
	defer s.drainMu.Unlock()
	for s.drainPending.Swap(false) {
		if err := s.drainOutboxInternal(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *ApnsService) drainOutboxInternal(ctx context.Context) error {
	if !s.Enabled(ctx) {
		return nil
	}
	var entries []OutboxEntry
	if err := s.db.WithContext(ctx).Where("next_attempt_at <= ?", time.Now()).Order("next_attempt_at ASC").Limit(outboxBatchSize).Find(&entries).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load push outbox", "error", err)
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	channelID, err := s.EnsureChannel(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Push outbox drain skipped", "error", err)
		return err
	}
	signer, err := s.signerInternal(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Push outbox drain skipped", "error", err)
		return err
	}
	for _, entry := range entries {
		if channelRevoked := s.sendOutboxEntryInternal(ctx, entry, channelID, signer); channelRevoked {
			return errors.New("push channel revoked")
		}
	}
	return nil
}

func (s *ApnsService) sendOutboxEntryInternal(ctx context.Context, entry OutboxEntry, channelID string, signer *signerInternal) bool {
	var envelope apnstypes.Envelope
	if err := json.Unmarshal([]byte(entry.Envelope), &envelope); err != nil {
		s.finishOutboxInternal(ctx, entry, envelope, "invalid envelope: "+err.Error())
		return false
	}
	envelope.ChannelID = channelID
	envelope.OccurredAt = time.Now().UTC()
	raw, err := json.Marshal(envelope)
	if err != nil {
		s.finishOutboxInternal(ctx, entry, envelope, "marshal: "+err.Error())
		return false
	}
	status, respBody, err := s.relayRequestInternal(ctx, http.MethodPost, "/v1/events", raw, signer)
	switch {
	case err == nil && (status == http.StatusAccepted || status == http.StatusOK):
		var parsed struct {
			UnknownRecipientIDs []string `json:"unknownRecipientIds"`
		}
		if json.Unmarshal(respBody, &parsed) == nil && len(parsed.UnknownRecipientIDs) > 0 {
			if err := s.db.WithContext(ctx).Where("recipient_id IN ?", parsed.UnknownRecipientIDs).Delete(&Device{}).Error; err != nil {
				slog.WarnContext(ctx, "Failed to prune stale push devices", "error", err)
			}
		}
		s.finishOutboxInternal(ctx, entry, envelope, "")
	case err == nil && status == http.StatusGone:
		if err := s.settings.UpdateSetting(ctx, "apnsChannelId", ""); err != nil {
			slog.WarnContext(ctx, "Failed to clear revoked push channel", "error", err)
		}
		if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&Device{}).Error; err != nil {
			slog.WarnContext(ctx, "Failed to remove push devices after channel revocation", "error", err)
		}
		s.finishOutboxInternal(ctx, entry, envelope, "relay channel revoked; devices must pair again")
		return true
	case err == nil && status >= 400 && status < 500 && status != http.StatusTooManyRequests:
		s.finishOutboxInternal(ctx, entry, envelope, fmt.Sprintf("relay returned %d: %s", status, strings.TrimSpace(string(respBody))))
	default:
		reason := fmt.Sprintf("relay returned %d", status)
		if err != nil {
			reason = err.Error()
		}
		s.retryOutboxInternal(ctx, entry, envelope, reason, min(time.Duration(1<<uint(entry.Attempts+1))*5*time.Second, 15*time.Minute))
	}
	return false
}

func (s *ApnsService) retryOutboxInternal(ctx context.Context, entry OutboxEntry, envelope apnstypes.Envelope, reason string, backoff time.Duration) {
	entry.Attempts++
	if entry.Attempts >= maxOutboxAttempts {
		s.finishOutboxInternal(ctx, entry, envelope, "gave up after "+reason)
		return
	}
	entry.NextAttemptAt = time.Now().Add(backoff)
	entry.LastError = new(reason)
	if err := s.db.WithContext(ctx).Save(&entry).Error; err != nil {
		slog.WarnContext(ctx, "Failed to reschedule push outbox entry", "error", err)
	}
}

func (s *ApnsService) finishOutboxInternal(ctx context.Context, entry OutboxEntry, envelope apnstypes.Envelope, failure string) {
	if err := s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
		slog.WarnContext(ctx, "Failed to delete push outbox entry", "error", err)
	}
	if failure == "" || s.events == nil {
		return
	}
	resourceType, resourceName := "notification", "mobile-push"
	envID := envelope.EnvironmentID
	if _, err := s.events.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeNotificationSend,
		Severity:      event.EventSeverityError,
		Title:         "Notification failed via mobile push",
		Description:   fmt.Sprintf("%s: %s", envelope.Title, failure),
		ResourceType:  &resourceType,
		ResourceName:  &resourceName,
		EnvironmentID: &envID,
		Metadata:      database.JSON{"provider": "mobile-push", "status": "failed", "eventType": envelope.Type},
	}); err != nil {
		slog.WarnContext(ctx, "Failed to log push failure event", "error", err)
	}
}
