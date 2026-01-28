package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types"
	secrettypes "github.com/getarcaneapp/arcane/types/secret"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// #nosec G101 -- not a credential, default data path.
	defaultSecretsDirectory = "/app/data/secrets"
	secretsTempDirName      = "tmp"
	secretsComposeDirName   = "compose"
)

type SecretService struct {
	db              *database.DB
	settingsService *SettingsService
	eventService    *EventService
	tempFilesMu     sync.Mutex
	tempFiles       map[string][]string
}

type secretUpdateState struct {
	oldName        string
	nameChanged    bool
	contentUpdated bool
	plaintext      string
}

func NewSecretService(db *database.DB, settingsService *SettingsService, eventService *EventService) *SecretService {
	return &SecretService{
		db:              db,
		settingsService: settingsService,
		eventService:    eventService,
		tempFiles:       make(map[string][]string),
	}
}

func (s *SecretService) ListSecretsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]secrettypes.Secret, pagination.Response, error) {
	envID := normalizeEnvironmentID(environmentID)

	var secrets []models.Secret
	q := s.db.WithContext(ctx).Model(&models.Secret{}).Where("environment_id = ?", envID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where("name LIKE ? OR COALESCE(description, '') LIKE ?", searchPattern, searchPattern)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &secrets)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate secrets: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.Secret, secrettypes.Secret](secrets)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map secrets: %w", mapErr)
	}

	for i := range out {
		composePath, err := s.composeSecretPath(ctx, out[i].Name)
		if err == nil {
			out[i].ComposePath = composePath
		}
	}

	return out, paginationResp, nil
}

func (s *SecretService) GetSecretByID(ctx context.Context, environmentID string, secretID string) (*models.Secret, error) {
	envID := normalizeEnvironmentID(environmentID)

	var secret models.Secret
	if err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", secretID, envID).
		First(&secret).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &models.NotFoundError{Message: "secret not found"}
		}
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	return &secret, nil
}

func (s *SecretService) GetSecretByName(ctx context.Context, environmentID string, name string) (*models.Secret, error) {
	envID := normalizeEnvironmentID(environmentID)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &models.ValidationError{Message: "secret name is required", Field: "name"}
	}

	var secret models.Secret
	if err := s.db.WithContext(ctx).
		Where("name = ? AND environment_id = ?", name, envID).
		First(&secret).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &models.NotFoundError{Message: "secret not found"}
		}
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	return &secret, nil
}

func (s *SecretService) GetSecretWithContent(ctx context.Context, environmentID string, secretID string) (*secrettypes.SecretWithContent, error) {
	secret, err := s.GetSecretByID(ctx, environmentID, secretID)
	if err != nil {
		return nil, err
	}

	content, err := crypto.Decrypt(secret.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secret: %w", err)
	}

	out := secrettypes.SecretWithContent{
		Secret: secrettypes.Secret{
			ID:            secret.ID,
			Name:          secret.Name,
			EnvironmentID: secret.EnvironmentID,
			Description:   secret.Description,
			CreatedAt:     secret.CreatedAt,
			UpdatedAt:     secret.UpdatedAt,
		},
		Content: content,
	}

	if composePath, err := s.composeSecretPath(ctx, secret.Name); err == nil {
		out.ComposePath = composePath
	}

	return &out, nil
}

func (s *SecretService) CreateSecret(ctx context.Context, environmentID string, req secrettypes.Create, user models.User) (*secrettypes.Secret, error) {
	envID := normalizeEnvironmentID(environmentID)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, &models.ValidationError{Message: "secret name is required", Field: "name"}
	}
	if strings.ContainsAny(name, string(os.PathSeparator)+"/") {
		return nil, &models.ValidationError{Message: "secret name must not contain path separators", Field: "name"}
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, &models.ValidationError{Message: "secret content is required", Field: "content"}
	}

	if err := s.ensureUniqueName(ctx, envID, name, ""); err != nil {
		return nil, err
	}

	secretsDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare secrets directory: %w", err)
	}

	encryptedContent, err := crypto.Encrypt(req.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	secretID := uuid.NewString()
	filePath := s.buildEncryptedFilePath(secretsDir, secretID)
	if err := crypto.EncryptToFile(req.Content, filePath); err != nil {
		return nil, fmt.Errorf("failed to persist secret file: %w", err)
	}

	composePath, err := s.persistComposeSecret(ctx, name, req.Content)
	if err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("failed to persist compose secret file: %w", err)
	}

	secret := &models.Secret{
		BaseModel:     models.BaseModel{ID: secretID},
		Name:          name,
		EnvironmentID: envID,
		Content:       encryptedContent,
		FilePath:      &filePath,
		Description:   req.Description,
	}

	if err := s.db.WithContext(ctx).Create(secret).Error; err != nil {
		_ = os.Remove(filePath)
		_ = os.Remove(composePath)
		if isUniqueConstraintError(err) {
			return nil, &models.ConflictError{Message: "secret name already exists"}
		}
		return nil, fmt.Errorf("failed to create secret: %w", err)
	}

	metadata := models.JSON{"action": "create"}
	if logErr := s.eventService.LogSecretEvent(ctx, models.EventTypeSecretCreate, secret.ID, secret.Name, user.ID, user.Username, envID, metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log secret creation", "secret", secret.Name, "error", logErr.Error())
	}

	out := secrettypes.Secret{
		ID:            secret.ID,
		Name:          secret.Name,
		EnvironmentID: secret.EnvironmentID,
		Description:   secret.Description,
		CreatedAt:     secret.CreatedAt,
		UpdatedAt:     secret.UpdatedAt,
		ComposePath:   composePath,
	}

	return &out, nil
}

func (s *SecretService) UpdateSecret(ctx context.Context, environmentID string, secretID string, req secrettypes.Update, user models.User) (*secrettypes.Secret, error) {
	secret, err := s.GetSecretByID(ctx, environmentID, secretID)
	if err != nil {
		return nil, err
	}

	updateState := secretUpdateState{oldName: secret.Name}

	if req.Name != nil {
		nameChanged, err := s.applySecretNameUpdate(ctx, secret, *req.Name)
		if err != nil {
			return nil, err
		}
		updateState.nameChanged = nameChanged
	}

	if req.Description != nil {
		secret.Description = req.Description
	}

	if req.Content != nil {
		contentUpdated, plaintext, err := s.applySecretContentUpdate(ctx, secret, *req.Content)
		if err != nil {
			return nil, err
		}
		updateState.contentUpdated = contentUpdated
		updateState.plaintext = plaintext
	}

	composePath, err := s.ensureComposeSecretForUpdate(ctx, secret, updateState)
	if err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Save(secret).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, &models.ConflictError{Message: "secret name already exists"}
		}
		return nil, fmt.Errorf("failed to update secret: %w", err)
	}

	if updateState.nameChanged {
		if err := s.removeComposeSecret(ctx, updateState.oldName); err != nil {
			slog.WarnContext(ctx, "failed to remove old compose secret file", "secret", updateState.oldName, "error", err.Error())
		}
	}

	metadata := models.JSON{"action": "update"}
	if logErr := s.eventService.LogSecretEvent(ctx, models.EventTypeSecretUpdate, secret.ID, secret.Name, user.ID, user.Username, secret.EnvironmentID, metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log secret update", "secret", secret.Name, "error", logErr.Error())
	}

	out := secrettypes.Secret{
		ID:            secret.ID,
		Name:          secret.Name,
		EnvironmentID: secret.EnvironmentID,
		Description:   secret.Description,
		CreatedAt:     secret.CreatedAt,
		UpdatedAt:     secret.UpdatedAt,
		ComposePath:   composePath,
	}

	return &out, nil
}

func (s *SecretService) DeleteSecret(ctx context.Context, environmentID string, secretID string, user models.User) error {
	secret, err := s.GetSecretByID(ctx, environmentID, secretID)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Delete(&models.Secret{}, "id = ? AND environment_id = ?", secret.ID, secret.EnvironmentID).Error; err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	if secret.FilePath != nil && *secret.FilePath != "" {
		if err := os.Remove(*secret.FilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "failed to remove secret file", "secret", secret.Name, "error", err.Error())
		}
	}

	if err := s.removeComposeSecret(ctx, secret.Name); err != nil {
		slog.WarnContext(ctx, "failed to remove compose secret file", "secret", secret.Name, "error", err.Error())
	}

	metadata := models.JSON{"action": "delete"}
	if logErr := s.eventService.LogSecretEvent(ctx, models.EventTypeSecretDelete, secret.ID, secret.Name, user.ID, user.Username, secret.EnvironmentID, metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log secret deletion", "secret", secret.Name, "error", logErr.Error())
	}

	return nil
}

func (s *SecretService) PrepareSecretMounts(ctx context.Context, environmentID, containerName string, mounts []secrettypes.Mount) ([]string, []string, error) {
	if len(mounts) == 0 {
		return nil, nil, nil
	}

	secretsDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return nil, nil, err
	}

	safeContainerName := sanitizePathSegment(containerName)
	tempDir := filepath.Join(secretsDir, secretsTempDirName, safeContainerName)
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare secrets temp directory: %w", err)
	}

	binds := make([]string, 0, len(mounts))
	tempPaths := make([]string, 0, len(mounts))

	for _, mount := range mounts {
		secret, err := s.GetSecretByID(ctx, environmentID, mount.SecretID)
		if err != nil {
			return nil, nil, err
		}

		hostPath := filepath.Join(tempDir, secret.Name)
		if err := s.ensurePlaintextSecretFile(ctx, secret, hostPath); err != nil {
			return nil, nil, err
		}

		binds = append(binds, fmt.Sprintf("%s:/run/secrets/%s:ro", hostPath, secret.Name))
		tempPaths = append(tempPaths, hostPath)
	}

	return binds, tempPaths, nil
}

func (s *SecretService) EnsureSecretMountsFromContainer(ctx context.Context, environmentID string, mounts []container.MountPoint) error {
	if len(mounts) == 0 {
		return nil
	}

	secretsDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return err
	}
	tempRoot := filepath.Join(secretsDir, secretsTempDirName)

	for _, mount := range mounts {
		if !strings.HasPrefix(mount.Destination, "/run/secrets/") {
			continue
		}
		if mount.Source == "" {
			continue
		}
		if !isWithinDir(tempRoot, mount.Source) {
			continue
		}

		secretName := path.Base(mount.Destination)
		if secretName == "" || secretName == "/" || secretName == "." {
			continue
		}

		secret, err := s.GetSecretByName(ctx, environmentID, secretName)
		if err != nil {
			return err
		}

		if err := s.ensurePlaintextSecretFile(ctx, secret, mount.Source); err != nil {
			return err
		}
	}

	return nil
}

func (s *SecretService) ExtractSecretMountPaths(ctx context.Context, mounts []container.MountPoint) []string {
	if len(mounts) == 0 {
		return nil
	}

	secretsDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return nil
	}
	tempRoot := filepath.Join(secretsDir, secretsTempDirName)

	paths := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		if !strings.HasPrefix(mount.Destination, "/run/secrets/") {
			continue
		}
		if mount.Source == "" {
			continue
		}
		if !isWithinDir(tempRoot, mount.Source) {
			continue
		}
		paths = append(paths, mount.Source)
	}

	return paths
}

func (s *SecretService) TrackTempFiles(containerID string, paths []string) {
	if containerID == "" || len(paths) == 0 {
		return
	}
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return
	}

	s.tempFilesMu.Lock()
	s.tempFiles[containerID] = append(s.tempFiles[containerID], cleaned...)
	s.tempFilesMu.Unlock()
}

func (s *SecretService) ConsumeTempFiles(containerID string) []string {
	if containerID == "" {
		return nil
	}
	var paths []string
	s.tempFilesMu.Lock()
	paths = append(paths, s.tempFiles[containerID]...)
	delete(s.tempFiles, containerID)
	s.tempFilesMu.Unlock()
	return paths
}

func (s *SecretService) CleanupTempFiles(paths []string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to remove secret temp file", "path", p, "error", err.Error())
		}
		cleanupEmptyDir(filepath.Dir(p))
	}
}

func (s *SecretService) ensurePlaintextSecretFile(ctx context.Context, secret *models.Secret, hostPath string) error {
	secretsDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return err
	}

	encryptedPath, err := s.ensureEncryptedFile(ctx, secret, secretsDir)
	if err != nil {
		return err
	}

	content, err := crypto.DecryptFromFile(encryptedPath)
	if err != nil {
		return fmt.Errorf("failed to decrypt secret file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		return fmt.Errorf("failed to create secret temp dir: %w", err)
	}

	if err := os.WriteFile(hostPath, []byte(content), 0o400); err != nil {
		return fmt.Errorf("failed to write secret temp file: %w", err)
	}

	return nil
}

func (s *SecretService) ensureEncryptedFile(ctx context.Context, secret *models.Secret, secretsDir string) (string, error) {
	if secret.FilePath == nil || *secret.FilePath == "" {
		filePath := s.buildEncryptedFilePath(secretsDir, secret.ID)
		if err := os.WriteFile(filePath, []byte(secret.Content), 0o400); err != nil {
			return "", fmt.Errorf("failed to persist secret file: %w", err)
		}
		if err := s.db.WithContext(ctx).Model(secret).Update("file_path", filePath).Error; err != nil {
			return "", fmt.Errorf("failed to update secret file path: %w", err)
		}
		secret.FilePath = &filePath
		return filePath, nil
	}

	if _, err := os.Stat(*secret.FilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(*secret.FilePath, []byte(secret.Content), 0o400); err != nil {
				return "", fmt.Errorf("failed to recreate secret file: %w", err)
			}
			return *secret.FilePath, nil
		}
		return "", err
	}

	return *secret.FilePath, nil
}

func (s *SecretService) persistEncryptedContent(ctx context.Context, secret *models.Secret, plaintext string) error {
	secretsDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return err
	}

	filePath := ""
	if secret.FilePath != nil {
		filePath = *secret.FilePath
	}
	if filePath == "" {
		filePath = s.buildEncryptedFilePath(secretsDir, secret.ID)
		secret.FilePath = &filePath
		if err := s.db.WithContext(ctx).Model(secret).Update("file_path", filePath).Error; err != nil {
			return fmt.Errorf("failed to update secret file path: %w", err)
		}
	}

	if err := crypto.EncryptToFile(plaintext, filePath); err != nil {
		return fmt.Errorf("failed to update secret file: %w", err)
	}

	return nil
}

func (s *SecretService) ensureUniqueName(ctx context.Context, environmentID, name, secretID string) error {
	var existing models.Secret
	query := s.db.WithContext(ctx).Where("name = ? AND environment_id = ?", name, environmentID)
	if secretID != "" {
		query = query.Where("id <> ?", secretID)
	}

	if err := query.First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("failed to check secret uniqueness: %w", err)
	}

	return &models.ConflictError{Message: "secret name already exists"}
}

func (s *SecretService) applySecretNameUpdate(ctx context.Context, secret *models.Secret, newName string) (bool, error) {
	name := strings.TrimSpace(newName)
	if name == "" {
		return false, &models.ValidationError{Message: "secret name is required", Field: "name"}
	}
	if strings.ContainsAny(name, string(os.PathSeparator)+"/") {
		return false, &models.ValidationError{Message: "secret name must not contain path separators", Field: "name"}
	}
	if name == secret.Name {
		return false, nil
	}
	if err := s.ensureUniqueName(ctx, secret.EnvironmentID, name, secret.ID); err != nil {
		return false, err
	}
	secret.Name = name
	return true, nil
}

func (s *SecretService) applySecretContentUpdate(ctx context.Context, secret *models.Secret, content string) (bool, string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false, "", &models.ValidationError{Message: "secret content is required", Field: "content"}
	}

	encryptedContent, err := crypto.Encrypt(trimmed)
	if err != nil {
		return false, "", fmt.Errorf("failed to encrypt secret: %w", err)
	}
	secret.Content = encryptedContent

	if err := s.persistEncryptedContent(ctx, secret, trimmed); err != nil {
		return false, "", err
	}

	return true, trimmed, nil
}

func (s *SecretService) ensureComposeSecretForUpdate(ctx context.Context, secret *models.Secret, state secretUpdateState) (string, error) {
	if !state.nameChanged && !state.contentUpdated {
		return s.composeSecretPath(ctx, secret.Name)
	}

	composeContent := state.plaintext
	if composeContent == "" {
		plaintext, err := crypto.Decrypt(secret.Content)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt secret for compose file: %w", err)
		}
		composeContent = plaintext
	}

	composePath, err := s.persistComposeSecret(ctx, secret.Name, composeContent)
	if err != nil {
		return "", fmt.Errorf("failed to persist compose secret file: %w", err)
	}
	return composePath, nil
}

func (s *SecretService) buildEncryptedFilePath(secretsDir, secretID string) string {
	return filepath.Join(secretsDir, fmt.Sprintf("secret-%s.enc", secretID))
}

func (s *SecretService) ensureSecretsDirectory(ctx context.Context) (string, error) {
	dir := defaultSecretsDirectory
	if s.settingsService != nil {
		dir = s.settingsService.GetStringSetting(ctx, "secretsDirectory", defaultSecretsDirectory)
	}
	if strings.TrimSpace(dir) == "" {
		dir = defaultSecretsDirectory
	}

	cleaned := filepath.Clean(dir)
	if !filepath.IsAbs(cleaned) {
		absPath, err := filepath.Abs(cleaned)
		if err == nil {
			cleaned = absPath
		}
	}
	if err := os.MkdirAll(cleaned, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(cleaned, secretsComposeDirName), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(cleaned, secretsTempDirName), 0o700); err != nil {
		return "", err
	}
	return cleaned, nil
}

func (s *SecretService) ComposeSecretPath(ctx context.Context, name string) (string, error) {
	return s.composeSecretPath(ctx, name)
}

func (s *SecretService) composeSecretPath(ctx context.Context, name string) (string, error) {
	baseDir, err := s.ensureSecretsDirectory(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, secretsComposeDirName, name), nil
}

func (s *SecretService) persistComposeSecret(ctx context.Context, name, content string) (string, error) {
	composePath, err := s.composeSecretPath(ctx, name)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		return "", err
	}
	return composePath, nil
}

func (s *SecretService) removeComposeSecret(ctx context.Context, name string) error {
	composePath, err := s.composeSecretPath(ctx, name)
	if err != nil {
		return err
	}
	if err := os.Remove(composePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func normalizeEnvironmentID(environmentID string) string {
	if strings.TrimSpace(environmentID) == "" {
		return types.LOCAL_DOCKER_ENVIRONMENT_ID
	}
	return environmentID
}

func sanitizePathSegment(value string) string {
	if value == "" {
		return "default"
	}
	clean := strings.TrimSpace(value)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(clean)
}

func cleanupEmptyDir(dir string) {
	if dir == "" {
		return
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}

func isWithinDir(root, candidate string) bool {
	rootClean := filepath.Clean(root)
	candidateClean := filepath.Clean(candidate)
	if rootClean == candidateClean {
		return true
	}
	if !strings.HasSuffix(rootClean, string(os.PathSeparator)) {
		rootClean += string(os.PathSeparator)
	}
	return strings.HasPrefix(candidateClean, rootClean)
}

func isUniqueConstraintError(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "unique") || strings.Contains(lower, "duplicate key") || strings.Contains(lower, "unique constraint")
}
