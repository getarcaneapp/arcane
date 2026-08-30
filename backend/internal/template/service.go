package template

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"uuid"

	"emperror.dev/errors"

	composeloader "github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/env"
	tmpl "github.com/getarcaneapp/arcane/types/v2/template"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type registryFetchMeta struct {
	LastModified string
	Templates    []ComposeTemplate
}

type TemplateService struct {
	db              *database.DB
	httpClient      *http.Client
	safeHTTPClient  *http.Client
	lookupIP        httputils.LookupIPFunc
	settingsService *settings.SettingsService

	remoteCache *hot.HotCache[struct{}, []ComposeTemplate]

	registryMu        sync.RWMutex
	registryFetchMeta map[string]*registryFetchMeta
	registryErrors    map[string]string // last fetch error per registry ID, cleared on success

	fsSyncMu   sync.Mutex
	lastFsSync time.Time
}

const (
	remoteCacheDuration         = 5 * time.Minute
	fsSyncInterval              = 1 * time.Minute
	remoteIconResolveLimit      = 4
	templateArcaneBlockKey      = "x-arcane"
	templateArcaneIconKey       = "icon"
	templateArcaneIconsAliasKey = "icons"
)

const remoteIDPrefix = "remote"

var errNoRemoteTemplates = errors.New("remote template registries returned no templates")

func NewTemplateService(ctx context.Context, db *database.DB, httpClient *http.Client, settingsService *settings.SettingsService) *TemplateService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	service := &TemplateService{
		db:                db,
		httpClient:        httpClient,
		lookupIP:          httputils.DefaultLookupIP,
		settingsService:   settingsService,
		registryFetchMeta: make(map[string]*registryFetchMeta),
		registryErrors:    make(map[string]string),
	}
	service.safeHTTPClient = service.newSafeHTTPClientInternal()
	revalidationCtx := context.WithoutCancel(ctx)
	loader := func(_ []struct{}) (map[struct{}][]ComposeTemplate, error) {
		loadCtx, cancel := context.WithTimeout(revalidationCtx, 2*time.Minute)
		defer cancel()
		templates, err := service.loadRemoteTemplates(loadCtx)
		if err != nil {
			return nil, err
		}
		if len(templates) == 0 {
			return nil, errNoRemoteTemplates
		}
		return map[struct{}][]ComposeTemplate{{}: templates}, nil
	}
	service.remoteCache = hot.NewHotCache[struct{}, []ComposeTemplate](hot.LRU, 1).
		WithTTL(remoteCacheDuration).
		WithLoaders(loader).
		WithRevalidation(24*time.Hour, loader).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		Build()

	if err := projects.EnsureDefaultTemplates(ctx, service.configuredTemplatesDirSettingInternal(ctx)); err != nil {
		slog.WarnContext(ctx, "failed to ensure default templates", "error", err)
	}

	return service
}

// configuredTemplatesDirSettingInternal returns the raw user-configured templates
// directory string (from the settingsService), without resolving it to an absolute
// path. Use this when calling helpers like projects.GetTemplatesDirectory that
// perform their own resolution.
func (s *TemplateService) configuredTemplatesDirSettingInternal(ctx context.Context) string {
	if s.settingsService == nil {
		return ""
	}
	return s.settingsService.GetStringSetting(ctx, "templatesDirectory", "/app/data/templates")
}

// getTemplatesDirectoryInternal resolves the effective templates directory by
// applying the same resolution rules used elsewhere for the projects directory.
func (s *TemplateService) getTemplatesDirectoryInternal(ctx context.Context) (string, error) {
	return projects.GetTemplatesDirectory(ctx, strings.TrimSpace(s.configuredTemplatesDirSettingInternal(ctx)))
}

func (s *TemplateService) ensureRemoteTemplatesLoaded(_ context.Context) error {
	if s.remoteCache == nil {
		return errors.New("remote template cache is not initialized")
	}
	templates, found, err := s.remoteCache.Get(struct{}{})
	if err != nil {
		return errors.WrapIf(err, "failed to load remote templates")
	}
	if !found || len(templates) == 0 {
		return errNoRemoteTemplates
	}
	return nil
}

func (s *TemplateService) refreshRemoteTemplates(ctx context.Context) error {
	templates, err := s.loadRemoteTemplates(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to load remote templates")
	}

	if len(templates) == 0 {
		return errNoRemoteTemplates
	}
	if s.remoteCache == nil {
		return errors.New("remote template cache is not initialized")
	}
	s.remoteCache.Set(struct{}{}, templates)
	return nil
}

func (s *TemplateService) GetAllTemplates(ctx context.Context) ([]ComposeTemplate, error) {
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
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func (s *TemplateService) GetTemplate(ctx context.Context, id string) (*ComposeTemplate, error) {
	if err := s.syncFilesystemTemplatesInternal(ctx); err != nil {
		slog.WarnContext(ctx, "failed to sync filesystem templates", "error", err)
	}

	var template ComposeTemplate
	if err := s.db.WithContext(ctx).Preload("Registry").Where("id = ?", id).First(&template).Error; err == nil {
		return &template, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.WrapIf(err, "failed to query local template")
	}

	if err := s.ensureRemoteTemplatesLoaded(ctx); err != nil && !errors.Is(err, errNoRemoteTemplates) {
		return nil, errors.WrapIff(err, "template %q lookup failed: registry refresh error", id)
	}

	if found := s.lookupRemoteFromCacheInternal(id); found != nil {
		return found, nil
	}

	// Remote IDs (remote:registry:slug) deserve a synchronous force-refresh on miss
	// before we return "not found" — the cache may be stale or the previous refresh
	// silently returned empty.
	if strings.HasPrefix(id, remoteIDPrefix+":") {
		slog.InfoContext(ctx, "remote template not in cache, forcing registry refresh", "templateID", id, "cacheSize", s.remoteCacheSizeInternal())
		if refreshErr := s.refreshRemoteTemplates(ctx); refreshErr != nil && !errors.Is(refreshErr, errNoRemoteTemplates) {
			return nil, errors.WrapIff(refreshErr, "template %q not found and registry refresh failed", id)
		}
		if found := s.lookupRemoteFromCacheInternal(id); found != nil {
			return found, nil
		}
		return nil, common.Classify(common.ErrTemplateNotFound, errors.WrapIf(errors.Errorf("template %q not found in any registered registry (cache size=%d after refresh)", id, s.remoteCacheSizeInternal()), "Template not found"))
	}

	return nil, common.Classify(common.ErrTemplateNotFound, errors.New("Template not found"))
}

func (s *TemplateService) lookupRemoteFromCacheInternal(id string) *ComposeTemplate {
	if s.remoteCache == nil {
		return nil
	}
	templates, found := s.remoteCache.Peek(struct{}{})
	if !found {
		return nil
	}
	for i := range templates {
		if templates[i].ID == id {
			cloned := cloneRemoteTemplates(templates[i : i+1])
			return &cloned[0]
		}
	}
	return nil
}

func (s *TemplateService) remoteCacheSizeInternal() int {
	if s.remoteCache == nil {
		return 0
	}
	templates, _ := s.remoteCache.Peek(struct{}{})
	return len(templates)
}

func (s *TemplateService) CreateTemplate(ctx context.Context, template *ComposeTemplate) error {
	if template.ID == "" {
		template.ID = uuid.New().String()
	}
	template.IsCustom = true
	template.IsRemote = false
	setTemplateIconURL(template, s.resolveTemplateIconURL(ctx, template.Content, mo.PointerToOption(template.EnvContent).OrEmpty()))
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(template).Error; err != nil {
			return errors.WrapIf(err, "failed to create template")
		}
		return nil
	})
}

func (s *TemplateService) UpdateTemplate(ctx context.Context, id string, updates *ComposeTemplate) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrTemplateNotFound, errors.New("Template not found"))
			}
			return errors.WrapIf(err, "failed to find template")
		}

		if existing.IsRemote {
			return errors.New("cannot update remote template")
		}

		existing.Name = updates.Name
		existing.Description = updates.Description
		existing.Content = updates.Content
		existing.EnvContent = updates.EnvContent
		setTemplateIconURL(&existing, s.resolveTemplateIconURL(ctx, existing.Content, mo.PointerToOption(existing.EnvContent).OrEmpty()))

		if err := tx.Save(&existing).Error; err != nil {
			return errors.WrapIf(err, "failed to update template")
		}

		return nil
	})
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrTemplateNotFound, errors.New("Template not found"))
			}
			return errors.WrapIf(err, "failed to find template")
		}

		if existing.IsRemote {
			return errors.New("cannot delete remote template directly")
		}

		baseDir, err := s.getTemplatesDirectoryInternal(ctx)
		if err != nil {
			return errors.WrapIf(err, "failed to get templates directory")
		}

		templatePath := filepath.Join(baseDir, existing.Name)
		if entry, err := acfs.Stat(ctx, baseDir, "/"+existing.Name, false); err == nil && entry.IsDirectory {
			if _, err := projects.DetectComposeFile(ctx, "", templatePath); err == nil {
				if err := acfs.RemoveAll(ctx, baseDir, entry.Path); err != nil {
					return errors.WrapIf(err, "failed to delete template directory")
				}
			}
		}

		if err := tx.Delete(&existing).Error; err != nil {
			return errors.WrapIf(err, "failed to delete template")
		}
		return nil
	})
}

// readDefaultTemplateInternal reads one of the default template files from the
// configured templates directory. These used to be read from a CWD-relative
// "data/templates", which only resolved when the process happened to run from
// the repository root.
func (s *TemplateService) readDefaultTemplateInternal(ctx context.Context, fileName string) (string, error) {
	baseDir, err := s.getTemplatesDirectoryInternal(ctx)
	if err != nil {
		return "", errors.WrapIf(err, "failed to get templates directory")
	}
	content, err := acfs.ReadFile(ctx, baseDir, "/"+fileName)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (s *TemplateService) GetComposeTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplateInternal(ctx, ".compose.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read compose template", "error", err)
		return ""
	}
	return content
}

func (s *TemplateService) GetSwarmStackTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplateInternal(ctx, ".swarm-stack.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read swarm stack template", "error", err)
		return projects.DefaultSwarmStackTemplate()
	}
	return content
}

func (s *TemplateService) GetSwarmStackEnvTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplateInternal(ctx, ".swarm-stack.env.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read swarm stack env template", "error", err)
		return projects.DefaultSwarmStackEnvTemplate()
	}
	return content
}

func (s *TemplateService) SaveComposeTemplate(ctx context.Context, content string) error {
	baseDir, err := s.getTemplatesDirectoryInternal(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to get templates directory")
	}
	return acfs.WriteFile(ctx, baseDir, "/.compose.template", []byte(content), utils.FilePerm)
}

func (s *TemplateService) GetEnvTemplate(ctx context.Context) string {
	content, err := s.readDefaultTemplateInternal(ctx, ".env.template")
	if err != nil {
		slog.WarnContext(ctx, "failed to read env template", "error", err)
		return ""
	}
	return content
}

func (s *TemplateService) SaveEnvTemplate(ctx context.Context, content string) error {
	baseDir, err := s.getTemplatesDirectoryInternal(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to get templates directory")
	}
	return acfs.WriteFile(ctx, baseDir, "/.env.template", []byte(content), utils.FilePerm)
}

func (s *TemplateService) GetRegistries(ctx context.Context) ([]TemplateRegistry, error) {
	var registries []TemplateRegistry
	err := s.db.WithContext(ctx).Find(&registries).Error
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get registries")
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
	// Hydrate metadata if needed
	if registry.Name == "" || registry.Description == "" {
		if registry.URL == "" {
			return errors.New("registry URL is required")
		}
		if manifest, err := s.fetchRegistryManifest(ctx, registry.URL); err == nil {
			if registry.Name == "" {
				registry.Name = manifest.Name
			}
			if registry.Description == "" {
				registry.Description = manifest.Description
			}
		} else if registry.Name == "" || registry.Description == "" {
			return errors.WrapIf(err, "failed to fetch registry manifest")
		}
	}

	if registry.ID == "" {
		registry.ID = uuid.New().String()
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(registry).Error; err != nil {
			return errors.WrapIf(err, "failed to create registry")
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
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing TemplateRegistry
		if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("registry not found")
			}
			return errors.WrapIf(err, "failed to find registry")
		}

		if err := s.hydrateRegistryUpdates(ctx, updates, &existing); err != nil {
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

func (s *TemplateService) hydrateRegistryUpdates(ctx context.Context, updates, existing *TemplateRegistry) error {
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
			return errors.WrapIf(err, "failed to fetch registry manifest")
		}
	}
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

func (s *TemplateService) loadRemoteTemplates(ctx context.Context) ([]ComposeTemplate, error) {
	registries, err := s.GetRegistries(ctx)
	if err != nil {
		return nil, err
	}

	var (
		mu        sync.Mutex
		templates []ComposeTemplate
	)

	g, groupCtx := errgroup.WithContext(ctx)

	for i := range registries {
		reg := registries[i]
		if !reg.Enabled {
			continue
		}

		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "template worker")

			remoteTemplates, err := s.fetchRegistryTemplates(groupCtx, &reg)
			if err != nil {
				slog.WarnContext(groupCtx, "failed to fetch templates from registry", "registry", reg.Name, "url", reg.URL, "error", err)
				s.registryMu.Lock()
				s.registryErrors[reg.ID] = err.Error()
				s.registryMu.Unlock()
				return nil // Don't fail the whole group if one registry fails
			}

			s.registryMu.Lock()
			delete(s.registryErrors, reg.ID)
			s.registryMu.Unlock()

			mu.Lock()
			defer mu.Unlock()
			for _, template := range remoteTemplates {
				template.Registry = cloneRegistry(&reg)
				template.RegistryID = mo.EmptyableToOption(strings.TrimSpace(reg.ID)).ToPointer()
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
	client, req, err := s.newSafeRequestInternal(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to fetch %s", url)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("HTTP status %d for URL %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to read response body from %s", url)
	}
	return body, nil
}

// fetchRegistryTemplates performs a conditional GET using If-Modified-Since.
// If the server replies 304 Not Modified, cached templates for the registry are reused.
func (s *TemplateService) fetchRegistryTemplates(ctx context.Context, reg *TemplateRegistry) ([]ComposeTemplate, error) {
	s.registryMu.RLock()
	fetchMeta := s.registryFetchMeta[reg.ID]
	s.registryMu.RUnlock()

	client, req, err := s.newSafeRequestInternal(ctx, http.MethodGet, reg.URL)
	if err != nil {
		return nil, errors.WrapIf(err, "create request")
	}
	if fetchMeta != nil && fetchMeta.LastModified != "" {
		req.Header.Set("If-Modified-Since", fetchMeta.LastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.WrapIf(err, "request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		if fetchMeta != nil {
			return cloneRemoteTemplates(fetchMeta.Templates), nil
		}
		return nil, errors.New("received 304 without cached data")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WrapIf(err, "read body")
	}

	var regDTO tmpl.RemoteRegistry
	if err := json.Unmarshal(body, &regDTO); err != nil {
		return nil, errors.WrapIf(err, "parse registry JSON")
	}

	templates := make([]ComposeTemplate, 0, len(regDTO.Templates))
	for _, remoteTemplate := range regDTO.Templates {
		templates = append(templates, s.convertRemoteToLocal(remoteTemplate, reg))
	}
	s.enrichRemoteTemplateIcons(ctx, templates)

	lm := resp.Header.Get("Last-Modified")
	newMeta := &registryFetchMeta{
		LastModified: lm,
		Templates:    cloneRemoteTemplates(templates),
	}
	s.registryMu.Lock()
	s.registryFetchMeta[reg.ID] = newMeta
	s.registryMu.Unlock()

	return templates, nil
}

func (s *TemplateService) fetchRegistryManifest(ctx context.Context, url string) (*tmpl.RemoteRegistry, error) {
	body, err := s.doGET(ctx, url)
	if err != nil {
		return nil, err
	}
	var reg tmpl.RemoteRegistry
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, errors.WrapIf(err, "failed to parse registry JSON")
	}
	if reg.Name == "" || len(reg.Templates) == 0 {
		return nil, errors.New("invalid registry manifest: missing required fields (name, templates)")
	}
	return &reg, nil
}

func (s *TemplateService) convertRemoteToLocal(remote tmpl.RemoteTemplate, registry *TemplateRegistry) ComposeTemplate {
	publicID := fmt.Sprintf("%s:%s:%s", remoteIDPrefix, registry.ID, remote.ID)

	return ComposeTemplate{
		ID:          publicID,
		Name:        remote.Name,
		Description: remote.Description,
		Content:     "",
		EnvContent:  nil,
		IsCustom:    false,
		IsRemote:    true,
		RegistryID:  mo.EmptyableToOption(strings.TrimSpace(registry.ID)).ToPointer(),
		Registry:    cloneRegistry(registry),
		Metadata: &ComposeTemplateMetadata{
			Version:          mo.EmptyableToOption(strings.TrimSpace(remote.Version)).ToPointer(),
			Author:           mo.EmptyableToOption(strings.TrimSpace(remote.Author)).ToPointer(),
			Tags:             remote.Tags,
			RemoteURL:        mo.EmptyableToOption(strings.TrimSpace(remote.ComposeURL)).ToPointer(),
			EnvURL:           mo.EmptyableToOption(strings.TrimSpace(remote.EnvURL)).ToPointer(),
			DocumentationURL: mo.EmptyableToOption(strings.TrimSpace(remote.DocumentationURL)).ToPointer(),
		},
	}
}

func (s *TemplateService) FetchTemplateContent(ctx context.Context, template *ComposeTemplate) (string, string, error) {
	if !template.IsRemote {
		envContent := ""
		if template.EnvContent != nil {
			envContent = *template.EnvContent
		}
		return template.Content, envContent, nil
	}
	if template.Metadata == nil || template.Metadata.RemoteURL == nil || strings.TrimSpace(*template.Metadata.RemoteURL) == "" {
		return "", "", errors.Errorf("remote template %q is missing compose_url in registry metadata", template.ID)
	}

	return s.fetchRemoteTemplateFiles(ctx, template)
}

func (s *TemplateService) fetchRemoteTemplateFiles(ctx context.Context, template *ComposeTemplate) (string, string, error) {
	if template == nil || template.Metadata == nil || template.Metadata.RemoteURL == nil {
		return "", "", errors.New("not a remote template or missing remote URL")
	}

	composeContent, err := s.fetchURL(ctx, *template.Metadata.RemoteURL)
	if err != nil {
		return "", "", errors.WrapIff(err, "failed to fetch compose content from %s", *template.Metadata.RemoteURL)
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

func (s *TemplateService) enrichRemoteTemplateIcons(ctx context.Context, templates []ComposeTemplate) {
	if len(templates) == 0 {
		return
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(remoteIconResolveLimit)

	for i := range templates {
		idx := i
		group.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "template worker")

			composeContent, envContent, err := s.fetchRemoteTemplateFiles(groupCtx, &templates[idx])
			if err != nil {
				slog.WarnContext(groupCtx, "failed to fetch remote template content for icon extraction", "templateID", templates[idx].ID, "error", err)
				setTemplateIconURL(&templates[idx], nil)
				return nil
			}

			setTemplateIconURL(&templates[idx], s.resolveTemplateIconURL(groupCtx, composeContent, envContent))
			return nil
		})
	}

	_ = group.Wait()
}

func (s *TemplateService) fetchURL(ctx context.Context, url string) (string, error) {
	body, err := s.doGET(ctx, url)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *TemplateService) newSafeHTTPClientInternal() *http.Client {
	client, err := httputils.NewSafeOutboundHTTPClient(s.httpClient, s.lookupIP)
	if err != nil {
		slog.Warn("failed to configure safe HTTP client", "error", err)
		return nil
	}
	return client
}

func (s *TemplateService) newSafeRequestInternal(ctx context.Context, method, rawURL string) (*http.Client, *http.Request, error) {
	parsedURL, err := httputils.ValidateSafeRemoteURL(ctx, rawURL, s.lookupIP)
	if err != nil {
		return nil, nil, err
	}

	// Double-checked under registryMu: the lazy init ran unsynchronized, so
	// concurrent template downloads raced on s.safeHTTPClient — a data race, and
	// each loser silently built and leaked its own client.
	s.registryMu.RLock()
	client := s.safeHTTPClient
	s.registryMu.RUnlock()

	if client == nil {
		s.registryMu.Lock()
		if s.safeHTTPClient == nil {
			s.safeHTTPClient = s.newSafeHTTPClientInternal()
		}
		client = s.safeHTTPClient
		s.registryMu.Unlock()

		if client == nil {
			return nil, nil, errors.New("failed to configure safe HTTP client")
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), nil)
	if err != nil {
		return nil, nil, errors.WrapIff(err, "failed to create request for %s", rawURL)
	}

	return client, req, nil
}

func (s *TemplateService) DownloadTemplate(ctx context.Context, remoteTemplate *ComposeTemplate) (*ComposeTemplate, error) {
	if !remoteTemplate.IsRemote {
		return nil, errors.New("template is not remote")
	}

	base := s.templateBaseFromRemote(remoteTemplate)

	dir, composePath, envPath, err := projects.EnsureTemplateDir(ctx, s.configuredTemplatesDirSettingInternal(ctx), base)
	if err != nil {
		return nil, err
	}
	srcDesc := fmt.Sprintf("Imported from %s/compose.yaml", dir)

	return s.downloadTemplateTransaction(ctx, remoteTemplate, base, composePath, envPath, srcDesc)
}

func (s *TemplateService) downloadTemplateTransaction(ctx context.Context, remoteTemplate *ComposeTemplate, base, composePath, envPath, srcDesc string) (*ComposeTemplate, error) {
	var resultTemplate *ComposeTemplate

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		if err := tx.
			Where("is_remote = ? AND registry_id IS NULL AND (description = ? OR name = ?)", false, srcDesc, base).
			First(&existing).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.WrapIf(err, "failed to check existing template")
		} else if err == nil {
			// Existing template found
			composeContent, envContent, err := s.FetchTemplateContent(ctx, remoteTemplate)
			if err != nil {
				return errors.WrapIf(err, "failed to fetch template content for existing local template")
			}

			envPtr, werr := projects.WriteTemplateFiles(composePath, envPath, composeContent, envContent)
			if werr != nil {
				return werr
			}

			existing.Content = composeContent
			existing.EnvContent = envPtr
			existing.Metadata = cloneTemplateMetadata(remoteTemplate.Metadata)

			if err := tx.Save(&existing).Error; err != nil {
				return errors.WrapIf(err, "failed to update existing local template")
			}
			resultTemplate = &existing
			return nil
		}

		// New template
		composeContent, envContent, err := s.FetchTemplateContent(ctx, remoteTemplate)
		if err != nil {
			return errors.WrapIf(err, "failed to fetch template content for download")
		}

		envPtr, werr := projects.WriteTemplateFiles(composePath, envPath, composeContent, envContent)
		if werr != nil {
			return werr
		}

		localTemplate := &ComposeTemplate{
			ID:          uuid.New().String(),
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
			return errors.WrapIf(err, "failed to save local template")
		}
		resultTemplate = localTemplate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resultTemplate, nil
}

func (s *TemplateService) templateBaseFromRemote(remoteTemplate *ComposeTemplate) string {
	base := projects.Slugify(remoteTemplate.Name)
	if base != "" {
		return base
	}
	parts := strings.Split(remoteTemplate.ID, ":")
	if len(parts) > 0 {
		base = projects.Slugify(parts[len(parts)-1])
	}
	if base == "" {
		base = "template-" + uuid.New().String()
	}
	return base
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
	if s.remoteCache != nil {
		s.remoteCache.Purge()
	}

	s.registryMu.Lock()
	s.registryFetchMeta = make(map[string]*registryFetchMeta)
	s.registryMu.Unlock()
}

func (s *TemplateService) SyncLocalTemplatesFromFilesystem(ctx context.Context) error {
	return s.syncFilesystemTemplatesInternal(ctx)
}

func (s *TemplateService) upsertFilesystemTemplate(ctx context.Context, name, desc, compose string, envPtr *string) error {
	iconURL := s.resolveTemplateIconURL(ctx, compose, mo.PointerToOption(envPtr).OrEmpty())

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ComposeTemplate
		// Match on description (which encodes the absolute compose path) OR name (folder name).
		// Matching on name as a fallback keeps a single row across templates-directory
		// reconfigurations or compose-filename renames within the same folder, but
		// only for templates previously imported from disk.
		q := tx.
			Where("is_remote = ? AND registry_id IS NULL AND (description = ? OR (name = ? AND description LIKE ?))", false, desc, name, "Imported from %").
			First(&existing)

		if q.Error == nil {
			existing.Name = name
			existing.Description = desc
			existing.Content = compose
			existing.EnvContent = envPtr
			existing.IsCustom = true
			existing.IsRemote = false
			setTemplateIconURL(&existing, iconURL)
			if err := tx.Save(&existing).Error; err != nil {
				return errors.WrapIff(err, "update template %s", existing.ID)
			}
			return nil
		}
		if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return errors.WrapIf(q.Error, "query existing template")
		}

		tpl := &ComposeTemplate{
			ID:          uuid.New().String(),
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
		setTemplateIconURL(tpl, iconURL)
		if err := tx.Create(tpl).Error; err != nil {
			return errors.WrapIff(err, "insert template %s", name)
		}
		return nil
	})
}

func (s *TemplateService) processFolderEntry(ctx context.Context, baseDir, folder string) error {
	compose, envPtr, desc, found, err := projects.ReadFolderComposeTemplate(ctx, baseDir, folder)
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

	dir, err := s.getTemplatesDirectoryInternal(ctx)
	if err != nil {
		return errors.WrapIf(err, "ensure templates dir")
	}

	entries, err := acfs.List(ctx, dir, "/")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.WrapIff(err, "read dir %s", dir)
	}

	for _, ent := range entries {
		// Only process directories; root-level compose files are ignored to prevent duplication.
		if !ent.IsDirectory {
			continue
		}
		if err := s.processFolderEntry(ctx, dir, ent.Name); err != nil {
			slog.WarnContext(ctx, "failed to read folder template", "folder", ent.Name, "error", err)
		}
	}

	s.lastFsSync = time.Now()
	return nil
}

// ParseComposeServices extracts service names from a compose file content using compose-go
func (s *TemplateService) ParseComposeServices(ctx context.Context, composeContent string) []string {
	if composeContent == "" {
		return []string{}
	}

	// Create a temp directory with dummy .env file to satisfy env_file references
	// System temp scratch: no acfs root exists for it.
	tmpDir, err := os.MkdirTemp("", "arcane-compose-parse-*")
	if err != nil {
		slog.WarnContext(ctx, "Failed to create temp dir for compose parsing", "error", err)
		return []string{}
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a dummy .env file to prevent env file errors
	envPath := filepath.Join(tmpDir, ".env")
	if err := projects.WriteFileWithPerm(envPath, "", utils.FilePerm); err != nil {
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

func (s *TemplateService) resolveTemplateIconURL(ctx context.Context, composeContent, envContent string) *string {
	if strings.TrimSpace(composeContent) == "" {
		return nil
	}

	// System temp scratch: no acfs root exists for it.
	tmpDir, err := os.MkdirTemp("", "arcane-template-icon-*")
	if err != nil {
		slog.WarnContext(ctx, "failed to create temp dir for template icon parsing", "error", err)
		return nil
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	envPath := filepath.Join(tmpDir, ".env")
	if err := projects.WriteFileWithPerm(envPath, envContent, utils.FilePerm); err != nil {
		slog.WarnContext(ctx, "failed to create temp env file for template icon parsing", "error", err)
	}

	envMap := make(composetypes.Mapping)
	for _, variable := range projects.ParseEnvContent(envContent) {
		if key := strings.TrimSpace(variable.Key); key != "" {
			envMap[key] = variable.Value
		}
	}
	envMap["PWD"] = tmpDir

	configDetails := composetypes.ConfigDetails{
		ConfigFiles: []composetypes.ConfigFile{
			{
				Content: []byte(composeContent),
			},
		},
		WorkingDir:  tmpDir,
		Environment: envMap,
	}

	project, err := composeloader.LoadWithContext(ctx, configDetails, composeloader.WithSkipValidation, func(opts *composeloader.Options) {
		opts.SkipConsistencyCheck = true
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to parse compose for template icon metadata", "error", err)
		return nil
	}

	if project == nil {
		return nil
	}

	arcaneBlock, ok := project.Extensions[templateArcaneBlockKey]
	if !ok {
		return nil
	}

	arcaneBlockMap, ok := utils.AsStringMap(arcaneBlock).Get()
	if !ok {
		return nil
	}

	icon := utils.FirstNonEmpty(
		utils.FirstNonEmpty(utils.Collect(arcaneBlockMap[templateArcaneIconKey], utils.ToString)...),
		utils.FirstNonEmpty(utils.Collect(arcaneBlockMap[templateArcaneIconsAliasKey], utils.ToString)...),
	)

	return mo.EmptyableToOption(strings.TrimSpace(icon)).ToPointer()
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

	template.Metadata.IconURL = iconURL
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

	var composeContent, envContent string
	if composeTemplate.IsRemote {
		composeContent, envContent, err = s.FetchTemplateContent(ctx, composeTemplate)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to fetch template content")
		}
	} else {
		composeContent = composeTemplate.Content
		if composeTemplate.EnvContent != nil {
			envContent = *composeTemplate.EnvContent
		}
	}

	setTemplateIconURL(composeTemplate, s.resolveTemplateIconURL(ctx, composeContent, envContent))

	var outTemplate tmpl.Template
	if mapErr := mapper.MapStruct(composeTemplate, &outTemplate); mapErr != nil {
		return nil, errors.WrapIf(mapErr, "failed to map template")
	}

	// Parse services from compose content using compose-go library
	services := s.ParseComposeServices(ctx, composeContent)

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

func (s *TemplateService) getMergedTemplates(ctx context.Context) ([]ComposeTemplate, error) {
	if err := s.syncFilesystemTemplatesInternal(ctx); err != nil {
		slog.WarnContext(ctx, "failed to sync filesystem templates", "error", err)
	}

	var templates []ComposeTemplate
	// Use Omit to avoid fetching heavy content fields which are not needed for listing
	if err := s.db.WithContext(ctx).Omit("Content", "EnvContent").Preload("Registry").Find(&templates).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to get local templates")
	}

	if err := s.ensureRemoteTemplatesLoaded(ctx); err != nil {
		slog.WarnContext(ctx, "failed to load remote templates", "error", err)
	} else {
		remoteTemplates, _ := s.remoteCache.Peek(struct{}{})
		copied := cloneRemoteTemplates(remoteTemplates)

		if len(copied) > 0 {
			templates = append(templates, copied...)
		}
	}

	return templates, nil
}
