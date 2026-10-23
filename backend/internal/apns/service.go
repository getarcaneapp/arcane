package apns

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
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

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/notifications"
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

	lifecycleCtx context.Context
	channelMu    sync.Mutex
	drainMu      sync.Mutex
	drainPending atomic.Bool
}

func NewApnsService(
	lifecycleCtx context.Context,
	db *database.DB,
	cfg *config.Config,
	settingsService *settings.SettingsService,
	roleService *role.RoleService,
	eventService *event.EventService,
	httpClient *http.Client,
) *ApnsService {
	return &ApnsService{
		db:           db,
		config:       cfg,
		settings:     settingsService,
		roles:        roleService,
		events:       eventService,
		httpClient:   httpClient,
		lifecycleCtx: lifecycleCtx,
	}
}

func (s *ApnsService) Enabled(ctx context.Context) bool {
	return s.config != nil && !s.config.EdgeAgent && s.settings.GetBoolSetting(ctx, "apnsEnabled")
}

type relaySigner struct {
	algorithm string
	sign      func([]byte) ([]byte, error)
	publicKey []byte
}

func (s *ApnsService) signer(ctx context.Context) (*relaySigner, error) {
	stored := s.settings.GetStringSetting(ctx, "apnsSigningKey")
	algorithm := strings.ToLower(s.config.ApnsKeyAlgorithm)
	var seed []byte
	if stored != "" {
		decrypted, err := crypto.Decrypt(stored)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt push signing key: %w", err)
		}
		alg, encoded, ok := strings.Cut(decrypted, ":")
		if !ok {
			return nil, errors.New("push signing key is malformed")
		}
		seed, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("failed to decode push signing key: %w", err)
		}
		algorithm = alg
	} else {
		seed = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, seed); err != nil {
			return nil, fmt.Errorf("failed to generate push signing key: %w", err)
		}
		encrypted, err := crypto.Encrypt(algorithm + ":" + base64.StdEncoding.EncodeToString(seed))
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt push signing key: %w", err)
		}
		if updateSettingErr := s.settings.UpdateSetting(ctx, "apnsSigningKey", encrypted); updateSettingErr != nil {
			return nil, updateSettingErr
		}
	}

	switch algorithm {
	case "ed25519":
		key := ed25519.NewKeyFromSeed(seed)
		return &relaySigner{
			algorithm: algorithm,
			publicKey: []byte(key[ed25519.SeedSize:]),
			sign:      func(msg []byte) ([]byte, error) { return ed25519.Sign(key, msg), nil },
		}, nil
	case "ml-dsa-87":
		key, err := mldsa.NewPrivateKey(mldsa.MLDSA87(), seed)
		if err != nil {
			return nil, fmt.Errorf("failed to load push signing key: %w", err)
		}
		return &relaySigner{
			algorithm: algorithm,
			publicKey: key.PublicKey().Bytes(),
			sign:      func(msg []byte) ([]byte, error) { return key.Sign(nil, msg, nil) },
		}, nil
	default:
		return nil, fmt.Errorf("unsupported push signing key algorithm %q", algorithm)
	}
}

func (s *ApnsService) relayRequest(ctx context.Context, method, path string, body []byte, signer *relaySigner) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.config.ApnsRelayUrl+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to build push relay request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if signer != nil {
		sig, signErr := signer.sign(body)
		if signErr != nil {
			return 0, nil, fmt.Errorf("failed to sign push relay request: %w", signErr)
		}
		req.Header.Set(signatureHeader, base64.StdEncoding.EncodeToString(sig))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", err.Error(), common.ErrApnsRelay)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", err.Error(), common.ErrApnsRelay)
	}
	return resp.StatusCode, respBody, nil
}

func (s *ApnsService) EnsureChannel(ctx context.Context) (string, error) {
	if !s.Enabled(ctx) {
		return "", common.ErrApnsDisabled
	}
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if channelID := s.settings.GetStringSetting(ctx, "apnsChannelId"); channelID != "" {
		return channelID, nil
	}
	signer, err := s.signer(ctx)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]string{"algorithm": signer.algorithm, "publicKey": base64.StdEncoding.EncodeToString(signer.publicKey)})
	if err != nil {
		return "", fmt.Errorf("failed to marshal channel registration: %w", err)
	}
	status, respBody, err := s.relayRequest(ctx, http.MethodPost, "/v1/channels", body, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return "", fmt.Errorf("channel registration returned %d: %s: %w", status, strings.TrimSpace(string(respBody)), common.ErrApnsRelay)
	}
	var parsed struct {
		ChannelID string `json:"channelId"`
	}
	if unmarshalErr := json.Unmarshal(respBody, &parsed); unmarshalErr != nil || parsed.ChannelID == "" {
		return "", fmt.Errorf("channel registration returned no channel id: %w", common.ErrApnsRelay)
	}
	if updateSettingErr := s.settings.UpdateSetting(ctx, "apnsChannelId", parsed.ChannelID); updateSettingErr != nil {
		return "", updateSettingErr
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
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	channelID := s.settings.GetStringSetting(ctx, "apnsChannelId")
	if channelID != "" {
		signer, err := s.signer(ctx)
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]any{"channelId": channelID, "issuedAt": time.Now().UTC(), "nonce": uuid.New().String()})
		if err != nil {
			return fmt.Errorf("failed to marshal channel revocation: %w", err)
		}
		status, respBody, err := s.relayRequest(ctx, http.MethodDelete, "/v1/channels/"+channelID, body, signer)
		if err != nil {
			return err
		}
		if status != http.StatusNoContent && status != http.StatusNotFound && status != http.StatusGone {
			return fmt.Errorf("channel revocation returned %d: %s: %w", status, strings.TrimSpace(string(respBody)), common.ErrApnsRelay)
		}
		if updateSettingErr := s.settings.UpdateSetting(ctx, "apnsChannelId", ""); updateSettingErr != nil {
			return updateSettingErr
		}
		slog.InfoContext(ctx, "Revoked push relay channel", "channelId", channelID)
	}
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&Device{}).Error; err != nil {
		return fmt.Errorf("failed to delete push devices: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&OutboxEntry{}).Error; err != nil {
		return fmt.Errorf("failed to clear push outbox: %w", err)
	}
	return nil
}

func (s *ApnsService) IssuePairingToken(ctx context.Context) (types.ApnsPairingToken, error) {
	channelID, err := s.EnsureChannel(ctx)
	if err != nil {
		return types.ApnsPairingToken{}, err
	}
	signer, err := s.signer(ctx)
	if err != nil {
		return types.ApnsPairingToken{}, err
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
		return types.ApnsPairingToken{}, fmt.Errorf("failed to marshal pairing token: %w", err)
	}
	sig, err := signer.sign(payload)
	if err != nil {
		return types.ApnsPairingToken{}, fmt.Errorf("failed to sign pairing token: %w", err)
	}
	token := pairingFormatV1 + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return types.ApnsPairingToken{Token: token, ChannelID: channelID, ExpiresAt: expiresAt}, nil
}

func deviceDTO(d Device) types.ApnsDevice {
	events := d.Events
	if events == nil {
		events = map[string]bool{}
	}
	envs := []string(d.EnvironmentIDs)
	if envs == nil {
		envs = []string{}
	}
	return types.ApnsDevice{ID: d.ID, Label: d.Label, Events: events, EnvironmentIDs: envs, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt}
}

func (s *ApnsService) Status(ctx context.Context, userID string) (types.ApnsStatus, error) {
	status := types.ApnsStatus{Enabled: s.Enabled(ctx), RelayURL: s.config.ApnsRelayUrl, Devices: []types.ApnsDevice{}}
	if status.Enabled {
		status.ChannelID = s.settings.GetStringSetting(ctx, "apnsChannelId")
	}
	var devices []Device
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at ASC").Find(&devices).Error; err != nil {
		return status, fmt.Errorf("failed to list push devices: %w", err)
	}
	for _, d := range devices {
		status.Devices = append(status.Devices, deviceDTO(d))
	}
	return status, nil
}

func (s *ApnsService) RegisterDevice(ctx context.Context, userID string, req types.ApnsRegisterDeviceRequest) (types.ApnsDevice, error) {
	if err := normalization.Normalize(&req); err != nil {
		return types.ApnsDevice{}, err
	}
	if !s.Enabled(ctx) {
		return types.ApnsDevice{}, common.ErrApnsDisabled
	}
	events := req.Events
	if events == nil {
		events = map[string]bool{}
		for _, t := range []notifications.NotificationEventType{
			notifications.NotificationEventImageUpdate,
			notifications.NotificationEventContainerUpdate,
			notifications.NotificationEventVulnerabilityFound,
			notifications.NotificationEventPruneReport,
			notifications.NotificationEventAutoHeal,
		} {
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
		return types.ApnsDevice{}, common.ErrApnsDeviceConflict
	case err == nil:
		existing.Label = req.Label
		existing.Events = events
		existing.EnvironmentIDs = environmentIDs
		existing.LastSeenAt = &now
		if updateDeviceErr := s.db.WithContext(ctx).Save(&existing).Error; updateDeviceErr != nil {
			return types.ApnsDevice{}, fmt.Errorf("failed to update push device: %w", updateDeviceErr)
		}
		return deviceDTO(existing), nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return types.ApnsDevice{}, fmt.Errorf("failed to load push device: %w", err)
	}
	device := Device{UserID: userID, RecipientID: req.RecipientID, Label: req.Label, Events: events, EnvironmentIDs: environmentIDs, LastSeenAt: &now}
	if createDeviceErr := s.db.WithContext(ctx).Create(&device).Error; createDeviceErr != nil {
		return types.ApnsDevice{}, fmt.Errorf("failed to register push device: %w", createDeviceErr)
	}
	return deviceDTO(device), nil
}

func (s *ApnsService) device(ctx context.Context, userID, id string) (*Device, error) {
	var device Device
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrApnsDeviceNotFound
		}
		return nil, fmt.Errorf("failed to load push device: %w", err)
	}
	return &device, nil
}

func (s *ApnsService) UpdateDevice(ctx context.Context, userID, id string, req types.ApnsDevice, submitted map[string]bool) (types.ApnsDevice, error) {
	if err := normalization.Normalize(&req); err != nil {
		return types.ApnsDevice{}, err
	}
	device, err := s.device(ctx, userID, id)
	if err != nil {
		return types.ApnsDevice{}, err
	}
	updated := Device{
		BaseModel:      device.BaseModel,
		UserID:         device.UserID,
		RecipientID:    device.RecipientID,
		Label:          kit.Ternary(submitted["label"], req.Label, device.Label),
		Events:         kit.Ternary(submitted["events"], req.Events, device.Events),
		EnvironmentIDs: append(database.StringSlice{}, kit.Ternary(submitted["environmentIds"], req.EnvironmentIDs, []string(device.EnvironmentIDs))...),
		LastSeenAt:     new(time.Now()),
	}
	if saveDeviceErr := s.db.WithContext(ctx).Save(&updated).Error; saveDeviceErr != nil {
		return types.ApnsDevice{}, fmt.Errorf("failed to update push device: %w", saveDeviceErr)
	}
	return deviceDTO(updated), nil
}

func (s *ApnsService) DeleteDevice(ctx context.Context, userID, id string) error {
	device, err := s.device(ctx, userID, id)
	if err != nil {
		return err
	}
	if deleteDeviceErr := s.db.WithContext(ctx).Delete(device).Error; deleteDeviceErr != nil {
		return fmt.Errorf("failed to delete push device: %w", deleteDeviceErr)
	}
	return nil
}

func (s *ApnsService) TestDevice(ctx context.Context, userID, id string) error {
	device, err := s.device(ctx, userID, id)
	if err != nil {
		return err
	}
	channelID, err := s.EnsureChannel(ctx)
	if err != nil {
		return err
	}
	signer, err := s.signer(ctx)
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
		return fmt.Errorf("failed to marshal test push: %w", err)
	}
	status, respBody, err := s.relayRequest(ctx, http.MethodPost, "/v1/test", body, signer)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return fmt.Errorf("test push returned %d: %s: %w", status, strings.TrimSpace(string(respBody)), common.ErrApnsRelay)
	}
	return nil
}

func truncate(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return string(r[:limit-1]) + "…"
}

// Enqueue queues a push for every subscribed device and reports whether one was
// queued. The outbox retries delivery to the relay, so a queued push counts as delivered.
func (s *ApnsService) Enqueue(ctx context.Context, environmentID, environmentName string, eventType notifications.NotificationEventType, subject string, metadata database.JSON) (bool, error) {
	if !s.Enabled(ctx) {
		return false, nil
	}
	recipients, err := s.recipients(ctx, environmentID, eventType)
	if err != nil || len(recipients) == 0 {
		return false, err
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
	route := types.ApnsRoute{Kind: "environment", EnvironmentID: environmentID}
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
			route = types.ApnsRoute{Kind: "container", EnvironmentID: environmentID, ID: containerID}
		}
	}
	envelope := types.ApnsEnvelope{
		Version:         1,
		EventID:         uuid.New().String(),
		OccurredAt:      time.Now().UTC(),
		Type:            string(eventType),
		Severity:        severity,
		EnvironmentID:   environmentID,
		EnvironmentName: truncate(environmentName, 100),
		Title:           truncate(title, maxTitleLen),
		Body:            truncate(body, maxBodyLen),
		Route:           route,
		RecipientIDs:    recipients,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return false, fmt.Errorf("failed to marshal push envelope: %w", err)
	}
	entry := OutboxEntry{EventID: envelope.EventID, Envelope: string(raw), NextAttemptAt: time.Now()}
	if enqueueNotificationErr := s.db.WithContext(ctx).Create(&entry).Error; enqueueNotificationErr != nil {
		return false, fmt.Errorf("failed to enqueue push notification: %w", enqueueNotificationErr)
	}
	go func() {
		if drainOutboxErr := s.DrainOutbox(s.lifecycleCtx); drainOutboxErr != nil && s.lifecycleCtx.Err() == nil {
			slog.WarnContext(s.lifecycleCtx, "Push outbox drain failed", "error", drainOutboxErr)
		}
	}()
	return true, nil
}

func (s *ApnsService) recipients(ctx context.Context, environmentID string, eventType notifications.NotificationEventType) ([]string, error) {
	var devices []Device
	if err := s.db.WithContext(ctx).Find(&devices).Error; err != nil {
		return nil, fmt.Errorf("failed to list push devices: %w", err)
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
	for s.drainPending.Load() {
		if !s.drainMu.TryLock() {
			return nil
		}
		for s.drainPending.Swap(false) {
			if err := s.drainOutbox(ctx); err != nil {
				s.drainMu.Unlock()
				return err
			}
		}
		s.drainMu.Unlock()
	}
	return nil
}

func (s *ApnsService) drainOutbox(ctx context.Context) error {
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
	signer, err := s.signer(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Push outbox drain skipped", "error", err)
		return err
	}
	for _, entry := range entries {
		if channelRevoked := s.sendOutboxEntry(ctx, entry, channelID, signer); channelRevoked {
			return errors.New("push channel revoked")
		}
	}
	return nil
}

func (s *ApnsService) sendOutboxEntry(ctx context.Context, entry OutboxEntry, channelID string, signer *relaySigner) bool {
	var envelope types.ApnsEnvelope
	if err := json.Unmarshal([]byte(entry.Envelope), &envelope); err != nil {
		s.finishOutbox(ctx, entry, envelope, "invalid envelope: "+err.Error())
		return false
	}
	envelope.ChannelID = channelID
	envelope.OccurredAt = time.Now().UTC()
	raw, err := json.Marshal(envelope)
	if err != nil {
		s.finishOutbox(ctx, entry, envelope, "marshal: "+err.Error())
		return false
	}
	status, respBody, err := s.relayRequest(ctx, http.MethodPost, "/v1/events", raw, signer)
	switch {
	case err == nil && (status == http.StatusAccepted || status == http.StatusOK):
		var parsed struct {
			UnknownRecipientIDs []string `json:"unknownRecipientIds"`
		}
		if json.Unmarshal(respBody, &parsed) == nil && len(parsed.UnknownRecipientIDs) > 0 {
			if deleteUnknownDevicesErr := s.db.WithContext(ctx).Where("recipient_id IN ?", parsed.UnknownRecipientIDs).Delete(&Device{}).Error; deleteUnknownDevicesErr != nil {
				slog.WarnContext(ctx, "Failed to prune stale push devices", "error", deleteUnknownDevicesErr)
			}
		}
		s.finishOutbox(ctx, entry, envelope, "")
	case err == nil && status == http.StatusGone:
		if updateSettingErr := s.settings.UpdateSetting(ctx, "apnsChannelId", ""); updateSettingErr != nil {
			slog.WarnContext(ctx, "Failed to clear revoked push channel", "error", updateSettingErr)
		}
		if deleteInvalidDevicesErr := s.db.WithContext(ctx).Where("1 = 1").Delete(&Device{}).Error; deleteInvalidDevicesErr != nil {
			slog.WarnContext(ctx, "Failed to remove push devices after channel revocation", "error", deleteInvalidDevicesErr)
		}
		s.finishOutbox(ctx, entry, envelope, "relay channel revoked; devices must pair again")
		return true
	case err == nil && status >= 400 && status < 500 && status != http.StatusTooManyRequests:
		s.finishOutbox(ctx, entry, envelope, fmt.Sprintf("relay returned %d: %s", status, strings.TrimSpace(string(respBody))))
	default:
		reason := fmt.Sprintf("relay returned %d", status)
		if err != nil {
			reason = err.Error()
		}
		s.retryOutbox(ctx, entry, envelope, reason, min(time.Duration(1<<uint(entry.Attempts+1))*5*time.Second, 15*time.Minute))
	}
	return false
}

func (s *ApnsService) retryOutbox(ctx context.Context, entry OutboxEntry, envelope types.ApnsEnvelope, reason string, backoff time.Duration) {
	entry.Attempts++
	if entry.Attempts >= maxOutboxAttempts {
		s.finishOutbox(ctx, entry, envelope, "gave up after "+reason)
		return
	}
	entry.NextAttemptAt = time.Now().Add(backoff)
	entry.LastError = new(reason)
	if err := s.db.WithContext(ctx).Save(&entry).Error; err != nil {
		slog.WarnContext(ctx, "Failed to reschedule push outbox entry", "error", err)
	}
}

func (s *ApnsService) finishOutbox(ctx context.Context, entry OutboxEntry, envelope types.ApnsEnvelope, failure string) {
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
