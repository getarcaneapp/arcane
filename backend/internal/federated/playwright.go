//go:build playwright

package federated

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"

	"context"
	"strings"

	"emperror.dev/errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreatePlaywrightCredential creates the service identity, credential, and role
// assignment required by the federated-auth end-to-end flow in one transaction.
func (s *FederatedCredentialService) CreatePlaywrightCredential(ctx context.Context, issuerURL string, audiences []string, subject string, roleID string, tokenTTLSeconds int) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("federated credential service is not available")
	}
	if strings.TrimSpace(issuerURL) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(roleID) == "" || len(audiences) == 0 {
		return "", errors.New("issuerUrl, subject, roleId, and audiences are required")
	}
	if tokenTTLSeconds <= 0 {
		tokenTTLSeconds = 600
	}

	var credentialID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		serviceUser := common.User{
			Username:         "svc_federated_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			IsServiceAccount: true,
		}
		if err := tx.Create(&serviceUser).Error; err != nil {
			return errors.WrapIf(err, "failed to create federated e2e user")
		}

		credential := FederatedCredential{
			Name:            "Playwright Federated Credential",
			Enabled:         true,
			IssuerURL:       strings.TrimRight(strings.TrimSpace(issuerURL), "/"),
			Audiences:       database.StringSlice(audiences),
			SubjectClaim:    "sub",
			SubjectMatch:    strings.TrimSpace(subject),
			MatchType:       FederatedCredentialMatchExact,
			RoleID:          strings.TrimSpace(roleID),
			IdentityUserID:  serviceUser.ID,
			TokenTTLSeconds: tokenTTLSeconds,
		}
		if err := tx.Create(&credential).Error; err != nil {
			return errors.WrapIf(err, "failed to create federated e2e credential")
		}

		assignment := role.UserRoleAssignment{
			UserID: serviceUser.ID,
			RoleID: credential.RoleID,
			Source: role.RoleAssignmentSourceManual,
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return errors.WrapIf(err, "failed to create federated e2e role assignment")
		}

		credentialID = credential.ID
		return nil
	})
	if err != nil {
		return "", err
	}
	return credentialID, nil
}
