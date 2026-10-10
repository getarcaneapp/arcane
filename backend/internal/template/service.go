package template

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v2/env"
	tmpl "github.com/getarcaneapp/arcane/types/v2/template"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

type registryFetchMeta struct {
	LastModified string
	Templates    []ComposeTemplate
}

type TemplateService struct {
	db              *database.DB
	httpClient      *http.Client
	safeHTTPClient  *http.Client
	lookupIP        httpx.LookupIPFunc
	settingsService *settings.SettingsService

	remoteCache      *hot.HotCache[uint64, []ComposeTemplate]
	remoteGeneration atomic.Uint64

	registryMu        sync.RWMutex
	registryFetchMeta map[string]*registryFetchMeta
	registryErrors    map[string]string // last fetch error per registry ID, cleared on success

	fsSyncMu   sync.Mutex
	lastFsSync time.Time
}

const (
	remoteCacheDuration = 5 * time.Minute
	fsSyncInterval      = 1 * time.Minute

	remoteIDPrefix = "remote"
)

var errRemoteCacheInvalidated = errors.New("remote template cache invalidated")

func NewTemplateService(ctx context.Context, db *database.DB, httpClient *http.Client, settingsService *settings.SettingsService) *TemplateService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	service := &TemplateService{
		db:                db,
		httpClient:        httpClient,
		lookupIP:          httpx.DefaultLookupIP,
		settingsService:   settingsService,
		registryFetchMeta: make(map[string]*registryFetchMeta),
		registryErrors:    make(map[string]string),
	}
	revalidationCtx := context.WithoutCancel(ctx)
	loader := func(generations []uint64) (map[uint64][]ComposeTemplate, error) {
		loadCtx, cancel := context.WithTimeout(revalidationCtx, 2*time.Minute)
		defer cancel()
		catalogs := make(map[uint64][]ComposeTemplate, len(generations))
		for _, generation := range generations {
			templates, err := service.loadRemoteTemplates(loadCtx, generation)
			if err != nil {
				return nil, err
			}
			catalogs[generation] = templates
		}
		return catalogs, nil
	}
	service.remoteCache = hot.NewHotCache[uint64, []ComposeTemplate](hot.LRU, 1).
		WithTTL(remoteCacheDuration).
		WithLoaders(loader).
		WithRevalidation(24*time.Hour, loader).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		Build()

	if err := projects.EnsureDefaultTemplates(ctx, service.configuredTemplatesDirSetting(ctx)); err != nil {
		slog.WarnContext(ctx, "failed to ensure default templates", "error", err)
	}

	return service
}

// configuredTemplatesDirSetting returns the raw templates directory setting; callers resolve it.
func (s *TemplateService) configuredTemplatesDirSetting(ctx context.Context) string {
	if s.settingsService == nil {
		return ""
	}
	return s.settingsService.GetStringSetting(ctx, "templatesDirectory", "/app/data/templates")
}

// getTemplatesDirectory resolves the effective templates directory.
func (s *TemplateService) getTemplatesDirectory(ctx context.Context) (string, error) {
	return projects.GetTemplatesDirectory(ctx, strings.TrimSpace(s.configuredTemplatesDirSetting(ctx)))
}

func (s *TemplateService) remoteTemplates(ctx context.Context, refresh bool) ([]ComposeTemplate, error) {
	if s.remoteCache == nil {
		return nil, errors.New("remote template cache is not initialized")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		generation := s.remoteGeneration.Load()
		var templates []ComposeTemplate
		var err error
		if refresh {
			templates, err = s.loadRemoteTemplates(ctx, generation)
			s.registryMu.Lock()
			if generation == s.remoteGeneration.Load() && err == nil {
				s.remoteCache.Set(generation, templates)
			}
			s.registryMu.Unlock()
		} else {
			var found bool
			templates, found, err = s.remoteCache.Get(generation)
			if err == nil && !found {
				err = errors.New("remote template catalog not found")
			}
		}
		if generation != s.remoteGeneration.Load() {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to load remote templates: %w", err)
		}
		return templates, nil
	}
}

func (s *TemplateService) GetAllTemplates(ctx context.Context) ([]ComposeTemplate, error) {
	if err := s.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
		slog.WarnContext(ctx, "failed to sync filesystem templates", "error", err)
	}

	var templates []ComposeTemplate
	// Omit heavy content fields which are not needed for listing.
	if err := s.db.WithContext(ctx).Omit("Content", "EnvContent").Preload("Registry").Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("failed to get local templates: %w", err)
	}

	remoteTemplates, err := s.remoteTemplates(ctx, false)
	if err != nil {
		slog.WarnContext(ctx, "failed to load remote templates", "error", err)
	} else {
		templates = append(templates, cloneRemoteTemplates(remoteTemplates)...)
	}

	return templates, nil
}

func (s *TemplateService) GetAllTemplatesPaginated(ctx context.Context, params pagination.QueryParams) ([]tmpl.Template, pagination.Response, error) {
	templates, err := s.GetAllTemplates(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	items := make([]tmpl.Template, 0, len(templates))
	for _, t := range templates {
		var dtoItem tmpl.Template
		if mapStructErr := mapping.MapStruct(&t, &dtoItem); mapStructErr != nil {
			slog.WarnContext(ctx, "failed to map template to DTO", "error", mapStructErr, "templateId", t.ID)
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
					return kit.Ternary(a.IsRemote, 1, -1)
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[tmpl.Template]{
			{
				Key: "type",
				Fn: func(item tmpl.Template, filterValue string) bool {
					value, valid := kit.ParseBool(filterValue)
					return !valid || item.IsRemote == value
				},
			},
		},
	}

	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func (s *TemplateService) GetTemplate(ctx context.Context, id string) (*ComposeTemplate, error) {
	if err := s.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
		slog.WarnContext(ctx, "failed to sync filesystem templates", "error", err)
	}

	var template ComposeTemplate
	if err := s.db.WithContext(ctx).Preload("Registry").Where("id = ?", id).First(&template).Error; err == nil {
		return &template, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to query local template: %w", err)
	}

	templates, err := s.remoteTemplates(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("template %q lookup failed: registry refresh error: %w", id, err)
	}

	matchesID := func(t ComposeTemplate) bool { return t.ID == id }
	if i := slices.IndexFunc(templates, matchesID); i >= 0 {
		return &cloneRemoteTemplates(templates[i : i+1])[0], nil
	}

	// Force-refresh remote IDs (remote:registry:slug) on a miss; the cache may be stale.
	if strings.HasPrefix(id, remoteIDPrefix+":") {
		slog.InfoContext(ctx, "remote template not in cache, forcing registry refresh", "templateId", id, "cacheSize", len(templates))
		refreshed, refreshErr := s.remoteTemplates(ctx, true)
		if refreshErr != nil {
			return nil, fmt.Errorf("template %q not found and registry refresh failed: %w", id, refreshErr)
		}
		if i := slices.IndexFunc(refreshed, matchesID); i >= 0 {
			return &cloneRemoteTemplates(refreshed[i : i+1])[0], nil
		}
		notFoundErr := fmt.Errorf("template %q not found in any registered registry (cache size=%d after refresh)", id, len(refreshed))
		return nil, common.Classify(common.ErrTemplateNotFound, fmt.Errorf("template not found: %w", notFoundErr))
	}

	return nil, common.Classify(common.ErrTemplateNotFound, errors.New("template not found"))
}

func (s *TemplateService) CreateTemplate(ctx context.Context, template *ComposeTemplate) error {
	if err := normalization.Normalize(template); err != nil {
		return err
	}
	if template.ID == "" {
		template.ID = uuid.New().String()
	}
	template.IsCustom = true
	template.IsRemote = false
	setTemplateIconURL(template, projects.ResolveTemplateIconURL(ctx, template.Content, mo.PointerToOption(template.EnvContent).OrEmpty()))
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(template).Error; err != nil {
			return fmt.Errorf("failed to create template: %w", err)
		}
		return nil
	})
}

func (s *TemplateService) UpdateTemplate(ctx context.Context, id string, updates *ComposeTemplate) error {
	if err := normalization.Normalize(updates); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrTemplateNotFound, errors.New("template not found"))
			}
			return fmt.Errorf("failed to find template: %w", err)
		}

		if existing.IsRemote {
			return errors.New("cannot update remote template")
		}

		existing.Name = updates.Name
		existing.Description = updates.Description
		existing.Content = updates.Content
		existing.EnvContent = updates.EnvContent
		setTemplateIconURL(&existing, projects.ResolveTemplateIconURL(ctx, existing.Content, mo.PointerToOption(existing.EnvContent).OrEmpty()))
		if err := tx.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update template: %w", err)
		}
		return nil
	})
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrTemplateNotFound, errors.New("template not found"))
			}
			return fmt.Errorf("failed to find template: %w", err)
		}

		if existing.IsRemote {
			return errors.New("cannot delete remote template directly")
		}

		baseDir, err := s.getTemplatesDirectory(ctx)
		if err != nil {
			return fmt.Errorf("failed to get templates directory: %w", err)
		}

		templatePath := filepath.Join(baseDir, existing.Name)
		if entry, statErr := acfs.Stat(ctx, baseDir, "/"+existing.Name, false); statErr == nil && entry.IsDirectory {
			if _, detectComposeFileErr := projects.DetectComposeFile(ctx, "", templatePath); detectComposeFileErr == nil {
				if removeAllErr := acfs.RemoveAll(ctx, baseDir, entry.Path); removeAllErr != nil {
					return fmt.Errorf("failed to delete template directory: %w", removeAllErr)
				}
			}
		}
		if deleteTemplateErr := tx.Delete(&existing).Error; deleteTemplateErr != nil {
			return fmt.Errorf("failed to delete template: %w", deleteTemplateErr)
		}
		return nil
	})
}

// readDefaultTemplate reads one of the default template files from the configured templates directory.
func (s *TemplateService) readDefaultTemplate(ctx context.Context, fileName string) (string, error) {
	baseDir, err := s.getTemplatesDirectory(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get templates directory: %w", err)
	}
	content, err := acfs.ReadFile(ctx, baseDir, "/"+fileName)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (s *TemplateService) GetComposeTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplate(ctx, ".compose.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read compose template", "error", err)
		return ""
	}
	return content
}

func (s *TemplateService) GetSwarmStackTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplate(ctx, ".swarm-stack.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read swarm stack template", "error", err)
		return projects.DefaultSwarmStackTemplate()
	}
	return content
}

func (s *TemplateService) GetSwarmStackEnvTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplate(ctx, ".swarm-stack.env.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read swarm stack env template", "error", err)
		return projects.DefaultSwarmStackEnvTemplate()
	}
	return content
}

func (s *TemplateService) SaveComposeTemplate(ctx context.Context, content string) error {
	baseDir, err := s.getTemplatesDirectory(ctx)
	if err != nil {
		return fmt.Errorf("failed to get templates directory: %w", err)
	}
	return acfs.Write(ctx, baseDir, "/.compose.template", []byte(content), acfs.WriteOptions{Mode: utils.FilePerm})
}

func (s *TemplateService) GetEnvTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplate(ctx, ".env.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read env template", "error", err)
		return ""
	}
	return content
}

func (s *TemplateService) SaveEnvTemplate(ctx context.Context, content string) error {
	baseDir, err := s.getTemplatesDirectory(ctx)
	if err != nil {
		return fmt.Errorf("failed to get templates directory: %w", err)
	}
	return acfs.Write(ctx, baseDir, "/.env.template", []byte(content), acfs.WriteOptions{Mode: utils.FilePerm})
}

func (s *TemplateService) GetRegistries(ctx context.Context) ([]TemplateRegistry, error) {
	var registries []TemplateRegistry
	err := s.db.WithContext(ctx).Find(&registries).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get registries: %w", err)
	}
	return registries, nil
}

// GetRegistryFetchErrors returns a snapshot of the last fetch error per registry ID.
// An absent entry means the registry fetched successfully (or has never been attempted).
func (s *TemplateService) GetRegistryFetchErrors() map[string]string {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	out := make(map[string]string, len(s.registryErrors))
	maps.Copy(out, s.registryErrors)
	return out
}

func (s *TemplateService) CreateRegistry(ctx context.Context, registry *TemplateRegistry) error {
	if err := normalization.Normalize(registry); err != nil {
		return err
	}
	// Hydrate metadata if needed
	if registry.Name == "" || registry.Description == "" {
		if registry.URL == "" {
			return errors.New("registry URL is required")
		}
		if manifest, err := s.fetchRegistryManifest(ctx, registry.URL); err == nil {
			registry.Name = cmp.Or(registry.Name, manifest.Name)
			registry.Description = cmp.Or(registry.Description, manifest.Description)
		} else if registry.Name == "" || registry.Description == "" {
			return fmt.Errorf("failed to fetch registry manifest: %w", err)
		}
	}

	if err := normalization.Normalize(registry); err != nil {
		return err
	}
	if registry.ID == "" {
		registry.ID = uuid.New().String()
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

func (s *TemplateService) UpdateRegistry(ctx context.Context, id string, updates *TemplateRegistry) error {
	if err := normalization.Normalize(updates); err != nil {
		return err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing TemplateRegistry
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("registry not found")
			}
			return fmt.Errorf("failed to find registry: %w", err)
		}

		// Hydrate blank fields from the manifest; a changed URL must yield them.
		urlChanged := updates.URL != "" && updates.URL != existing.URL
		manifestURL := cmp.Or(updates.URL, existing.URL)
		if (urlChanged || updates.Name == "" || updates.Description == "") && manifestURL != "" {
			if manifest, err := s.fetchRegistryManifest(ctx, manifestURL); err == nil {
				updates.Name = cmp.Or(updates.Name, manifest.Name)
				updates.Description = cmp.Or(updates.Description, manifest.Description)
			} else if urlChanged && (updates.Name == "" || updates.Description == "") {
				return fmt.Errorf("failed to fetch registry manifest: %w", err)
			}
		}

		if err := normalization.Normalize(updates); err != nil {
			return err
		}
		if err := tx.Model(&TemplateRegistry{}).Where("id = ?", id).
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

func (s *TemplateService) DeleteRegistry(ctx context.Context, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ?", id).Delete(&TemplateRegistry{})
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

func (s *TemplateService) loadRemoteTemplates(ctx context.Context, generation uint64) (templates []ComposeTemplate, err error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "template.sync_remote")
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(templates)))
		tracing.End(span, err)
	}()

	registries, err := s.GetRegistries(ctx)
	if err != nil {
		return nil, err
	}

	var (
		mu                   sync.Mutex
		fetchErrors          []error
		enabledRegistries    int
		successfulRegistries int
	)

	g, groupCtx := errgroup.WithContext(ctx)

	for i := range registries {
		reg := registries[i]
		if !reg.Enabled {
			continue
		}
		enabledRegistries++

		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "template worker")

			remoteTemplates, fetchRegistryTemplatesErr := s.fetchRegistryTemplates(groupCtx, &reg, generation)
			if fetchRegistryTemplatesErr != nil {
				slog.WarnContext(groupCtx, "failed to fetch templates from registry", "registry", reg.Name, "url", reg.URL, "error", fetchRegistryTemplatesErr)
				s.registryMu.Lock()
				if generation == s.remoteGeneration.Load() {
					s.registryErrors[reg.ID] = fetchRegistryTemplatesErr.Error()
				}
				s.registryMu.Unlock()
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Errorf("registry %q: %w", reg.Name, fetchRegistryTemplatesErr))
				mu.Unlock()
				return nil // Don't fail the whole group if one registry fails
			}

			s.registryMu.Lock()
			if generation == s.remoteGeneration.Load() {
				delete(s.registryErrors, reg.ID)
			}
			s.registryMu.Unlock()

			mu.Lock()
			defer mu.Unlock()
			successfulRegistries++
			for _, template := range remoteTemplates {
				template.Registry = cloneRegistry(&reg)
				template.RegistryID = mo.EmptyableToOption(strings.TrimSpace(reg.ID)).ToPointer()
				templates = append(templates, template)
			}
			return nil
		})
	}

	waitErr := g.Wait()
	span.SetAttributes(
		attribute.Int("arcane.template.registry_count", enabledRegistries),
		attribute.Int("arcane.template.failed_registry_count", len(fetchErrors)),
	)
	if waitErr != nil {
		return nil, waitErr
	}
	if generation != s.remoteGeneration.Load() {
		return nil, errRemoteCacheInvalidated
	}
	if successfulRegistries == 0 && len(fetchErrors) > 0 {
		return nil, errors.Join(fetchErrors...)
	}

	return templates, nil
}

func (s *TemplateService) FetchRaw(ctx context.Context, rawURL string) ([]byte, error) {
	client, req, err := s.newSafeRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d for URL %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %w", rawURL, err)
	}
	return body, nil
}

// fetchRegistryTemplates performs a conditional GET; a 304 reuses the registry's cached templates.
func (s *TemplateService) fetchRegistryTemplates(ctx context.Context, reg *TemplateRegistry, generation uint64) (templates []ComposeTemplate, err error) {
	registryHost := ""
	if parsedURL, parseErr := url.Parse(reg.URL); parseErr == nil {
		registryHost = parsedURL.Host
	}
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "template.fetch_registry", trace.WithAttributes(
		attribute.String("arcane.template.registry_id", reg.ID),
		attribute.String("arcane.template.registry_host", registryHost),
	))
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", len(templates)))
		tracing.End(span, err)
	}()

	s.registryMu.RLock()
	if generation != s.remoteGeneration.Load() {
		s.registryMu.RUnlock()
		return nil, errRemoteCacheInvalidated
	}
	fetchMeta := s.registryFetchMeta[reg.ID]
	s.registryMu.RUnlock()

	client, req, err := s.newSafeRequest(ctx, http.MethodGet, reg.URL)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if fetchMeta != nil && fetchMeta.LastModified != "" {
		req.Header.Set("If-Modified-Since", fetchMeta.LastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	span.SetAttributes(attribute.Bool("arcane.template.not_modified", resp.StatusCode == http.StatusNotModified))
	if resp.StatusCode == http.StatusNotModified {
		if fetchMeta != nil {
			return cloneRemoteTemplates(fetchMeta.Templates), nil
		}
		return nil, errors.New("received 304 without cached data")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var regDTO tmpl.RemoteRegistry
	if unmarshalErr := json.Unmarshal(body, &regDTO); unmarshalErr != nil {
		return nil, fmt.Errorf("parse registry JSON: %w", unmarshalErr)
	}

	templates = make([]ComposeTemplate, 0, len(regDTO.Templates))
	for _, remote := range regDTO.Templates {
		templates = append(templates, ComposeTemplate{
			ID:          fmt.Sprintf("%s:%s:%s", remoteIDPrefix, reg.ID, remote.ID),
			Name:        remote.Name,
			Description: remote.Description,
			IsRemote:    true,
			RegistryID:  mo.EmptyableToOption(strings.TrimSpace(reg.ID)).ToPointer(),
			Registry:    cloneRegistry(reg),
			Metadata: &ComposeTemplateMetadata{
				Version:          mo.EmptyableToOption(strings.TrimSpace(remote.Version)).ToPointer(),
				Author:           mo.EmptyableToOption(strings.TrimSpace(remote.Author)).ToPointer(),
				Tags:             remote.Tags,
				RemoteURL:        mo.EmptyableToOption(strings.TrimSpace(remote.ComposeURL)).ToPointer(),
				EnvURL:           mo.EmptyableToOption(strings.TrimSpace(remote.EnvURL)).ToPointer(),
				DocumentationURL: mo.EmptyableToOption(strings.TrimSpace(remote.DocumentationURL)).ToPointer(),
				IconURL:          mo.EmptyableToOption(strings.TrimSpace(remote.IconURL)).ToPointer(),
				RegistryIconURL:  mo.EmptyableToOption(strings.TrimSpace(remote.IconURL)).ToPointer(),
			},
		})
	}

	newMeta := &registryFetchMeta{
		LastModified: resp.Header.Get("Last-Modified"),
		Templates:    cloneRemoteTemplates(templates),
	}
	s.registryMu.Lock()
	if generation != s.remoteGeneration.Load() {
		s.registryMu.Unlock()
		return nil, errRemoteCacheInvalidated
	}
	s.registryFetchMeta[reg.ID] = newMeta
	s.registryMu.Unlock()

	return templates, nil
}

func (s *TemplateService) fetchRegistryManifest(ctx context.Context, rawURL string) (*tmpl.RemoteRegistry, error) {
	body, err := s.FetchRaw(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var reg tmpl.RemoteRegistry
	if unmarshalErr := json.Unmarshal(body, &reg); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse registry JSON: %w", unmarshalErr)
	}
	if reg.Name == "" || len(reg.Templates) == 0 {
		return nil, errors.New("invalid registry manifest: missing required fields (name, templates)")
	}
	return &reg, nil
}

func (s *TemplateService) FetchTemplateContent(ctx context.Context, template *ComposeTemplate) (composeContent, envContent string, err error) {
	if !template.IsRemote {
		return template.Content, mo.PointerToOption(template.EnvContent).OrEmpty(), nil
	}
	if template.Metadata == nil || template.Metadata.RemoteURL == nil || strings.TrimSpace(*template.Metadata.RemoteURL) == "" {
		return "", "", fmt.Errorf("remote template %q is missing compose_url in registry metadata", template.ID)
	}

	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "template.fetch", trace.WithAttributes(
		attribute.String("arcane.template.id", template.ID),
		attribute.String("arcane.template.registry_id", mo.PointerToOption(template.RegistryID).OrEmpty()),
	))
	defer func() { tracing.End(span, err) }()

	composeBody, err := s.FetchRaw(ctx, *template.Metadata.RemoteURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch compose content from %s: %w", *template.Metadata.RemoteURL, err)
	}

	if envURL := mo.PointerToOption(template.Metadata.EnvURL).OrEmpty(); envURL != "" {
		envBody, envErr := s.FetchRaw(ctx, envURL)
		if envErr != nil {
			slog.WarnContext(ctx, "failed to fetch env content", "url", envURL, "error", envErr)
		}
		envContent = string(envBody)
	}

	return string(composeBody), envContent, nil
}

func (s *TemplateService) newSafeRequest(ctx context.Context, method, rawURL string) (*http.Client, *http.Request, error) {
	parsedURL, err := httpx.ValidateSafeRemoteURL(ctx, rawURL, s.lookupIP)
	if err != nil {
		return nil, nil, err
	}

	// Lazily built once under registryMu (double-checked) so concurrent requests share one client.
	s.registryMu.RLock()
	client := s.safeHTTPClient
	s.registryMu.RUnlock()

	if client == nil {
		s.registryMu.Lock()
		if s.safeHTTPClient == nil {
			safeClient, clientErr := httpx.NewSafeOutboundHTTPClient(s.httpClient, s.lookupIP)
			if clientErr != nil {
				slog.WarnContext(ctx, "failed to configure safe HTTP client", "error", clientErr)
			}
			s.safeHTTPClient = safeClient
		}
		client = s.safeHTTPClient
		s.registryMu.Unlock()

		if client == nil {
			return nil, nil, errors.New("failed to configure safe HTTP client")
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), http.NoBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request for %s: %w", rawURL, err)
	}

	return client, req, nil
}

func (s *TemplateService) DownloadTemplate(ctx context.Context, remoteTemplate *ComposeTemplate) (*ComposeTemplate, error) {
	if !remoteTemplate.IsRemote {
		return nil, errors.New("template is not remote")
	}

	base := projects.Slugify(remoteTemplate.Name)
	if base == "" {
		base = cmp.Or(projects.Slugify(remoteTemplate.ID[strings.LastIndex(remoteTemplate.ID, ":")+1:]), "template-"+uuid.New().String())
	}

	dir, composePath, envPath, err := projects.EnsureTemplateDir(ctx, s.configuredTemplatesDirSetting(ctx), base)
	if err != nil {
		return nil, err
	}
	srcDesc := fmt.Sprintf("Imported from %s/compose.yaml", dir)

	var resultTemplate *ComposeTemplate
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		findErr := tx.Where("is_remote = ? AND registry_id IS NULL AND (description = ? OR name = ?)", false, srcDesc, base).First(&existing).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to check existing template: %w", findErr)
		}

		composeContent, envContent, fetchErr := s.FetchTemplateContent(ctx, remoteTemplate)
		if fetchErr != nil {
			return fmt.Errorf("failed to fetch template content for download: %w", fetchErr)
		}
		envPtr, writeErr := projects.WriteTemplateFiles(composePath, envPath, composeContent, envContent)
		if writeErr != nil {
			return writeErr
		}

		if findErr == nil {
			existing.Content = composeContent
			existing.EnvContent = envPtr
			existing.Metadata = cloneTemplateMetadata(remoteTemplate.Metadata)
			if saveErr := tx.Save(&existing).Error; saveErr != nil {
				return fmt.Errorf("failed to update existing local template: %w", saveErr)
			}
			resultTemplate = &existing
			return nil
		}

		localTemplate := &ComposeTemplate{
			ID:          uuid.New().String(),
			Name:        base,
			Description: srcDesc,
			Content:     composeContent,
			EnvContent:  envPtr,
			IsCustom:    true,
			Metadata:    cloneTemplateMetadata(remoteTemplate.Metadata),
		}
		if createErr := tx.Create(localTemplate).Error; createErr != nil {
			return fmt.Errorf("failed to save local template: %w", createErr)
		}
		resultTemplate = localTemplate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resultTemplate, nil
}

func cloneTemplateMetadata(meta *ComposeTemplateMetadata) *ComposeTemplateMetadata {
	if meta == nil {
		return nil
	}
	return &ComposeTemplateMetadata{
		Version:          mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.Version).OrEmpty())).ToPointer(),
		Author:           mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.Author).OrEmpty())).ToPointer(),
		Tags:             append([]string(nil), meta.Tags...),
		RemoteURL:        mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.RemoteURL).OrEmpty())).ToPointer(),
		EnvURL:           mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.EnvURL).OrEmpty())).ToPointer(),
		DocumentationURL: mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.DocumentationURL).OrEmpty())).ToPointer(),
		IconURL:          mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.IconURL).OrEmpty())).ToPointer(),
		RegistryIconURL:  mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(meta.RegistryIconURL).OrEmpty())).ToPointer(),
	}
}

func cloneRemoteTemplates(items []ComposeTemplate) []ComposeTemplate {
	if len(items) == 0 {
		return nil
	}

	cloned := make([]ComposeTemplate, len(items))
	for i := range items {
		cloned[i] = items[i]
		cloned[i].RegistryID = mo.EmptyableToOption(strings.TrimSpace(mo.PointerToOption(items[i].RegistryID).OrEmpty())).ToPointer()
		cloned[i].Registry = cloneRegistry(items[i].Registry)
		cloned[i].Metadata = cloneTemplateMetadata(items[i].Metadata)
	}
	return cloned
}

func cloneRegistry(registry *TemplateRegistry) *TemplateRegistry {
	if registry == nil {
		return nil
	}

	return new(*registry)
}

func (s *TemplateService) invalidateRemoteCache() {
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	s.remoteGeneration.Add(1)
	s.registryFetchMeta = make(map[string]*registryFetchMeta)
	s.registryErrors = make(map[string]string)
	if s.remoteCache != nil {
		s.remoteCache.Purge()
	}
}

// SyncLocalTemplatesFromFilesystem imports template folders from the templates directory, at most once per fsSyncInterval.
func (s *TemplateService) SyncLocalTemplatesFromFilesystem(ctx context.Context) (err error) {
	s.fsSyncMu.Lock()
	defer s.fsSyncMu.Unlock()

	if !s.lastFsSync.IsZero() && time.Since(s.lastFsSync) < fsSyncInterval {
		return nil
	}

	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "template.sync_filesystem")
	synced, failed := 0, 0
	defer func() {
		span.SetAttributes(attribute.Int("arcane.result.count", synced), attribute.Int("arcane.template.failed_count", failed))
		tracing.End(span, err)
	}()

	dir, err := s.getTemplatesDirectory(ctx)
	if err != nil {
		return fmt.Errorf("ensure templates dir: %w", err)
	}

	entries, err := acfs.List(ctx, dir, "/")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	for _, ent := range entries {
		// Only process directories; root-level compose files are ignored to prevent duplication.
		if !ent.IsDirectory {
			continue
		}
		compose, envPtr, desc, found, folderErr := projects.ReadFolderComposeTemplate(ctx, dir, ent.Name)
		if folderErr == nil && !found {
			continue
		}
		if folderErr == nil {
			iconURL := projects.ResolveTemplateIconURL(ctx, compose, mo.PointerToOption(envPtr).OrEmpty())
			folderErr = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				// Match on description (the absolute compose path) or, for disk imports, the folder name so
				// directory reconfigurations and compose renames keep a single row.
				var existing ComposeTemplate
				findErr := tx.Where("is_remote = ? AND registry_id IS NULL AND (description = ? OR (name = ? AND description LIKE ?))", false, desc, ent.Name, "Imported from %").
					First(&existing).Error
				if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
					return fmt.Errorf("query existing template: %w", findErr)
				}

				tpl := &existing
				if findErr != nil {
					tpl = &ComposeTemplate{ID: uuid.New().String()}
				}
				tpl.Name = ent.Name
				tpl.Description = desc
				tpl.Content = compose
				tpl.EnvContent = envPtr
				tpl.IsCustom = true
				tpl.IsRemote = false
				setTemplateIconURL(tpl, iconURL)

				if findErr == nil {
					if saveErr := tx.Save(tpl).Error; saveErr != nil {
						return fmt.Errorf("update template %s: %w", tpl.ID, saveErr)
					}
					return nil
				}
				if createErr := tx.Create(tpl).Error; createErr != nil {
					return fmt.Errorf("insert template %s: %w", ent.Name, createErr)
				}
				return nil
			})
		}
		if folderErr != nil {
			failed++
			slog.WarnContext(ctx, "failed to read folder template", "folder", ent.Name, "error", folderErr)
			continue
		}
		synced++
	}

	s.lastFsSync = time.Now()
	return nil
}

func setTemplateIconURL(template *ComposeTemplate, iconURL *string) {
	if template == nil {
		return
	}

	if template.Metadata == nil {
		if iconURL == nil {
			return
		}
		template.Metadata = &ComposeTemplateMetadata{}
	}

	// Downloaded templates fall back to their registry icon.
	template.Metadata.IconURL = cmp.Or(iconURL, template.Metadata.RegistryIconURL)
	if template.Metadata.Version == nil &&
		template.Metadata.Author == nil &&
		len(template.Metadata.Tags) == 0 &&
		template.Metadata.RemoteURL == nil &&
		template.Metadata.EnvURL == nil &&
		template.Metadata.DocumentationURL == nil &&
		template.Metadata.IconURL == nil {
		template.Metadata = nil
	}
}

// GetTemplateContentWithParsedData returns template content along with parsed metadata
func (s *TemplateService) GetTemplateContentWithParsedData(ctx context.Context, id string) (*tmpl.TemplateContent, error) {
	composeTemplate, err := s.GetTemplate(ctx, id)
	if err != nil {
		return nil, err
	}

	composeContent, envContent, err := s.FetchTemplateContent(ctx, composeTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch template content: %w", err)
	}
	if !composeTemplate.IsRemote {
		setTemplateIconURL(composeTemplate, projects.ResolveTemplateIconURL(ctx, composeContent, envContent))
	}

	var outTemplate tmpl.Template
	if mapErr := mapping.MapStruct(composeTemplate, &outTemplate); mapErr != nil {
		return nil, fmt.Errorf("failed to map template: %w", mapErr)
	}

	// Parse services from compose content using compose-go library
	services := projects.ParseComposeServices(ctx, composeContent)

	// Parse environment variables
	parsedEnvVars := projects.ParseEnvContent(envContent)
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
