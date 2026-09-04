package federated

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"uuid"

	"emperror.dev/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/samber/mo"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
	httpxtypes "github.com/getarcaneapp/arcane/types/v2/httpx"
)

const (
	federatedCredentialLastUsedWriteWindow = 5 * time.Minute
	defaultFederatedSubjectClaim           = "sub"
)

type FederatedCredentialService struct {
	db              *database.DB
	authService     *auth.AuthService
	userService     *user.UserService
	settingsService *settings.SettingsService
	eventService    *event.EventService
	roleService     *role.RoleService
	httpClient      *http.Client
	keySetManager   *oidcjwk.KeySetManager
	providerMu      sync.RWMutex
	keySets         map[string]oidc.KeySet
	providerGroup   singleflight.Group
}

func NewFederatedCredentialService(
	db *database.DB,
	authService *auth.AuthService,
	userService *user.UserService,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	httpClient *http.Client,
	keySetManager *oidcjwk.KeySetManager,
) *FederatedCredentialService {
	if httpClient == nil {
		httpClient = httpx.NewHTTPClient(httpxtypes.ClientOptions{Timeout: 15 * time.Second, TLSHandshakeTimeout: 10 * time.Second})
	}

	return &FederatedCredentialService{
		db:              db,
		authService:     authService,
		userService:     userService,
		settingsService: settingsService,
		eventService:    eventService,
		httpClient:      httpClient,
		keySetManager:   keySetManager,
		keySets:         make(map[string]oidc.KeySet),
	}
}

func (s *FederatedCredentialService) WithRoleService(roleService *role.RoleService) *FederatedCredentialService {
	s.roleService = roleService
	return s
}

func (s *FederatedCredentialService) Create(ctx context.Context, callerUserID string, req federatedtypes.CreateFederatedCredential) (*federatedtypes.FederatedCredential, error) {
	normalized, err := normalizeCreateFederatedCredentialInternal(req)
	if err != nil {
		return nil, err
	}
	if err := s.validateRoleGrantAgainstUserInternal(ctx, callerUserID, normalized.RoleID, normalized.EnvironmentID); err != nil {
		return nil, err
	}

	var created FederatedCredential
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		serviceUser := common.User{
			Username:         "svc_federated_" + strings.ReplaceAll(uuid.New().String(), "-", ""),
			DisplayName:      mo.EmptyableToOption(strings.TrimSpace("Federated: " + normalized.Name)).ToPointer(),
			IsServiceAccount: true,
		}
		if err := tx.Create(&serviceUser).Error; err != nil {
			return errors.WrapIf(err, "failed to create federated service user")
		}

		created = FederatedCredential{
			Name:            normalized.Name,
			Description:     normalized.Description,
			Enabled:         normalized.Enabled,
			IssuerURL:       normalized.IssuerURL,
			Audiences:       normalized.Audiences,
			SubjectClaim:    normalized.SubjectClaim,
			SubjectMatch:    normalized.SubjectMatch,
			MatchType:       normalized.MatchType,
			RoleID:          normalized.RoleID,
			EnvironmentID:   normalized.EnvironmentID,
			IdentityUserID:  serviceUser.ID,
			TokenTTLSeconds: normalized.TokenTTLSeconds,
			ExpiresAt:       normalized.ExpiresAt,
		}
		if err := tx.Create(&created).Error; err != nil {
			return errors.WrapIf(err, "failed to create federated credential")
		}

		assignment := role.UserRoleAssignment{
			UserID:        serviceUser.ID,
			RoleID:        normalized.RoleID,
			EnvironmentID: normalized.EnvironmentID,
			Source:        role.RoleAssignmentSourceManual,
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return errors.WrapIf(err, "failed to create federated role assignment")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.roleService != nil {
		s.roleService.InvalidateUser(created.IdentityUserID)
	}

	reloaded, err := s.Get(ctx, created.ID)
	if err != nil {
		return nil, err
	}
	return reloaded, nil
}

func (s *FederatedCredentialService) List(ctx context.Context, params pagination.QueryParams) ([]federatedtypes.FederatedCredential, pagination.Response, error) {
	var credentials []FederatedCredential
	query := s.db.WithContext(ctx).
		Model(&FederatedCredential{}).
		Preload("IdentityUser").
		Preload("Role").
		Preload("Environment")

	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR COALESCE(description, '') LIKE ? OR issuer_url LIKE ? OR subject_match LIKE ?", pattern, pattern, pattern, pattern)
	}

	resp, err := pagination.PaginateAndSortDB(params, query, &credentials)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate federated credentials")
	}

	result := make([]federatedtypes.FederatedCredential, len(credentials))
	for i := range credentials {
		result[i] = toFederatedCredentialDTOInternal(&credentials[i])
	}
	return result, resp, nil
}

func (s *FederatedCredentialService) Get(ctx context.Context, id string) (*federatedtypes.FederatedCredential, error) {
	var credential FederatedCredential
	if err := s.db.WithContext(ctx).
		Preload("IdentityUser").
		Preload("Role").
		Preload("Environment").
		Where("id = ?", id).
		First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.Classify(common.ErrFederatedCredentialNotFound, errors.New("federated credential not found"))
		}
		return nil, errors.WrapIf(err, "failed to get federated credential")
	}
	return new(toFederatedCredentialDTOInternal(&credential)), nil
}

func (s *FederatedCredentialService) Update(ctx context.Context, callerUserID, id string, req federatedtypes.UpdateFederatedCredential) (*federatedtypes.FederatedCredential, error) {
	var credential FederatedCredential
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.Classify(common.ErrFederatedCredentialNotFound, errors.New("federated credential not found"))
		}
		return nil, errors.WrapIf(err, "failed to load federated credential")
	}

	updated, roleChanged, err := applyFederatedCredentialUpdateInternal(credential, req)
	if err != nil {
		return nil, err
	}
	revokeActiveSessions := credential.Enabled && !updated.Enabled
	if roleChanged {
		if err := s.validateRoleGrantAgainstUserInternal(ctx, callerUserID, updated.RoleID, updated.EnvironmentID); err != nil {
			return nil, err
		}
	}

	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Save(&updated).Error; err != nil {
			return errors.WrapIf(err, "failed to update federated credential")
		}
		if revokeActiveSessions {
			now := time.Now()
			if err := tx.Model(&session.UserSession{}).
				Where("federated_credential_id = ? AND revoked_at IS NULL", updated.ID).
				Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
				return errors.WrapIf(err, "failed to revoke federated credential sessions")
			}
		}
		if roleChanged {
			if err := tx.Where("user_id = ? AND source = ?", updated.IdentityUserID, role.RoleAssignmentSourceManual).
				Delete(&role.UserRoleAssignment{}).Error; err != nil {
				return errors.WrapIf(err, "failed to clear federated role assignment")
			}
			assignment := role.UserRoleAssignment{
				UserID:        updated.IdentityUserID,
				RoleID:        updated.RoleID,
				EnvironmentID: updated.EnvironmentID,
				Source:        role.RoleAssignmentSourceManual,
			}
			if err := tx.Create(&assignment).Error; err != nil {
				return errors.WrapIf(err, "failed to update federated role assignment")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if roleChanged && s.roleService != nil {
		s.roleService.InvalidateUser(updated.IdentityUserID)
	}
	if (revokeActiveSessions || roleChanged) && s.authService != nil {
		s.authService.InvalidateUserTokenCache(updated.IdentityUserID)
	}
	return s.Get(ctx, id)
}

func (s *FederatedCredentialService) Delete(ctx context.Context, id string) error {
	var credential FederatedCredential
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.Classify(common.ErrFederatedCredentialNotFound, errors.New("federated credential not found"))
		}
		return errors.WrapIf(err, "failed to load federated credential")
	}

	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Delete(&FederatedCredential{}, "id = ?", credential.ID).Error; err != nil {
			return errors.WrapIf(err, "failed to delete federated credential")
		}
		if err := tx.Delete(&common.User{}, "id = ?", credential.IdentityUserID).Error; err != nil {
			return errors.WrapIf(err, "failed to delete federated service user")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.roleService != nil {
		s.roleService.InvalidateUser(credential.IdentityUserID)
	}
	if s.authService != nil {
		s.authService.InvalidateUserTokenCache(credential.IdentityUserID)
	}
	return nil
}
