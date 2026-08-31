package federated

import (
	"context"
	"net/url"
	"strings"

	"emperror.dev/errors"
	"github.com/samber/mo"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
)

func normalizeCreateFederatedCredentialInternal(req federatedtypes.CreateFederatedCredential) (federatedtypes.CreateFederatedCredential, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.IssuerURL = strings.TrimRight(strings.TrimSpace(req.IssuerURL), "/")
	req.SubjectClaim = strings.TrimSpace(req.SubjectClaim)
	req.SubjectMatch = strings.TrimSpace(req.SubjectMatch)
	req.MatchType = normalizeMatchTypeInternal(req.MatchType)
	req.Audiences = utils.UniqueNonEmptyStrings(req.Audiences)
	req.EnvironmentID = mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(req.EnvironmentID).OrEmpty())).ToPointer()
	req.TokenTTLSeconds = auth.ClampFederatedTokenTTLSeconds(req.TokenTTLSeconds)

	if req.SubjectClaim == "" {
		req.SubjectClaim = defaultFederatedSubjectClaim
	}
	if req.Name == "" || req.SubjectMatch == "" || req.RoleID == "" || len(req.Audiences) == 0 {
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
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return existing, false, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
		}
		existing.Name = name
	}
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
		audiences := utils.UniqueNonEmptyStrings(req.Audiences)
		if len(audiences) == 0 {
			return existing, false, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
		}
		existing.Audiences = audiences
	}
	if req.SubjectClaim != nil {
		subjectClaim := strings.TrimSpace(*req.SubjectClaim)
		if subjectClaim == "" {
			subjectClaim = defaultFederatedSubjectClaim
		}
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
		existing.MatchType = normalizeMatchTypeInternal(*req.MatchType)
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

func normalizeMatchTypeInternal(matchType string) string {
	if strings.EqualFold(strings.TrimSpace(matchType), federatedtypes.MatchTypeGlob) {
		return federatedtypes.MatchTypeGlob
	}
	return federatedtypes.MatchTypeExact
}

func validateIssuerURLInternal(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Scheme != "https" {
		return common.Classify(common.ErrFederatedCredentialInvalid, errors.WithStackIf(errors.New("invalid federated credential: issuerUrl must be an HTTPS URL")))
	}
	return nil
}

func validateSubjectMatchInternal(matchType, subjectMatch string) error {
	if strings.TrimSpace(subjectMatch) == "" || normalizeMatchTypeInternal(matchType) == federatedtypes.MatchTypeGlob && strings.TrimSpace(subjectMatch) == "*" {
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
		return errors.WrapIf(err, "load user for federated role validation")
	}
	permissions, err := s.roleService.ResolvePermissions(ctx, user)
	if err != nil {
		return errors.WrapIf(err, "resolve user permissions")
	}
	if err := s.roleService.ValidateRoleAssignmentAgainstCaller(ctx, permissions, roleID, environmentID); err != nil {
		if errors.Is(err, common.ErrRolePermissionEscalation) {
			return common.Classify(common.ErrFederatedCredentialPermissionEscalation, errors.WrapIf(err, "cannot map a federated credential to a role you do not hold"))
		}
		return common.Classify(common.ErrFederatedCredentialInvalid, errors.WrapIf(err, "invalid federated credential"))
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
