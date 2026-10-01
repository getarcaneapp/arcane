package federated

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/jwtclaims"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
)

func (s *FederatedCredentialService) ExchangeToken(ctx context.Context, req federatedtypes.TokenExchangeRequest) (*federatedtypes.FederatedTokenResponse, error) {
	claims := jwtclaims.ParseJWTClaims(req.SubjectToken)
	issuer := ""
	subject := ""
	var audiences []string
	if claims != nil {
		issuer = kit.ToString(jwtclaims.GetByPath(claims, "iss").OrEmpty())
		subject = kit.ToString(jwtclaims.GetByPath(claims, "sub").OrEmpty())
		audiences = kit.Unique(kit.TrimNonEmpty(kit.Collect(jwtclaims.GetByPath(claims, "aud").OrEmpty(), func(item any) string { return kit.As(item, "") })))
	}

	logResult := "failure"
	logReason := ""
	var matchedCredential *FederatedCredential
	var matchedUser *common.User
	defer func() {
		s.logExchangeInternal(ctx, logResult, logReason, issuer, subject, audiences, matchedCredential, matchedUser)
	}()

	if req.GrantType != federatedtypes.TokenExchangeGrantType || strings.TrimSpace(req.SubjectToken) == "" {
		logReason = "invalid_request"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidRequest, errors.New("invalid federated token exchange request"))
	}
	switch req.SubjectTokenType {
	case federatedtypes.SubjectTokenTypeJWT, federatedtypes.SubjectTokenTypeIDToken:
	default:
		logReason = "invalid_request"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidRequest, errors.New("invalid federated token exchange request"))
	}
	if req.RequestedTokenType != "" && req.RequestedTokenType != federatedtypes.RequestedTokenTypeAccessJWT {
		logReason = "invalid_request"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidRequest, errors.New("invalid federated token exchange request"))
	}
	if issuer == "" {
		logReason = "missing_issuer"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, errors.New("invalid federated token grant"))
	}

	var credentials []FederatedCredential
	if err := s.db.WithContext(ctx).
		Where("issuer_url = ? AND enabled = ?", issuer, true).
		Order("created_at ASC").
		Order("id ASC").
		Find(&credentials).Error; err != nil {
		logReason = "credential_lookup_failed"
		return nil, fmt.Errorf("failed to list federated credentials for issuer: %w", err)
	}
	now := time.Now()
	active := credentials[:0]
	for _, credential := range credentials {
		if credential.ExpiresAt == nil || !now.After(*credential.ExpiresAt) {
			active = append(active, credential)
		}
	}
	credentials = active
	if len(credentials) == 0 {
		logReason = "issuer_not_allowed"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, errors.New("invalid federated token grant"))
	}

	verifiedToken, verifiedClaims, err := s.verifySubjectTokenInternal(ctx, issuer, req.SubjectToken)
	if err != nil {
		logReason = "token_verification_failed"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, fmt.Errorf("invalid federated token grant: %w", err))
	}
	if subject == "" {
		subject = kit.ToString(jwtclaims.GetByPath(verifiedClaims, defaultFederatedSubjectClaim).OrEmpty())
	}
	if len(audiences) == 0 {
		audiences = append([]string{}, verifiedToken.Audience...)
	}

	credential := selectMatchingCredentialInternal(credentials, verifiedToken.Audience, verifiedClaims)
	if credential == nil {
		logReason = "no_matching_credential"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, errors.New("invalid federated token grant"))
	}
	matchedCredential = credential
	if err := s.recordTokenReplayGuardInternal(ctx, issuer, req.SubjectToken, verifiedClaims, verifiedToken.Expiry); err != nil {
		logReason = "token_replay_rejected"
		return nil, err
	}

	user, err := s.userService.GetUserByID(ctx, credential.IdentityUserID)
	if err != nil {
		logReason = "identity_user_missing"
		return nil, common.Classify(common.ErrFederatedCredentialInvalidGrant, fmt.Errorf("invalid federated token grant: %w", err))
	}
	matchedUser = user

	tokenPair, err := s.authService.IssueFederatedToken(ctx, user, credential.ID, credential.TokenTTLSeconds)
	if err != nil {
		logReason = "token_issue_failed"
		return nil, err
	}

	go func() {
		bgCtx := context.WithoutCancel(ctx)
		now := time.Now()
		cutoff := now.Add(-federatedCredentialLastUsedWriteWindow)
		if err := s.db.WithContext(bgCtx).
			Model(&FederatedCredential{}).
			Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", credential.ID, cutoff).
			Update("last_used_at", now).Error; err != nil {
			slog.WarnContext(bgCtx, "failed to update federated credential last_used_at", "credential_id", credential.ID, "error", err)
		}
	}()

	logResult = "success"
	logReason = "matched"
	return &federatedtypes.FederatedTokenResponse{
		AccessToken:     tokenPair.AccessToken,
		TokenType:       "Bearer",
		ExpiresIn:       max(int(time.Until(tokenPair.ExpiresAt).Seconds()), 0),
		IssuedTokenType: federatedtypes.IssuedTokenTypeAccessToken,
	}, nil
}

func (s *FederatedCredentialService) verifySubjectTokenInternal(ctx context.Context, issuer, rawToken string) (*oidc.IDToken, map[string]any, error) {
	keySet, err := s.keySetForIssuerInternal(ctx, issuer)
	if err != nil {
		return nil, nil, err
	}

	providerCtx := oidc.ClientContext(ctx, s.httpClient)
	verifier := oidc.NewVerifier(issuer, keySet, &oidc.Config{
		SkipClientIDCheck:    true,
		SupportedSigningAlgs: oidcjwk.SupportedSigningAlgs(),
	})
	idToken, err := verifier.Verify(providerCtx, rawToken)
	if err != nil {
		return nil, nil, err
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, nil, err
	}
	return idToken, claims, nil
}

func (s *FederatedCredentialService) recordTokenReplayGuardInternal(ctx context.Context, issuer, rawToken string, claims map[string]any, expiresAt time.Time) error {
	if expiresAt.IsZero() || time.Now().After(expiresAt) {
		return common.Classify(common.ErrFederatedCredentialInvalidGrant, errors.New("invalid federated token grant"))
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).
		Where("expires_at < ?", now).
		Delete(&FederatedTokenReplay{}).Error; err != nil {
		return fmt.Errorf("failed to prune federated token replay records: %w", err)
	}

	tokenID := strings.TrimSpace(kit.ToString(jwtclaims.GetByPath(claims, "jti").OrEmpty()))
	tokenKind := "jti"
	if tokenID == "" {
		tokenID = rawToken
		tokenKind = "token"
	}
	replay := FederatedTokenReplay{
		TokenHash: kit.SHA256Hex(issuer + "\x00" + tokenKind + "\x00" + tokenID),
		IssuerURL: issuer,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&replay).Error; err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "unique") || strings.Contains(message, "duplicate key") {
			return common.Classify(common.ErrFederatedCredentialInvalidGrant, errors.New("invalid federated token grant"))
		}
		return fmt.Errorf("failed to record federated token replay guard: %w", err)
	}
	return nil
}

func (s *FederatedCredentialService) keySetForIssuerInternal(ctx context.Context, issuer string) (oidc.KeySet, error) {
	s.providerMu.RLock()
	if keySet := s.keySets[issuer]; keySet != nil {
		s.providerMu.RUnlock()
		return keySet, nil
	}
	s.providerMu.RUnlock()

	value, err, _ := s.providerGroup.Do(issuer, func() (any, error) {
		providerCtx := oidc.ClientContext(context.WithoutCancel(ctx), s.httpClient)
		provider, err := oidc.NewProvider(providerCtx, issuer)
		if err != nil {
			return nil, fmt.Errorf("failed to discover federated issuer: %w", err)
		}

		var metadata struct {
			JWKSURL string `json:"jwks_uri"`
		}
		if err := provider.Claims(&metadata); err != nil {
			return nil, fmt.Errorf("failed to read federated issuer metadata: %w", err)
		}
		if metadata.JWKSURL == "" {
			return nil, errors.New("federated issuer metadata is missing jwks_uri")
		}
		if s.keySetManager == nil {
			return nil, errors.New("JWK set manager is not configured")
		}

		keySet, err := s.keySetManager.KeySet(context.WithoutCancel(ctx), s.httpClient, metadata.JWKSURL)
		if err != nil {
			return nil, fmt.Errorf("failed to configure federated issuer JWK set: %w", err)
		}
		s.providerMu.Lock()
		s.keySets[issuer] = keySet
		s.providerMu.Unlock()
		return keySet, nil
	})
	if err != nil {
		return nil, err
	}

	keySet, ok := value.(oidc.KeySet)
	if !ok || keySet == nil {
		return nil, errors.New("federated issuer discovery returned invalid key set")
	}
	return keySet, nil
}

func selectMatchingCredentialInternal(credentials []FederatedCredential, tokenAudiences []string, claims map[string]any) *FederatedCredential {
	for i := range credentials {
		credential := &credentials[i]
		if credentialMatchesTokenInternal(credential, tokenAudiences, claims) {
			return credential
		}
	}
	return nil
}

func credentialMatchesTokenInternal(credential *FederatedCredential, tokenAudiences []string, claims map[string]any) bool {
	audiences := make(map[string]struct{}, len(credential.Audiences))
	for _, audience := range credential.Audiences {
		if audience = strings.TrimSpace(audience); audience != "" {
			audiences[audience] = struct{}{}
		}
	}
	audienceMatched := false
	for _, audience := range tokenAudiences {
		if _, audienceMatched = audiences[audience]; audienceMatched {
			break
		}
	}
	if !audienceMatched {
		return false
	}

	subjectClaim := cmp.Or(strings.TrimSpace(credential.SubjectClaim), defaultFederatedSubjectClaim)
	subject := kit.ToString(jwtclaims.GetByPath(claims, subjectClaim).OrEmpty())
	if subject == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(credential.MatchType), federatedtypes.MatchTypeGlob) {
		return subject == credential.SubjectMatch
	}

	var expression strings.Builder
	expression.WriteString("^")
	for _, character := range credential.SubjectMatch {
		switch character {
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteByte('.')
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteString("$")
	matched, err := regexp.MatchString(expression.String(), subject)
	return err == nil && matched
}

func (s *FederatedCredentialService) logExchangeInternal(ctx context.Context, result, reason, issuer, subject string, audiences []string, credential *FederatedCredential, user *common.User) {
	credentialID := ""
	credentialName := ""
	if credential != nil {
		credentialID = credential.ID
		credentialName = credential.Name
	}
	slog.InfoContext(ctx, "Federated credential token exchange",
		"result", result,
		"reason", reason,
		"issuer", issuer,
		"subject", subject,
		"audiences", audiences,
		"credential_id", credentialID,
	)

	if s.eventService == nil {
		return
	}

	metadata := database.JSON{
		"action":       "federated_token_exchange",
		"result":       result,
		"reason":       reason,
		"issuer":       issuer,
		"subject":      subject,
		"audiences":    audiences,
		"credentialId": credentialID,
	}

	userID := ""
	username := ""
	if user != nil {
		userID = user.ID
		username = user.Username
	}
	severity := event.EventSeverityInfo
	title := "Federated credential token exchange"
	if result != "success" {
		severity = event.EventSeverityWarning
		title = "Federated credential token exchange rejected"
	}

	go func() {
		bgCtx := context.WithoutCancel(ctx)
		_, err := s.eventService.CreateEvent(bgCtx, event.CreateEventRequest{
			Type:         event.EventTypeFederatedExchange,
			Severity:     severity,
			Title:        title,
			Description:  "Workload identity federation token exchange",
			ResourceType: mo.EmptyableToOption(strings.TrimSpace("federated_credential")).ToPointer(),
			ResourceID:   mo.EmptyableToOption(strings.TrimSpace(credentialID)).ToPointer(),
			ResourceName: mo.EmptyableToOption(strings.TrimSpace(credentialName)).ToPointer(),
			UserID:       mo.EmptyableToOption(strings.TrimSpace(userID)).ToPointer(),
			Username:     mo.EmptyableToOption(strings.TrimSpace(username)).ToPointer(),
			Metadata:     metadata,
		})
		if err != nil {
			slog.WarnContext(bgCtx, "failed to audit federated credential token exchange", "error", err)
		}
	}()
}
