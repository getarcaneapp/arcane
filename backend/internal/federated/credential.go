package federated

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	kit "go.getarcane.app/kit/pkg"
)

func normalizeCreateFederatedCredentialInternal(req federatedtypes.CreateFederatedCredential) (federatedtypes.CreateFederatedCredential, error) {
	if err := normalization.Normalize(&req); err != nil {
		return req, common.Classify(common.ErrFederatedCredentialInvalid, err)
	}
	req.IssuerURL = strings.TrimRight(strings.TrimSpace(req.IssuerURL), "/")
	req.SubjectClaim = strings.TrimSpace(req.SubjectClaim)
	req.SubjectMatch = strings.TrimSpace(req.SubjectMatch)
	req.MatchType = kit.Ternary(
		strings.EqualFold(strings.TrimSpace(req.MatchType), federatedtypes.MatchTypeGlob),
		federatedtypes.MatchTypeGlob,
		federatedtypes.MatchTypeExact,
	)
	req.Audiences = kit.Unique(kit.TrimNonEmpty(req.Audiences))
	req.EnvironmentID = mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(req.EnvironmentID).OrEmpty())).ToPointer()
	req.TokenTTLSeconds = auth.ClampFederatedTokenTTLSeconds(req.TokenTTLSeconds)

	req.SubjectClaim = cmp.Or(req.SubjectClaim, defaultFederatedSubjectClaim)
	if req.SubjectMatch == "" || req.RoleID == "" || len(req.Audiences) == 0 {
		return req, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
	}
	if err := validateIssuerURLInternal(req.IssuerURL); err != nil {
		return req, err
	}
	if err := validateSubjectMatchInternal(req.MatchType, req.SubjectMatch); err != nil {
		return req, err
	}
	return req, nil
}

func applyFederatedCredentialUpdateInternal(existing FederatedCredential, req federatedtypes.UpdateFederatedCredential) (FederatedCredential, bool, error) {
	if err := normalization.Normalize(&req); err != nil {
		return existing, false, common.Classify(common.ErrFederatedCredentialInvalid, err)
	}
	utils.ApplyChanged(&existing.Name, mo.PointerToOption(req.Name))
	if req.Description != nil {
		existing.Description = req.Description
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.IssuerURL != nil {
		issuerURL := strings.TrimRight(strings.TrimSpace(*req.IssuerURL), "/")
		if err := validateIssuerURLInternal(issuerURL); err != nil {
			return existing, false, err
		}
		existing.IssuerURL = issuerURL
	}
	if req.Audiences != nil {
		audiences := kit.Unique(kit.TrimNonEmpty(req.Audiences))
		if len(audiences) == 0 {
			return existing, false, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
		}
		existing.Audiences = audiences
	}
	if req.SubjectClaim != nil {
		subjectClaim := cmp.Or(strings.TrimSpace(*req.SubjectClaim), defaultFederatedSubjectClaim)
		existing.SubjectClaim = subjectClaim
	}
	if req.SubjectMatch != nil {
		subjectMatch := strings.TrimSpace(*req.SubjectMatch)
		if subjectMatch == "" {
			return existing, false, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
		}
		existing.SubjectMatch = subjectMatch
	}
	if req.MatchType != nil {
		existing.MatchType = kit.Ternary(
			strings.EqualFold(strings.TrimSpace(*req.MatchType), federatedtypes.MatchTypeGlob),
			federatedtypes.MatchTypeGlob,
			federatedtypes.MatchTypeExact,
		)
	}
	if err := validateSubjectMatchInternal(existing.MatchType, existing.SubjectMatch); err != nil {
		return existing, false, err
	}

	roleChanged := false
	if req.RoleID != nil {
		roleID := strings.TrimSpace(*req.RoleID)
		if roleID == "" {
			return existing, false, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
		}
		roleChanged = roleID != existing.RoleID
		existing.RoleID = roleID
	}
	if req.EnvironmentID != nil {
		environmentID := mo.EmptyableToOption(strings.TrimSpace(*req.EnvironmentID)).ToPointer()
		roleChanged = roleChanged || mo.PointerToOption(existing.EnvironmentID).OrEmpty() != mo.PointerToOption(environmentID).OrEmpty()
		existing.EnvironmentID = environmentID
	}
	if req.TokenTTLSeconds != nil {
		existing.TokenTTLSeconds = auth.ClampFederatedTokenTTLSeconds(*req.TokenTTLSeconds)
	}
	if req.ExpiresAt != nil {
		existing.ExpiresAt = req.ExpiresAt
	}
	return existing, roleChanged, nil
}

func validateIssuerURLInternal(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Scheme != "https" {
		return common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential: issuerUrl must be an HTTPS URL"))
	}
	return nil
}

func validateSubjectMatchInternal(matchType, subjectMatch string) error {
	if strings.TrimSpace(subjectMatch) == "" || strings.EqualFold(strings.TrimSpace(matchType), federatedtypes.MatchTypeGlob) && strings.TrimSpace(subjectMatch) == "*" {
		return common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
	}
	return nil
}

func (s *FederatedCredentialService) validateRoleGrantAgainstUserInternal(ctx context.Context, userID, roleID string, environmentID *string) error {
	if s.roleService == nil || strings.TrimSpace(userID) == "" {
		return nil
	}

	user, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user for federated role validation: %w", err)
	}
	permissions, err := s.roleService.ResolvePermissions(ctx, user)
	if err != nil {
		return fmt.Errorf("resolve user permissions: %w", err)
	}
	if err := s.roleService.ValidateRoleAssignmentAgainstCaller(ctx, permissions, roleID, environmentID); err != nil {
		if errors.Is(err, common.ErrRolePermissionEscalation) {
			return common.Classify(common.ErrFederatedCredentialPermissionEscalation, fmt.Errorf("cannot map a federated credential to a role you do not hold: %w", err))
		}
		return common.Classify(common.ErrFederatedCredentialInvalid, fmt.Errorf("invalid federated credential: %w", err))
	}
	return nil
}

func toFederatedCredentialDTOInternal(credential *FederatedCredential) federatedtypes.FederatedCredential {
	if credential == nil {
		return federatedtypes.FederatedCredential{}
	}
	dto := federatedtypes.FederatedCredential{
		ID:              credential.ID,
		Name:            credential.Name,
		Description:     credential.Description,
		Enabled:         credential.Enabled,
		IssuerURL:       credential.IssuerURL,
		Audiences:       []string(credential.Audiences),
		SubjectClaim:    credential.SubjectClaim,
		SubjectMatch:    credential.SubjectMatch,
		MatchType:       credential.MatchType,
		RoleID:          credential.RoleID,
		EnvironmentID:   credential.EnvironmentID,
		IdentityUserID:  credential.IdentityUserID,
		TokenTTLSeconds: credential.TokenTTLSeconds,
		LastUsedAt:      credential.LastUsedAt,
		ExpiresAt:       credential.ExpiresAt,
		CreatedAt:       credential.CreatedAt,
		UpdatedAt:       credential.UpdatedAt,
	}
	if credential.IdentityUser != nil {
		dto.ServiceUsername = credential.IdentityUser.Username
	}
	if credential.Role != nil {
		dto.RoleName = credential.Role.Name
	}
	if credential.Environment != nil {
		dto.EnvironmentName = credential.Environment.Name
	}
	return dto
}
