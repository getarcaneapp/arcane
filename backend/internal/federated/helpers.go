package federated

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

func normalizeFederatedCredential(req types.FederatedCredential) (types.FederatedCredential, error) {
	if err := normalization.Normalize(&req); err != nil {
		return req, common.Classify(common.ErrFederatedCredentialInvalid, err)
	}
	normalized := types.FederatedCredential{
		Name:         req.Name,
		Description:  req.Description,
		Enabled:      req.Enabled,
		IssuerURL:    strings.TrimRight(strings.TrimSpace(req.IssuerURL), "/"),
		Audiences:    kit.Unique(kit.TrimNonEmpty(req.Audiences)),
		SubjectClaim: cmp.Or(strings.TrimSpace(req.SubjectClaim), defaultFederatedSubjectClaim),
		SubjectMatch: strings.TrimSpace(req.SubjectMatch),
		MatchType: kit.Ternary(
			strings.EqualFold(strings.TrimSpace(req.MatchType), types.FederatedMatchTypeGlob),
			types.FederatedMatchTypeGlob,
			types.FederatedMatchTypeExact,
		),
		RoleID:          strings.TrimSpace(req.RoleID),
		EnvironmentID:   mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(req.EnvironmentID).OrEmpty())).ToPointer(),
		TokenTTLSeconds: auth.ClampFederatedTokenTTLSeconds(req.TokenTTLSeconds),
		ExpiresAt:       req.ExpiresAt,
	}
	if normalized.Name == "" || normalized.SubjectMatch == "" || normalized.RoleID == "" || len(normalized.Audiences) == 0 {
		return normalized, common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
	}
	if err := validateSubjectMatch(normalized.MatchType, normalized.SubjectMatch); err != nil {
		return normalized, err
	}
	return normalized, nil
}

func validateIssuerURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Scheme != "https" {
		return common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential: issuerUrl must be an HTTPS URL"))
	}
	return nil
}

func validateSubjectMatch(matchType, subjectMatch string) error {
	if strings.TrimSpace(subjectMatch) == "" || strings.EqualFold(strings.TrimSpace(matchType), types.FederatedMatchTypeGlob) && strings.TrimSpace(subjectMatch) == "*" {
		return common.Classify(common.ErrFederatedCredentialInvalid, errors.New("invalid federated credential"))
	}
	return nil
}

func (s *FederatedCredentialService) validateRoleGrant(ctx context.Context, db *gorm.DB, caller *authz.PermissionSet, roleID string, environmentID *string) error {
	if validateBindingErr := s.roleService.ValidateBindingAgainstCaller(ctx, db, caller, roleID, environmentID, nil); validateBindingErr != nil {
		if errors.Is(validateBindingErr, common.ErrRolePermissionEscalation) {
			return common.Classify(common.ErrFederatedCredentialPermissionEscalation, fmt.Errorf("cannot map a federated credential to a role you do not hold: %w", validateBindingErr))
		}
		return common.Classify(common.ErrFederatedCredentialInvalid, fmt.Errorf("invalid federated credential: %w", validateBindingErr))
	}
	return nil
}

func toFederatedCredentialDTO(credential *FederatedCredential) types.FederatedCredential {
	if credential == nil {
		return types.FederatedCredential{}
	}
	dto := types.FederatedCredential{
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
