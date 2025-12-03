package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	composeloader "github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	appfs "github.com/getarcaneapp/arcane/backend/internal/utils/fs"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	templateutil "github.com/getarcaneapp/arcane/backend/internal/utils/template"
	"github.com/google/uuid"
	"go.getarcane.app/types/env"
	tmpl "go.getarcane.app/types/template"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type remoteCache struct {
	templates  []models.ComposeTemplate
	lastFetch  time.Time
	refreshing bool
}

type registryFetchMeta struct {
	LastModified string
	Templates    []tmpl.RemoteTemplate
}

type TemplateService struct {
	db              *database.DB
	httpClient      *http.Client
	settingsService *SettingsService

	remoteMu    sync.RWMutex
	remoteCache remoteCache

	registryMu        sync.RWMutex
	registryFetchMeta map[string]*registryFetchMeta

	fsSyncMu   sync.Mutex
	lastFsSync time.Time
}

const (
	remoteCacheDuration = 5 * time.Minute
	fsSyncInterval      = 1 * time.Minute
)

const remoteIDPrefix = "remote"

func makeRemoteID(registryID, slug string) string {
	return fmt.Sprintf("%s:%s:%s", remoteIDPrefix, registryID, slug)
}

func NewTemplateService(ctx context.Context, db *database.DB, httpClient *http.Client, settingsService *SettingsService) *TemplateService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	if err := appfs.EnsureDefaultTemplates(ctx); err != nil {
		slog.WarnContext(ctx, "failed to ensure default templates", "error", err)
	}

	return &TemplateService{
		db:                db,
		httpClient:        httpClient,
		settingsService:   settingsService,
		remoteCache:       remoteCache{},
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}
}

func (s *TemplateService) ensureRemoteTemplatesLoaded(ctx context.Context) error {
	s.remoteMu.Lock()

	// If cache is valid, return
	if s.remoteCache.templates != nil && time.Since(s.remoteCache.lastFetch) < remoteCacheDuration {
		s.remoteMu.Unlock()
		return nil
	}

	// If we have cache (even stale) and are not already refreshing, trigger background refresh
	if s.remoteCache.templates != nil {
		if !s.remoteCache.refreshing {
			s.remoteCache.refreshing = true
			s.remoteMu.Unlock()

			// Use a closure that accepts context to satisfy linter, though we create a new one
			go func(parentCtx context.Context) {
				// Create a detached context with timeout for background fetch
				// We use context.WithoutCancel(parentCtx) to ensure it outlives the request
				bgCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), 2*time.Minute)
				defer cancel()

				defer func() {
					s.remoteMu.Lock()
					s.remoteCache.refreshing = false
					s.remoteMu.Unlock()
				}()

				if _, err := s.refreshRemoteTemplates(bgCtx); err != nil {
					slog.WarnContext(bgCtx, "background remote template refresh failed", "error", err)
				}
			}(ctx)
			return nil // Return stale cache
		}
		s.remoteMu.Unlock()
		return nil // Return stale cache while someone else refreshes
	}

	s.remoteMu.Unlock()

	// No cache at all, must block
	_, err := s.refreshRemoteTemplates(ctx)
	return err
}

func (s *TemplateService) refreshRemoteTemplates(ctx context.Context) ([]models.ComposeTemplate, error) {
	templates, err := s.loadRemoteTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load remote templates: %w", err)
	}

	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	s.remoteCache.templates = templates
	s.remoteCache.lastFetch = time.Now()
	return templates, nil
}

func (s *TemplateService) GetAllTemplates(ctx context.Context) ([]models.ComposeTemplate, error) {
	return s.getMergedTemplates(ctx)
}

func (s *TemplateService) GetAllTemplatesPaginated(ctx context.Context, params pagination.QueryParams) ([]tmpl.Template, pagination.Response, error) {
	templates, err := s.getMergedTemplates(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	items := make([]tmpl.Template, 0, len(templates))
	for _, t := range templates {
		var dtoItem tmpl.Template
		if err := mapper.MapStruct(&t, &dtoItem); err != nil {
			slog.WarnContext(ctx, "failed to map template to DTO", "error", err, "templateID", t.ID)
			continue
		}
		items = append(items, dtoItem)
	}

	config := pagination.Config[tmpl.Template]{
		SearchAccessors: []pagination.SearchAccessor[tmpl.Template]{
			func(t tmpl.Template) (string, error) { return t.Name, nil },
			func(t tmpl.Template) (string, error) { return t.Description, nil },
			func(t tmpl.Template) (string, error) {
				if t.Metadata != nil && len(t.Metadata.Tags) > 0 {
					return strings.Join(t.Metadata.Tags, " "), nil
				}
				return "", nil
			},
		},
		SortBindings: []pagination.SortBinding[tmpl.Template]{
			{
				Key: "name",
				Fn: func(a, b tmpl.Template) int {
					return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
				},
			},
			{
				Key: "description",
				Fn: func(a, b tmpl.Template) int {
					return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
				},
			},
			{
				Key: "isRemote",
				Fn: func(a, b tmpl.Template) int {
					if a.IsRemote == b.IsRemote {
						return 0
					}
					if a.IsRemote {
						return 1
					}
					return -1
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[tmpl.Template]{
			{
				Key: "type",
				Fn: func(item tmpl.Template, filterValue string) bool {
					switch filterValue {
					case "true":
						return item.IsRemote
					case "false":
						return !item.IsRemote
					}
					return true
				},
			},
		},
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)

	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (int64(result.TotalCount) + int64(params.Limit) - 1) / int64(params.Limit)
	}

	currentPage := 1
	if params.Limit > 0 && params.Start > 0 {
		currentPage = (params.Start / params.Limit) + 1
	}

	paginationResp := pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      int64(result.TotalCount),
		CurrentPage:     currentPage,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: int64(result.TotalAvailable),
	}

	return result.Items, paginationResp, nil
}

var ErrTemplateNotFound = errors.New("template not found")

func (s *TemplateService) GetTemplate(ctx context.Context, id string) (*models.ComposeTemplate, error) {
	if err := s.syncFilesystemTemplatesInternal(ctx); err != nil {
		slog.WarnContext(ctx, "failed to sync filesystem templates", "error", err)
	}

	var template models.ComposeTemplate
	if err := s.db.WithContext(ctx).Preload("Registry").Where("id = ?", id).First(&template).Error; err == nil {
		return &template, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to query local template: %w", err)
	}

	if err := s.ensureRemoteTemplatesLoaded(ctx); err != nil {
		return nil, fmt.Errorf("template not found (failed to load remote templates): %w", err)
	}
	s.remoteMu.RLock()
	copied := make([]models.ComposeTemplate, len(s.remoteCache.templates))
	copy(copied, s.remoteCache.templates)
	s.remoteMu.RUnlock()

	for _, remoteTemplate := range copied {
		if remoteTemplate.ID == id {
			t := remoteTemplate
			return &t, nil
		}
	}

	return nil, ErrTemplateNotFound
}

func (s *TemplateService) CreateTemplate(ctx context.Context, template *models.ComposeTemplate) error {
	if template.ID == "" {
		template.ID = uuid.NewString()
	}
	template.IsCustom = true
	template.IsRemote = false
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(template).Error; err != nil {
			return fmt.Errorf("failed to create template: %w", err)
		}
		return nil
	})
}

func (s *TemplateService) UpdateTemplate(ctx context.Context, id string, updates *models.ComposeTemplate) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.ComposeTemplate
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("template not found")
			}
			return fmt.Errorf("failed to find template: %w", err)
		}

		if existing.IsRemote {
			return fmt.Errorf("cannot update remote template")
		}

		existing.Name = updates.Name
		existing.Description = updates.Description
		existing.Content = updates.Content
		existing.EnvContent = updates.EnvContent

		if err := tx.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update template: %w", err)
		}

		return nil
	})
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.ComposeTemplate
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("template not found")
			}
			return fmt.Errorf("failed to find template: %w", err)
		}

		if existing.IsRemote {
			return fmt.Errorf("cannot delete remote template directly")
		}

		baseDir, err := appfs.GetTemplatesDirectory(ctx)
		if err != nil {
			return fmt.Errorf("failed to get templates directory: %w", err)
		} else {
			templatePath := filepath.Join(baseDir, existing.Name)

			if stat, err := os.Stat(templatePath); err == nil && stat.IsDir() {
				composeFile := filepath.Join(templatePath, "compose.yaml")
				if _, err := os.Stat(composeFile); err == nil {
					if err := os.RemoveAll(templatePath); err != nil {
						return fmt.Errorf("failed to delete template directory: %w", err)
					}
				}
			}
		}

		if err := tx.Delete(&existing).Error; err != nil {
			return fmt.Errorf("failed to delete template: %w", err)
		}
		return nil
	})
}

func (s *TemplateService) GetComposeTemplate() string {
	composePath := filepath.Join("data", "templates", ".compose.template")
	content, err := os.ReadFile(composePath)
	if err != nil {
		slog.Warn("failed to read compose template", "error", err)
		return ""
	}
	return string(content)
}

func (s *TemplateService) SaveComposeTemplate(content string) error {
	templateDir := filepath.Join("data", "templates")
	composePath := filepath.Join(templateDir, ".compose.template")
	return appfs.WriteTemplateFile(composePath, content)
}

func (s *TemplateService) GetEnvTemplate() string {
	envPath := filepath.Join("data", "templates", ".env.template")
	content, err := os.ReadFile(envPath)
	if err != nil {
		slog.Warn("failed to read env template", "error", err)
		return ""
	}
	return string(content)
}

func (s *TemplateService) SaveEnvTemplate(content string) error {
	templateDir := filepath.Join("data", "templates")
	envPath := filepath.Join(templateDir, ".env.template")
	return appfs.WriteTemplateFile(envPath, content)
}

func (s *TemplateService) GetRegistries(ctx context.Context) ([]models.TemplateRegistry, error) {
	var registries []models.TemplateRegistry
	err := s.db.WithContext(ctx).Find(&registries).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get registries: %w", err)
	}
	return registries, nil
}

func (s *TemplateService) CreateRegistry(ctx context.Context, registry *models.TemplateRegistry) error {
	// Hydrate metadata if needed
	if registry.Name == "" || registry.Description == "" {
		if registry.URL == "" {
			return fmt.Errorf("registry URL is required")
		}
		if manifest, err := s.fetchRegistryManifest(ctx, registry.URL); err == nil {
			if registry.Name == "" {
				registry.Name = manifest.Name
			}
			if registry.Description == "" {
				registry.Description = manifest.Description
			}
		} else if registry.Name == "" || registry.Description == "" {
			return fmt.Errorf("failed to fetch registry manifest: %w", err)
		}
	}

	if registry.ID == "" {
		registry.ID = uuid.NewString()
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(registry).Error; err != nil {
			return fmt.Errorf("failed to create registry: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.invalidateRemoteCache()
	return nil
}

func (s *TemplateService) UpdateRegistry(ctx context.Context, id string, updates *models.TemplateRegistry) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.TemplateRegistry
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("registry not found")
			}
			return fmt.Errorf("failed to find registry: %w", err)
		}

		if err := s.hydrateRegistryUpdates(ctx, updates, &existing); err != nil {
			return err
		}

		if err := tx.Model(&models.TemplateRegistry{}).Where("id = ?", id).
			Select("Name", "URL", "Description", "Enabled").
			Updates(updates).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.invalidateRemoteCache()
	return nil
}

func (s *TemplateService) hydrateRegistryUpdates(ctx context.Context, updates, existing *models.TemplateRegistry) error {
	urlChanged := updates.URL != "" && updates.URL != existing.URL
	needsHydration := updates.Name == "" || updates.Description == ""

	if (urlChanged || needsHydration) && (updates.URL != "" || existing.URL != "") {
		manifestURL := updates.URL
		if manifestURL == "" {
			manifestURL = existing.URL
		}
		if manifest, err := s.fetchRegistryManifest(ctx, manifestURL); err == nil {
			if updates.Name == "" {
				updates.Name = manifest.Name
			}
			if updates.Description == "" {
				updates.Description = manifest.Description
			}
		} else if urlChanged && (updates.Name == "" || updates.Description == "") {
			return fmt.Errorf("failed to fetch registry manifest: %w", err)
		}
	}
	return nil
}

func (s *TemplateService) DeleteRegistry(ctx context.Context, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ?", id).Delete(&models.TemplateRegistry{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("registry not found")
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.invalidateRemoteCache()
	return nil
}

func (s *TemplateService) loadRemoteTemplates(ctx context.Context) ([]models.ComposeTemplate, error) {
	registries, err := s.GetRegistries(ctx)
	if err != nil {
		return nil, err
	}

	var (
		mu        sync.Mutex
		templates []models.ComposeTemplate
	)

	g, ctx := errgroup.WithContext(ctx)

	for i := range registries {
		reg := registries[i]
		if !reg.Enabled {
			continue
		}

		g.Go(func() error {
			remoteTemplates, err := s.fetchRegistryTemplates(ctx, &reg)
			if err != nil {
				slog.WarnContext(ctx, "failed to fetch templates from registry", "registry", reg.Name, "url", reg.URL, "error", err)
				return nil // Don't fail the whole group if one registry fails
			}

			mu.Lock()
			defer mu.Unlock()
			for _, rt := range remoteTemplates {
				template := s.convertRemoteToLocal(rt, &reg)
				templates = append(templates, template)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return templates, nil
}

func (s *TemplateService) FetchRaw(ctx context.Context, url string) ([]byte, error) {
	return s.doGET(ctx, url)
}

func (s *TemplateService) doGET(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", url, err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d for URL %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %w", url, err)
	}
	return body, nil
}

// fetchRegistryTemplates performs a conditional GET using If-Modified-Since.
// If the server replies 304 Not Modified, cached templates for the registry are reused.
func (s *TemplateService) fetchRegistryTemplates(ctx context.Context, reg *models.TemplateRegistry) ([]tmpl.RemoteTemplate, error) {
	s.registryMu.RLock()
	fetchMeta := s.registryFetchMeta[reg.ID]
	s.registryMu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if fetchMeta != nil && fetchMeta.LastModified != "" {
		req.Header.Set("If-Modified-Since", fetchMeta.LastModified)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		if fetchMeta != nil {
			return fetchMeta.Templates, nil
		}
		return nil, fmt.Errorf("received 304 without cached data")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var regDTO tmpl.RemoteRegistry
	if err := json.Unmarshal(body, &regDTO); err != nil {
		return nil, fmt.Errorf("parse registry JSON: %w", err)
	}

	lm := resp.Header.Get("Last-Modified")
	newMeta := &registryFetchMeta{
		LastModified: lm,
		Templates:    regDTO.Templates,
	}
	s.registryMu.Lock()
	s.registryFetchMeta[reg.ID] = newMeta
	s.registryMu.Unlock()

	return regDTO.Templates, nil
}

func (s *TemplateService) fetchRegistryManifest(ctx context.Context, url string) (*tmpl.RemoteRegistry, error) {
	body, err := s.doGET(ctx, url)
	if err != nil {
		return nil, err
	}
	var reg tmpl.RemoteRegistry
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry JSON: %w", err)
	}
	if reg.Name == "" || len(reg.Templates) == 0 {
		return nil, fmt.Errorf("invalid registry manifest: missing required fields (name, templates)")
	}
	return &reg, nil
}

func (s *TemplateService) convertRemoteToLocal(remote tmpl.RemoteTemplate, registry *models.TemplateRegistry) models.ComposeTemplate {
	publicID := makeRemoteID(registry.ID, remote.ID)

	return models.ComposeTemplate{
		BaseModel:   models.BaseModel{ID: publicID},
		Name:        remote.Name,
		Description: remote.Description,
		Content:     "",
		EnvContent:  nil,
		IsCustom:    false,
		IsRemote:    true,
		RegistryID:  &registry.ID,
		Registry:    registry,
		Metadata: &models.ComposeTemplateMetadata{
			Version:          &remote.Version,
			Author:           &remote.Author,
			Tags:             remote.Tags,
			RemoteURL:        &remote.ComposeURL,
			EnvURL:           &remote.EnvURL,
			DocumentationURL: &remote.DocumentationURL,
		},
	}
}

func (s *TemplateService) FetchTemplateContent(ctx context.Context, template *models.ComposeTemplate) (string, string, error) {
	if !template.IsRemote || template.Metadata == nil || template.Metadata.RemoteURL == nil {
		return template.Content, "", fmt.Errorf("not a remote template or missing remote URL")
	}

	composeContent, err := s.fetchURL(ctx, *template.Metadata.RemoteURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch compose content from %s: %w", *template.Metadata.RemoteURL, err)
	}

	var envContent string
	if template.Metadata.EnvURL != nil && *template.Metadata.EnvURL != "" {
		envContent, err = s.fetchURL(ctx, *template.Metadata.EnvURL)
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch env content", "url", *template.Metadata.EnvURL, "error", err)
			envContent = ""
		}
	}

	return composeContent, envContent, nil
}

func (s *TemplateService) fetchURL(ctx context.Context, url string) (string, error) {
	body, err := s.doGET(ctx, url)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *TemplateService) DownloadTemplate(ctx context.Context, remoteTemplate *models.ComposeTemplate) (*models.ComposeTemplate, error) {
	if !remoteTemplate.IsRemote {
		return nil, fmt.Errorf("template is not remote")
	}

	base := s.templateBaseFromRemote(remoteTemplate)

	dir, composePath, envPath, err := appfs.EnsureTemplateDir(ctx, base)
	if err != nil {
		return nil, err
	}
	srcDesc := appfs.ImportedComposeDescription(dir)

	return s.downloadTemplateTransaction(ctx, remoteTemplate, base, composePath, envPath, srcDesc)
}

func (s *TemplateService) downloadTemplateTransaction(ctx context.Context, remoteTemplate *models.ComposeTemplate, base, composePath, envPath, srcDesc string) (*models.ComposeTemplate, error) {
	var resultTemplate *models.ComposeTemplate

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.ComposeTemplate
		if err := tx.
			Where("is_remote = ? AND registry_id IS NULL AND (description = ? OR name = ?)", false, srcDesc, base).
			First(&existing).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to check existing template: %w", err)
		} else if err == nil {
			// Existing template found
			composeContent, envContent, err := s.FetchTemplateContent(ctx, remoteTemplate)
			if err != nil {
				return fmt.Errorf("failed to fetch template content for existing local template: %w", err)
			}

			envPtr, werr := appfs.WriteTemplateFiles(composePath, envPath, composeContent, envContent)
			if werr != nil {
				return werr
			}

			existing.Content = composeContent
			existing.EnvContent = envPtr
			existing.Metadata = cloneTemplateMetadata(remoteTemplate.Metadata)

			if err := tx.Save(&existing).Error; err != nil {
				return fmt.Errorf("failed to update existing local template: %w", err)
			}
			resultTemplate = &existing
			return nil
		}

		// New template
		composeContent, envContent, err := s.FetchTemplateContent(ctx, remoteTemplate)
		if err != nil {
			return fmt.Errorf("failed to fetch template content for download: %w", err)
		}

		envPtr, werr := appfs.WriteTemplateFiles(composePath, envPath, composeContent, envContent)
		if werr != nil {
			return werr
		}

		localTemplate := &models.ComposeTemplate{
			BaseModel:   models.BaseModel{ID: uuid.NewString()},
			Name:        base,
			Description: srcDesc,
			Content:     composeContent,
			EnvContent:  envPtr,
			IsCustom:    true,
			IsRemote:    false,
			RegistryID:  nil,
			Registry:    nil,
			Metadata:    cloneTemplateMetadata(remoteTemplate.Metadata),
		}

		if err := tx.Create(localTemplate).Error; err != nil {
			return fmt.Errorf("failed to save local template: %w", err)
		}
		resultTemplate = localTemplate
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resultTemplate, nil
}

func (s *TemplateService) templateBaseFromRemote(remoteTemplate *models.ComposeTemplate) string {
	base := appfs.Slugify(remoteTemplate.Name)
	if base != "" {
		return base
	}
	parts := strings.Split(remoteTemplate.ID, ":")
	if len(parts) > 0 {
		base = appfs.Slugify(parts[len(parts)-1])
	}
	if base == "" {
		base = "template-" + uuid.NewString()
	}
	return base
}

func cloneTemplateMetadata(meta *models.ComposeTemplateMetadata) *models.ComposeTemplateMetadata {
	if meta == nil {
		return nil
	}
	return &models.ComposeTemplateMetadata{
		Version:          meta.Version,
		Author:           meta.Author,
		Tags:             meta.Tags,
		RemoteURL:        meta.RemoteURL,
		EnvURL:           meta.EnvURL,
		DocumentationURL: meta.DocumentationURL,
	}
}

func (s *TemplateService) invalidateRemoteCache() {
	s.remoteMu.Lock()
	s.remoteCache = remoteCache{}
	s.remoteMu.Unlock()

	s.registryMu.Lock()
	s.registryFetchMeta = make(map[string]*registryFetchMeta)
	s.registryMu.Unlock()
}

func (s *TemplateService) SyncLocalTemplatesFromFilesystem(ctx context.Context) error {
	return s.syncFilesystemTemplatesInternal(ctx)
}

func (s *TemplateService) upsertFilesystemTemplate(ctx context.Context, name, desc, compose string, envPtr *string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.ComposeTemplate
		q := tx.
			Where("is_remote = ? AND registry_id IS NULL AND description = ?", false, desc).
			First(&existing)

		if q.Error == nil {
			existing.Name = name
			existing.Content = compose
			existing.EnvContent = envPtr
			existing.IsCustom = true
			existing.IsRemote = false
			if err := tx.Save(&existing).Error; err != nil {
				return fmt.Errorf("update template %s: %w", existing.ID, err)
			}
			return nil
		}
		if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query existing template: %w", q.Error)
		}

		tpl := &models.ComposeTemplate{
			BaseModel:   models.BaseModel{ID: uuid.NewString()},
			Name:        name,
			Description: desc,
			Content:     compose,
			EnvContent:  envPtr,
			IsCustom:    true,
			IsRemote:    false,
			RegistryID:  nil,
			Registry:    nil,
			Metadata:    nil,
		}
		if err := tx.Create(tpl).Error; err != nil {
			return fmt.Errorf("insert template %s: %w", name, err)
		}
		return nil
	})
}

func (s *TemplateService) processFolderEntry(ctx context.Context, baseDir, folder string) error {
	compose, envPtr, desc, found, err := appfs.ReadFolderComposeTemplate(baseDir, folder)
	if err != nil || !found {
		return err
	}
	return s.upsertFilesystemTemplate(ctx, folder, desc, compose, envPtr)
}

func (s *TemplateService) syncFilesystemTemplatesInternal(ctx context.Context) error {
	s.fsSyncMu.Lock()
	defer s.fsSyncMu.Unlock()

	if !s.lastFsSync.IsZero() && time.Since(s.lastFsSync) < fsSyncInterval {
		return nil
	}

	dir, err := appfs.GetTemplatesDirectory(ctx)
	if err != nil {
		return fmt.Errorf("ensure templates dir: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	for _, ent := range entries {
		// Only process directories; root-level compose files are ignored to prevent duplication.
		if !ent.IsDir() {
			continue
		}
		if err := s.processFolderEntry(ctx, dir, ent.Name()); err != nil {
			slog.WarnContext(ctx, "failed to read folder template", "folder", ent.Name(), "error", err)
		}
	}

	s.lastFsSync = time.Now()
	return nil
}

func (s *TemplateService) getGlobalVariablesPath(ctx context.Context) (string, error) {
	projectsDirectory, err := appfs.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects"))
	if err != nil {
		return "", fmt.Errorf("failed to get projects directory: %w", err)
	}

	return filepath.Join(projectsDirectory, ".env.global"), nil
}

func (s *TemplateService) GetGlobalVariables(ctx context.Context) ([]env.Variable, error) {
	envPath, err := s.getGlobalVariablesPath(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		slog.DebugContext(ctx, "Global variables file does not exist yet", "path", envPath)
		return []env.Variable{}, nil
	}

	file, err := os.Open(envPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open global variables file: %w", err)
	}
	defer file.Close()

	vars := []env.Variable{}
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			slog.WarnContext(ctx, "Skipping invalid line in global variables file",
				"line", lineNum,
				"content", line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
		}

		vars = append(vars, env.Variable{
			Key:   key,
			Value: value,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading global variables file: %w", err)
	}

	return vars, nil
}

func (s *TemplateService) UpdateGlobalVariables(ctx context.Context, vars []env.Variable) error {
	envPath, err := s.getGlobalVariablesPath(ctx)
	if err != nil {
		return err
	}

	projectsDirectory := filepath.Dir(envPath)
	if err := os.MkdirAll(projectsDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create projects directory: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("# Global Environment Variables\n")
	builder.WriteString("# These variables are available to all projects\n")
	builder.WriteString("# Last updated: " + time.Now().Format(time.RFC3339) + "\n\n")

	for _, v := range vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}

		key := strings.TrimSpace(v.Key)
		value := strings.TrimSpace(v.Value)

		if strings.ContainsAny(value, " \t\n\r#") {
			value = fmt.Sprintf(`"%s"`, strings.ReplaceAll(value, `"`, `\"`))
		}

		builder.WriteString(fmt.Sprintf("%s=%s\n", key, value))
	}

	if err := appfs.WriteFileWithPerm(envPath, builder.String(), appfs.FilePerm); err != nil {
		return fmt.Errorf("failed to write global variables file: %w", err)
	}

	slog.InfoContext(ctx, "Updated global variables",
		"path", envPath,
		"count", len(vars))

	return nil
}

// ParseComposeServices extracts service names from a compose file content using compose-go
func (s *TemplateService) ParseComposeServices(ctx context.Context, composeContent string) []string {
	if composeContent == "" {
		return []string{}
	}

	// Create a temp directory with dummy .env file to satisfy env_file references
	tmpDir, err := os.MkdirTemp("", "arcane-compose-parse-*")
	if err != nil {
		slog.WarnContext(ctx, "Failed to create temp dir for compose parsing", "error", err)
		return []string{}
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy .env file to prevent env file errors
	envPath := filepath.Join(tmpDir, ".env")
	if err := appfs.WriteFileWithPerm(envPath, "", appfs.FilePerm); err != nil {
		slog.WarnContext(ctx, "Failed to create dummy env file", "error", err)
	}

	// Parse using compose-go
	configDetails := composetypes.ConfigDetails{
		ConfigFiles: []composetypes.ConfigFile{
			{
				Content: []byte(composeContent),
			},
		},
		WorkingDir:  tmpDir,
		Environment: composetypes.Mapping{},
	}

	project, err := composeloader.LoadWithContext(ctx, configDetails, composeloader.WithSkipValidation)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse compose services", "error", err)
		return []string{}
	}

	serviceNames := make([]string, 0, len(project.Services))
	for _, service := range project.Services {
		serviceNames = append(serviceNames, service.Name)
	}

	return serviceNames
}

// GetTemplateContentWithParsedData returns template content along with parsed metadata
func (s *TemplateService) GetTemplateContentWithParsedData(ctx context.Context, id string) (*tmpl.TemplateContent, error) {
	composeTemplate, err := s.GetTemplate(ctx, id)
	if err != nil {
		return nil, err
	}

	var outTemplate tmpl.Template
	if mapErr := mapper.MapStruct(composeTemplate, &outTemplate); mapErr != nil {
		return nil, fmt.Errorf("failed to map template: %w", mapErr)
	}

	var composeContent, envContent string
	if composeTemplate.IsRemote {
		composeContent, envContent, err = s.FetchTemplateContent(ctx, composeTemplate)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch template content: %w", err)
		}
	} else {
		composeContent = composeTemplate.Content
		if composeTemplate.EnvContent != nil {
			envContent = *composeTemplate.EnvContent
		}
	}

	// Parse services from compose content using compose-go library
	services := s.ParseComposeServices(ctx, composeContent)

	// Parse environment variables
	parsedEnvVars := templateutil.ParseEnvContent(envContent)
	envVars := make([]env.Variable, len(parsedEnvVars))
	for i, v := range parsedEnvVars {
		envVars[i] = env.Variable{Key: v.Key, Value: v.Value}
	}

	return &tmpl.TemplateContent{
		Template:     outTemplate,
		Content:      composeContent,
		EnvContent:   envContent,
		Services:     services,
		EnvVariables: envVars,
	}, nil
}

func (s *TemplateService) getMergedTemplates(ctx context.Context) ([]models.ComposeTemplate, error) {
	if err := s.syncFilesystemTemplatesInternal(ctx); err != nil {
		slog.WarnContext(ctx, "failed to sync filesystem templates", "error", err)
	}

	var templates []models.ComposeTemplate
	// Use Omit to avoid fetching heavy content fields which are not needed for listing
	if err := s.db.WithContext(ctx).Omit("Content", "EnvContent").Preload("Registry").Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("failed to get local templates: %w", err)
	}

	if err := s.ensureRemoteTemplatesLoaded(ctx); err != nil {
		slog.WarnContext(ctx, "failed to load remote templates", "error", err)
	} else {
		s.remoteMu.RLock()
		copied := make([]models.ComposeTemplate, len(s.remoteCache.templates))
		copy(copied, s.remoteCache.templates)
		s.remoteMu.RUnlock()

		if len(copied) > 0 {
			templates = append(templates, copied...)
		}
	}

	return templates, nil
}
