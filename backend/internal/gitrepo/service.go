package gitrepo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/samber/mo"
	"go.getarcane.app/builds/pkg/contextsource"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

type GitRepositoryService struct {
	*git.Client

	db              *database.DB
	eventService    *event.EventService
	settingsService *settings.SettingsService
}

func NewGitRepositoryService(db *database.DB, workDir string, eventService *event.EventService, settingsService *settings.SettingsService) *GitRepositoryService {
	return &GitRepositoryService{
		Client:          git.NewClient(workDir),
		db:              db,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

func (s *GitRepositoryService) GetRepositoriesPaginated(ctx context.Context, params pagination.QueryParams) ([]gitops.GitRepository, pagination.Response, error) {
	var repositories []GitRepository
	q := s.db.WithContext(ctx).Model(&GitRepository{})

	q = pagination.ApplyLikeSearch(q, params.Search, "name LIKE ? OR url LIKE ? OR COALESCE(description, '') LIKE ?")

	q = pagination.ApplyBooleanFilter(q, "enabled", params.Filters["enabled"])
	q = pagination.ApplyFilter(q, "auth_type", params.Filters["authType"])

	out, paginationResp, err := params.PaginateSortAndMapDB[GitRepository, gitops.GitRepository](q, &repositories)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list git repositories: %w", err)
	}

	return out, paginationResp, nil
}

func (s *GitRepositoryService) GetRepositoryByID(ctx context.Context, id string) (*GitRepository, error) {
	var repository GitRepository
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&repository).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("repository not found")
		}
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}
	return &repository, nil
}

func (s *GitRepositoryService) GetRepositoryByName(ctx context.Context, name string) (*GitRepository, error) {
	var repository GitRepository
	if err := s.db.WithContext(ctx).Where("name = ?", name).First(&repository).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("repository not found")
		}
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}
	return &repository, nil
}

func (s *GitRepositoryService) FindEnabledRepositoryByURL(ctx context.Context, rawURL string) (*GitRepository, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	normalizedURL := contextsource.NormalizeGitBuildContextSourceForMatch(rawURL)
	if normalizedURL == "" {
		return nil, nil
	}

	likePrefix := normalizedURL
	if idx := strings.Index(likePrefix, "#"); idx >= 0 {
		likePrefix = likePrefix[:idx]
	}
	likePrefix = strings.TrimSuffix(likePrefix, "/")

	var repositories []GitRepository
	query := s.db.WithContext(ctx).Where("enabled = ?", true)
	if likePrefix != "" {
		query = query.Where("url = ? OR url = ? OR url LIKE ? OR url LIKE ?", rawURL, normalizedURL, likePrefix+"%", likePrefix+"/%")
	}
	if err := query.Find(&repositories).Error; err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}

	for i := range repositories {
		if contextsource.NormalizeGitBuildContextSourceForMatch(repositories[i].URL) == normalizedURL {
			return new(repositories[i]), nil
		}
	}

	return nil, nil
}

const (
	defaultCommitAuthorName  = "Arcane"
	defaultCommitAuthorEmail = "arcane@localhost"
)

func (s *GitRepositoryService) CreateRepository(ctx context.Context, req gitops.CreateRepositoryRequest, actor user.Actor) (*GitRepository, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	repository := GitRepository{
		Name:                   req.Name,
		URL:                    req.URL,
		AuthType:               req.AuthType,
		Username:               req.Username,
		SSHHostKeyVerification: req.SSHHostKeyVerification,
		CommitAuthorName:       req.CommitAuthorName,
		CommitAuthorEmail:      req.CommitAuthorEmail,
		Description:            req.Description,
		Enabled:                true,
	}

	// Default to accept_new if not specified
	repository.SSHHostKeyVerification = cmp.Or(repository.SSHHostKeyVerification, "accept_new")

	if req.Enabled != nil {
		repository.Enabled = *req.Enabled
	}

	// Encrypt sensitive fields
	if req.Token != "" {
		encrypted, err := crypto.Encrypt(req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		repository.Token = encrypted
	}

	if req.SSHKey != "" {
		encrypted, err := crypto.Encrypt(req.SSHKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt SSH key: %w", err)
		}
		repository.SSHKey = encrypted
	}

	if req.SigningKey != "" {
		signingKey, passphrase, err := encryptSigningKeyInternal(req.SigningKey, req.SigningKeyPassphrase)
		if err != nil {
			return nil, err
		}
		repository.SigningKey = signingKey
		repository.SigningKeyPassphrase = passphrase
	}

	if err := s.db.WithContext(ctx).Create(&repository).Error; err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:         event.EventTypeGitRepositoryCreate,
		Severity:     event.EventSeveritySuccess,
		Title:        "Git repository created",
		Description:  fmt.Sprintf("Created git repository '%s' (%s)", repository.Name, repository.URL),
		ResourceType: new("git_repository"),
		ResourceID:   new(repository.ID),
		ResourceName: new(repository.Name),
		UserID:       new(actor.ID),
		Username:     new(actor.Username),
	})

	return &repository, nil
}

func (s *GitRepositoryService) UpdateRepository(ctx context.Context, id string, req gitops.UpdateRepositoryRequest, actor user.Actor) (*GitRepository, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if validateCredentialTargetChangeErr := validation.ValidateCredentialTargetChange(
		"repository URL",
		repository.URL,
		req.URL,
		nil,
		map[string]bool{
			"sshKey": repository.SSHKey != "",
			"token":  repository.Token != "",
		},
		map[string]bool{
			"sshKey": req.SSHKey != nil,
			"token":  req.Token != nil,
		},
	); validateCredentialTargetChangeErr != nil {
		return nil, validateCredentialTargetChangeErr
	}

	updates := make(map[string]any)

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.URL != nil {
		updates["url"] = *req.URL
	}
	if req.AuthType != nil {
		updates["auth_type"] = *req.AuthType
	}
	if req.Username != nil {
		updates["username"] = *req.Username
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.SSHHostKeyVerification != nil {
		updates["ssh_host_key_verification"] = *req.SSHHostKeyVerification
	}
	if req.CommitAuthorName != nil {
		updates["commit_author_name"] = *req.CommitAuthorName
	}
	if req.CommitAuthorEmail != nil {
		updates["commit_author_email"] = *req.CommitAuthorEmail
	}

	if req.Token != nil {
		if *req.Token == "" {
			updates["token"] = ""
		} else {
			encrypted, encryptErr := crypto.Encrypt(*req.Token)
			if encryptErr != nil {
				return nil, fmt.Errorf("failed to encrypt token: %w", encryptErr)
			}
			updates["token"] = encrypted
		}
	}

	if req.SSHKey != nil {
		if *req.SSHKey == "" {
			updates["ssh_key"] = ""
		} else {
			encrypted, encryptSSHKeyErr := crypto.Encrypt(*req.SSHKey)
			if encryptSSHKeyErr != nil {
				return nil, fmt.Errorf("failed to encrypt SSH key: %w", encryptSSHKeyErr)
			}
			updates["ssh_key"] = encrypted
		}
	}

	if applySigningKeyUpdateErr := applySigningKeyUpdateInternal(repository, req, updates); applySigningKeyUpdateErr != nil {
		return nil, applySigningKeyUpdateErr
	}

	if len(updates) > 0 {
		if updateRepositoryErr := s.db.WithContext(ctx).Model(repository).Updates(updates).Error; updateRepositoryErr != nil {
			return nil, fmt.Errorf("failed to update repository: %w", updateRepositoryErr)
		}

		// Log event
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:         event.EventTypeGitRepositoryUpdate,
			Severity:     event.EventSeveritySuccess,
			Title:        "Git repository updated",
			Description:  fmt.Sprintf("Updated git repository '%s'", repository.Name),
			ResourceType: new("git_repository"),
			ResourceID:   new(repository.ID),
			ResourceName: new(repository.Name),
			UserID:       new(actor.ID),
			Username:     new(actor.Username),
		})
	}

	return s.GetRepositoryByID(ctx, id)
}

func (s *GitRepositoryService) DeleteRepository(ctx context.Context, id string, actor user.Actor) error {
	// Check if repository is used by any syncs
	var count int64
	if err := s.db.WithContext(ctx).Table("gitops_syncs").Where("repository_id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check repository usage: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("repository is used by %d sync configuration(s)", count)
	}

	// Get repository info before deleting
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return err
	}

	if deleteRepositoryErr := s.db.WithContext(ctx).Where("id = ?", id).Delete(&GitRepository{}).Error; deleteRepositoryErr != nil {
		return fmt.Errorf("failed to delete repository: %w", deleteRepositoryErr)
	}

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:         event.EventTypeGitRepositoryDelete,
		Severity:     event.EventSeverityInfo,
		Title:        "Git repository deleted",
		Description:  fmt.Sprintf("Deleted git repository '%s'", repository.Name),
		ResourceType: new("git_repository"),
		ResourceID:   new(repository.ID),
		ResourceName: new(repository.Name),
		UserID:       new(actor.ID),
		Username:     new(actor.Username),
	})

	return nil
}

func (s *GitRepositoryService) TestConnection(ctx context.Context, id, branch string, actor user.Actor) error {
	localSettings := s.settingsService.GetSettingsConfig()
	ctx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.GitOperationTimeout.AsInt(), timeouts.DefaultGitOperation))
	defer cancel()

	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return err
	}

	authConfig, err := s.GetAuthConfig(ctx, repository)
	if err != nil {
		return err
	}

	err = s.Client.TestConnection(ctx, repository.URL, branch, authConfig)
	if err != nil {
		// Log error event
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:         event.EventTypeGitRepositoryError,
			Severity:     event.EventSeverityError,
			Title:        "Git repository connection test failed",
			Description:  fmt.Sprintf("Failed to connect to repository '%s': %s", repository.Name, err.Error()),
			ResourceType: new("git_repository"),
			ResourceID:   new(repository.ID),
			ResourceName: new(repository.Name),
			UserID:       new(actor.ID),
			Username:     new(actor.Username),
		})
		return err
	}

	// Log success event
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:         event.EventTypeGitRepositoryTest,
		Severity:     event.EventSeveritySuccess,
		Title:        "Git repository connection successful",
		Description:  fmt.Sprintf("Successfully connected to repository '%s'", repository.Name),
		ResourceType: new("git_repository"),
		ResourceID:   new(repository.ID),
		ResourceName: new(repository.Name),
		UserID:       new(actor.ID),
		Username:     new(actor.Username),
	})

	return nil
}

func (s *GitRepositoryService) GetAuthConfig(ctx context.Context, repository *GitRepository) (git.AuthConfig, error) {
	authConfig := git.AuthConfig{
		AuthType:               repository.AuthType,
		Username:               repository.Username,
		SSHHostKeyVerification: repository.SSHHostKeyVerification,
	}

	if repository.Token != "" {
		token, err := crypto.Decrypt(repository.Token)
		if err != nil {
			return authConfig, fmt.Errorf("failed to decrypt token: %w", err)
		}
		authConfig.Token = token
	}

	if repository.SSHKey != "" {
		sshKey, err := crypto.Decrypt(repository.SSHKey)
		if err != nil {
			return authConfig, fmt.Errorf("failed to decrypt SSH key: %w", err)
		}
		authConfig.SSHKey = sshKey
	}

	return authConfig, nil
}

// GetCommitIdentity returns the author for commits Arcane pushes to the
// repository, with the signing key unlocked when one is stored.
func (s *GitRepositoryService) GetCommitIdentity(ctx context.Context, repository *GitRepository) (git.CommitIdentity, error) {
	identity := git.CommitIdentity{Name: repository.CommitAuthorName, Email: repository.CommitAuthorEmail}
	identity.Name = cmp.Or(identity.Name, defaultCommitAuthorName)
	identity.Email = cmp.Or(identity.Email, defaultCommitAuthorEmail)
	if repository.SigningKey == "" {
		return identity, nil
	}
	armored, err := crypto.Decrypt(repository.SigningKey)
	if err != nil {
		return identity, fmt.Errorf("failed to decrypt signing key: %w", err)
	}
	passphrase := ""
	if repository.SigningKeyPassphrase != "" {
		if passphrase, err = crypto.Decrypt(repository.SigningKeyPassphrase); err != nil {
			return identity, fmt.Errorf("failed to decrypt signing key passphrase: %w", err)
		}
	}
	identity.SignKey, err = git.ParseSigningKey(armored, passphrase)
	return identity, err
}

// encryptSigningKeyInternal validates that armored unlocks with passphrase and
// returns both encrypted for storage.
func encryptSigningKeyInternal(armored, passphrase string) (string, string, error) {
	if _, err := git.ParseSigningKey(armored, passphrase); err != nil {
		return "", "", common.Classify(common.ErrValidation, &base.FieldError{Field: "signingKey", Err: err})
	}
	encryptedKey, err := crypto.Encrypt(armored)
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt signing key: %w", err)
	}
	encryptedPassphrase := ""
	if passphrase != "" {
		if encryptedPassphrase, err = crypto.Encrypt(passphrase); err != nil {
			return "", "", fmt.Errorf("failed to encrypt signing key passphrase: %w", err)
		}
	}
	return encryptedKey, encryptedPassphrase, nil
}

// applySigningKeyUpdateInternal resolves the signing key and passphrase an
// update leaves in place, re-validating the pair whenever either changes.
func applySigningKeyUpdateInternal(current *GitRepository, req gitops.UpdateRepositoryRequest, updates map[string]any) error {
	if req.SigningKey == nil && req.SigningKeyPassphrase == nil {
		return nil
	}
	if req.SigningKey != nil && *req.SigningKey == "" {
		updates["signing_key"] = ""
		updates["signing_key_passphrase"] = ""
		return nil
	}
	armored := ""
	if req.SigningKey != nil {
		armored = *req.SigningKey
	} else if current.SigningKey != "" {
		decrypted, err := crypto.Decrypt(current.SigningKey)
		if err != nil {
			return fmt.Errorf("failed to decrypt signing key: %w", err)
		}
		armored = decrypted
	}
	if armored == "" {
		return nil
	}
	passphrase := ""
	if req.SigningKeyPassphrase != nil {
		passphrase = *req.SigningKeyPassphrase
	} else if current.SigningKeyPassphrase != "" {
		decrypted, err := crypto.Decrypt(current.SigningKeyPassphrase)
		if err != nil {
			return fmt.Errorf("failed to decrypt signing key passphrase: %w", err)
		}
		passphrase = decrypted
	}
	encryptedKey, encryptedPassphrase, err := encryptSigningKeyInternal(armored, passphrase)
	if err != nil {
		return err
	}
	updates["signing_key"] = encryptedKey
	updates["signing_key_passphrase"] = encryptedPassphrase
	return nil
}

func (s *GitRepositoryService) ListBranches(ctx context.Context, id string) ([]gitops.BranchInfo, error) {
	localSettings := s.settingsService.GetSettingsConfig()
	listCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.GitOperationTimeout.AsInt(), timeouts.DefaultGitOperation))
	defer cancel()

	repository, err := s.GetRepositoryByID(listCtx, id)
	if err != nil {
		return nil, err
	}

	authConfig, err := s.GetAuthConfig(listCtx, repository)
	if err != nil {
		return nil, err
	}

	branches, err := s.Client.ListBranches(listCtx, repository.URL, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var result []gitops.BranchInfo
	for _, branch := range branches {
		result = append(result, gitops.BranchInfo{
			Name:      branch.Name,
			IsDefault: branch.IsDefault,
		})
	}

	return result, nil
}

func (s *GitRepositoryService) BrowseFiles(ctx context.Context, id, branch, path string) (*gitops.BrowseResponse, error) {
	localSettings := s.settingsService.GetSettingsConfig()
	ctx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.GitOperationTimeout.AsInt(), timeouts.DefaultGitOperation))
	defer cancel()

	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	authConfig, err := s.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, err
	}

	// Clone the repository
	repoPath, err := s.Clone(ctx, repository.URL, branch, authConfig, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer s.Discard(ctx, repoPath)

	// Browse the tree
	files, err := s.BrowseTree(ctx, repoPath, path)
	if err != nil {
		return nil, err
	}

	return &gitops.BrowseResponse{
		Path:  path,
		Files: files,
	}, nil
}

// SyncRepositories syncs repositories from a manager to this agent instance.
// It creates, updates, or deletes repositories to match the provided list.
func (s *GitRepositoryService) SyncRepositories(ctx context.Context, syncItems []gitops.RepositorySync) error {
	if err := normalization.Normalize(&syncItems); err != nil {
		return err
	}
	existingMap, err := s.getExistingRepositoriesMap(ctx)
	if err != nil {
		return err
	}

	syncedIDs := make(map[string]bool)

	// Process each sync item
	for _, item := range syncItems {
		syncedIDs[item.ID] = true

		if processSyncItemErr := s.processSyncItem(ctx, item, existingMap); processSyncItemErr != nil {
			return processSyncItemErr
		}
	}

	// Delete repositories that are not in the sync list
	return s.deleteUnsynced(ctx, existingMap, syncedIDs)
}

func (s *GitRepositoryService) getExistingRepositoriesMap(ctx context.Context) (map[string]*GitRepository, error) {
	var existing []GitRepository
	if err := s.db.WithContext(ctx).Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("failed to get existing repositories: %w", err)
	}

	existingMap := make(map[string]*GitRepository)
	for i := range existing {
		existingMap[existing[i].ID] = &existing[i]
	}
	return existingMap, nil
}

func (s *GitRepositoryService) processSyncItem(ctx context.Context, item gitops.RepositorySync, existingMap map[string]*GitRepository) error {
	existing, exists := existingMap[item.ID]
	if exists {
		return s.updateExistingRepository(ctx, item, existing)
	}
	return s.createNewRepository(ctx, item)
}

func (s *GitRepositoryService) updateExistingRepository(ctx context.Context, item gitops.RepositorySync, existing *GitRepository) error {
	needsUpdate, err := s.checkRepositoryNeedsUpdate(item, existing)
	if err != nil {
		return fmt.Errorf("failed to reconcile repository %s: %w", item.ID, err)
	}

	if needsUpdate {
		// Use Save to trigger GORM callbacks including UpdatedAt
		if syncRepositoryErr := s.db.WithContext(ctx).Save(existing).Error; syncRepositoryErr != nil {
			return fmt.Errorf("failed to update repository %s: %w", item.ID, syncRepositoryErr)
		}
	}

	return nil
}

func (s *GitRepositoryService) checkRepositoryNeedsUpdate(item gitops.RepositorySync, existing *GitRepository) (bool, error) {
	needsUpdate := utils.ApplyChanged(&existing.Name, mo.Some(item.Name))
	needsUpdate = utils.ApplyChanged(&existing.URL, mo.Some(item.URL)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.AuthType, mo.Some(item.AuthType)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.Username, mo.Some(item.Username)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.SSHHostKeyVerification, mo.Some(item.SSHHostKeyVerification)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.CommitAuthorName, mo.Some(item.CommitAuthorName)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.CommitAuthorEmail, mo.Some(item.CommitAuthorEmail)) || needsUpdate
	needsUpdate = utils.ApplyNullable(&existing.Description, mo.PointerToOption(item.Description)) || needsUpdate
	needsUpdate = utils.ApplyChanged(&existing.Enabled, mo.Some(item.Enabled)) || needsUpdate

	signingKey, passphrase := item.SigningKey, item.SigningKeyPassphrase
	if signingKey != "" {
		if _, err := git.ParseSigningKey(signingKey, passphrase); err != nil {
			signingKey, passphrase = "", ""
		}
	}
	for _, credential := range []struct {
		field     *string
		plaintext string
	}{
		{&existing.Token, item.Token},
		{&existing.SSHKey, item.SSHKey},
		{&existing.SigningKey, signingKey},
		{&existing.SigningKeyPassphrase, passphrase},
	} {
		changed, err := utils.ApplyEncrypted(credential.field, credential.plaintext)
		if err != nil {
			return false, err
		}
		needsUpdate = changed || needsUpdate
	}
	return needsUpdate, nil
}

func (s *GitRepositoryService) createNewRepository(ctx context.Context, item gitops.RepositorySync) error {
	var encryptedToken, encryptedSSHKey string
	var err error

	if item.Token != "" {
		encryptedToken, err = crypto.Encrypt(item.Token)
		if err != nil {
			return fmt.Errorf("failed to encrypt token for repository %s: %w", item.ID, err)
		}
	}

	if item.SSHKey != "" {
		encryptedSSHKey, err = crypto.Encrypt(item.SSHKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt SSH key for repository %s: %w", item.ID, err)
		}
	}

	var encryptedSigningKey, encryptedPassphrase string
	if item.SigningKey != "" {
		encryptedSigningKey, encryptedPassphrase, err = encryptSigningKeyInternal(item.SigningKey, item.SigningKeyPassphrase)
		if err != nil {
			return fmt.Errorf("failed to encrypt signing key for repository %s: %w", item.ID, err)
		}
	}

	sshHostKeyVerification := cmp.Or(item.SSHHostKeyVerification, "accept_new")

	repo := GitRepository{
		Name:                   item.Name,
		URL:                    item.URL,
		AuthType:               item.AuthType,
		Username:               item.Username,
		Token:                  encryptedToken,
		SSHKey:                 encryptedSSHKey,
		SSHHostKeyVerification: sshHostKeyVerification,
		CommitAuthorName:       item.CommitAuthorName,
		CommitAuthorEmail:      item.CommitAuthorEmail,
		SigningKey:             encryptedSigningKey,
		SigningKeyPassphrase:   encryptedPassphrase,
		Description:            item.Description,
		Enabled:                item.Enabled,
		ID:                     item.ID,
	}
	if createRepositoryErr := s.db.WithContext(ctx).Create(&repo).Error; createRepositoryErr != nil {
		return fmt.Errorf("failed to create repository %s: %w", item.ID, createRepositoryErr)
	}
	return nil
}

func (s *GitRepositoryService) deleteUnsynced(ctx context.Context, existingMap map[string]*GitRepository, syncedIDs map[string]bool) error {
	for id := range existingMap {
		if !syncedIDs[id] {
			if err := s.db.WithContext(ctx).Delete(&GitRepository{}, "id = ?", id).Error; err != nil {
				return fmt.Errorf("failed to delete repository %s: %w", id, err)
			}
		}
	}
	return nil
}
