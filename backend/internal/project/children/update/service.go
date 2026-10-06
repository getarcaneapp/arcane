// Package update owns project content updates: compose image edits and the
// rename journal that makes project renames recoverable.
package update

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"go.getarcane.app/acfs"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"
	updatertypes "go.getarcane.app/updater/types"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

// Service edits project Compose sources for image updates and persists
// rename journals in the KV store. Project rows stay with the parent, which
// supplies their state through callbacks.
type Service struct {
	kv              *kv.KVService
	dockerService   *docker.DockerClientService
	registryService *registry.ContainerRegistryService
	credentials     func(ctx context.Context) ([]containerregistry.Credential, error)
	projectState    func(ctx context.Context, projectID string) (name, path string, found bool, err error)
	restoreState    func(ctx context.Context, journal *project.RenameJournal) error
}

func New(
	kvService *kv.KVService,
	dockerService *docker.DockerClientService,
	registryService *registry.ContainerRegistryService,
	credentials func(ctx context.Context) ([]containerregistry.Credential, error),
	projectState func(ctx context.Context, projectID string) (name, path string, found bool, err error),
	restoreState func(ctx context.Context, journal *project.RenameJournal) error,
) *Service {
	return &Service{
		kv:              kvService,
		dockerService:   dockerService,
		registryService: registryService,
		credentials:     credentials,
		projectState:    projectState,
		restoreState:    restoreState,
	}
}

// SyncState holds the paths an in-flight rename protects from filesystem sync.
type SyncState struct {
	SkipDiscoveredPaths map[string]struct{}
	ProtectSeenPaths    map[string]struct{}
}

// composeImageEdit is an image scalar's source byte span and its replacement text.
type composeImageEdit struct {
	start, end int
	text       []byte
}

// SyncState collects the paths pending renames protect during filesystem sync.
func (s *Service) SyncState(ctx context.Context) SyncState {
	state := SyncState{
		SkipDiscoveredPaths: make(map[string]struct{}),
		ProtectSeenPaths:    make(map[string]struct{}),
	}
	if s == nil || s.kv == nil {
		return state
	}

	entries, err := s.kv.ListByPrefix(ctx, project.RenameJournalKeyPrefix)
	if err != nil {
		slog.WarnContext(ctx, "failed to list project rename journals during filesystem sync", "error", err)
		return state
	}

	for _, entry := range entries {
		var journal project.RenameJournal
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &journal); unmarshalErr != nil {
			slog.WarnContext(ctx, "failed to decode project rename journal during filesystem sync", "key", entry.Key, "error", unmarshalErr)
			continue
		}
		if !projects.RenameJournalFilesystemSyncPending(journal.Phase) {
			continue
		}
		if oldPath := strings.TrimSpace(journal.OldPath); oldPath != "" {
			state.ProtectSeenPaths[filepath.Clean(oldPath)] = struct{}{}
		}
		if newPath := strings.TrimSpace(journal.NewPath); newPath != "" {
			state.SkipDiscoveredPaths[filepath.Clean(newPath)] = struct{}{}
		}
	}

	return state
}

// Prepare builds the journal for renaming a project, or nil when the name
// does not change.
func (s *Service) Prepare(projectID, oldName, oldPath string, oldDirName, name *string, projectsDirectory string, migration volume.Migration) *project.RenameJournal {
	if s == nil || s.kv == nil || name == nil {
		return nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || oldName == newName {
		return nil
	}

	newDirName := strings.TrimSpace(projects.SanitizeProjectName(newName))
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return nil
	}

	journal := &project.RenameJournal{
		ProjectID:  projectID,
		OldName:    oldName,
		NewName:    newName,
		OldPath:    filepath.Clean(oldPath),
		NewPath:    filepath.Clean(filepath.Join(projectsDirectory, newDirName)),
		NewDirName: newDirName,
		Phase:      project.RenameJournalPhaseStarted,
	}
	if oldDirName != nil {
		journal.OldDirName = new(*oldDirName)
	}

	if source, ok := migration.(volume.JournalSource); ok {
		journal.Volumes = source.JournalVolumes()
	}

	return journal
}

// WriteJournal stores the journal at the given phase.
func (s *Service) WriteJournal(ctx context.Context, journal *project.RenameJournal, phase string) error {
	if s == nil || s.kv == nil || journal == nil {
		return nil
	}
	journal.Phase = phase
	journal.UpdatedAt = time.Now().UTC()

	payload, err := json.Marshal(journal)
	if err != nil {
		return fmt.Errorf("marshal project rename journal: %w", err)
	}

	if setErr := s.kv.Set(ctx, project.RenameJournalKeyPrefix+journal.ProjectID, string(payload)); setErr != nil {
		return fmt.Errorf("write project rename journal: %w", setErr)
	}
	return nil
}

func (s *Service) clearJournal(ctx context.Context, projectID string) error {
	if s == nil || s.kv == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kv.Delete(ctx, project.RenameJournalKeyPrefix+projectID)
}

func (s *Service) writeRollbackCleanup(ctx context.Context, journal *project.RenameJournal) error {
	if s == nil || s.kv == nil || journal == nil || strings.TrimSpace(journal.ProjectID) == "" || len(journal.Volumes) == 0 {
		return nil
	}

	cleanup := project.RenameRollbackCleanup{
		ProjectID: journal.ProjectID,
		OldName:   journal.OldName,
		OldPath:   filepath.Clean(journal.OldPath),
		NewName:   journal.NewName,
		NewPath:   filepath.Clean(journal.NewPath),
		Volumes:   journal.Volumes,
		UpdatedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(cleanup)
	if err != nil {
		return fmt.Errorf("marshal project rename rollback cleanup: %w", err)
	}
	if setErr := s.kv.Set(ctx, project.RenameRollbackCleanupKeyPrefix+journal.ProjectID, string(payload)); setErr != nil {
		return fmt.Errorf("write project rename rollback cleanup: %w", setErr)
	}
	return nil
}

func (s *Service) clearRollbackCleanup(ctx context.Context, projectID string) error {
	if s == nil || s.kv == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kv.Delete(ctx, project.RenameRollbackCleanupKeyPrefix+projectID)
}

// RecoverAll replays every pending rename journal and rollback cleanup.
func (s *Service) RecoverAll(ctx context.Context) error {
	if s == nil || s.kv == nil {
		return nil
	}

	entries, err := s.kv.ListByPrefix(ctx, project.RenameJournalKeyPrefix)
	if err != nil {
		return err
	}

	var recoverErr error
	for _, entry := range entries {
		var journal project.RenameJournal
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &journal); unmarshalErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("decode project rename journal %s: %w", entry.Key, unmarshalErr))
			continue
		}
		if journalErr := s.recoverJournal(ctx, &journal); journalErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename journal %s: %w", entry.Key, journalErr))
		}
	}

	cleanups, err := s.kv.ListByPrefix(ctx, project.RenameRollbackCleanupKeyPrefix)
	if err != nil {
		return errors.Join(recoverErr, err)
	}
	for _, entry := range cleanups {
		var cleanup project.RenameRollbackCleanup
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &cleanup); unmarshalErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("decode project rename rollback cleanup %s: %w", entry.Key, unmarshalErr))
			continue
		}
		if cleanupErr := s.recoverRollbackCleanup(ctx, &cleanup); cleanupErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename rollback cleanup %s: %w", entry.Key, cleanupErr))
		}
	}
	return recoverErr
}

// RecoverProject replays one project's pending rename journal.
func (s *Service) RecoverProject(ctx context.Context, projectID string) error {
	if s == nil || s.kv == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}

	raw, ok, err := s.kv.Get(ctx, project.RenameJournalKeyPrefix+projectID)
	if err != nil || !ok {
		return err
	}

	var journal project.RenameJournal
	if unmarshalErr := json.Unmarshal([]byte(raw), &journal); unmarshalErr != nil {
		return fmt.Errorf("decode project rename journal: %w", unmarshalErr)
	}
	return s.recoverJournal(ctx, &journal)
}

func (s *Service) recoverJournal(ctx context.Context, journal *project.RenameJournal) error {
	if s == nil || journal == nil || strings.TrimSpace(journal.ProjectID) == "" {
		return nil
	}

	name, path, found, stateErr := s.projectState(ctx, journal.ProjectID)
	if stateErr != nil {
		return fmt.Errorf("load project for rename recovery: %w", stateErr)
	}

	projectCommitted := found && (name == journal.NewName || filepath.Clean(path) == filepath.Clean(journal.NewPath))
	return projects.RecoverRenameJournal(ctx, journal, projectCommitted, s.Operations())
}

func (s *Service) recoverRollbackCleanup(ctx context.Context, cleanup *project.RenameRollbackCleanup) error {
	if s == nil || cleanup == nil || strings.TrimSpace(cleanup.ProjectID) == "" {
		return nil
	}
	if len(cleanup.Volumes) == 0 {
		return s.clearRollbackCleanup(ctx, cleanup.ProjectID)
	}

	name, path, found, stateErr := s.projectState(ctx, cleanup.ProjectID)
	if stateErr != nil {
		return fmt.Errorf("load project for rename rollback cleanup: %w", stateErr)
	}
	if !found {
		slog.WarnContext(ctx, "clearing project rename rollback cleanup because project no longer exists", "projectId", cleanup.ProjectID)
		return s.clearRollbackCleanup(ctx, cleanup.ProjectID)
	}

	if name != cleanup.OldName || filepath.Clean(path) != filepath.Clean(cleanup.OldPath) {
		slog.WarnContext(ctx, "clearing project rename rollback cleanup because project state changed", "projectId", cleanup.ProjectID, "projectName", name, "projectPath", path)
		return s.clearRollbackCleanup(ctx, cleanup.ProjectID)
	}

	return projects.CleanupRenameRollbackTargets(ctx, cleanup, s.Operations())
}

func (s *Service) dockerClient(ctx context.Context, dockerRequired bool) (*client.Client, error) {
	if !dockerRequired {
		return nil, nil
	}
	if s.dockerService == nil {
		return nil, errors.New("docker service unavailable")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	return dockerClient, nil
}

// Operations exposes journal persistence to the shared rename engine.
func (s *Service) Operations() project.RenameRecoveryOperations {
	return project.RenameRecoveryOperations{
		Docker:               s.dockerClient,
		WriteJournal:         s.WriteJournal,
		ClearJournal:         s.clearJournal,
		ClearRollbackCleanup: s.clearRollbackCleanup,
		WriteRollbackCleanup: s.writeRollbackCleanup,
		RestoreState:         s.restoreState,
	}
}

func PersistProjectServiceImages(ctx context.Context, projectPath, logical string, original, updated []byte) error {
	entry, err := acfs.Stat(ctx, projectPath, logical, false)
	if err != nil {
		return fmt.Errorf("inspect Compose source: %w", err)
	}
	if entry.IsSymlink {
		return fmt.Errorf("tag updates refuse a symlinked Compose source: %s", filepath.Base(logical))
	}
	current, err := acfs.ReadFile(ctx, projectPath, logical)
	if err != nil {
		return fmt.Errorf("recheck Compose source: %w", err)
	}
	if !bytes.Equal(current, original) {
		return errors.New("Compose source changed during the image update; check updates again") //nolint:staticcheck // Preserve the existing error message.
	}
	if writeErr := acfs.Write(ctx, projectPath, logical, updated, acfs.WriteOptions{Mode: os.FileMode(entry.UnixMode).Perm()}); writeErr != nil {
		return fmt.Errorf("persist Compose image changes: %w", writeErr)
	}
	return nil
}

func composeImageField(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func validateImageUpdateSource(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return errors.New("tag updates do not rewrite Compose YAML anchors or aliases; use explicit service images")
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "<<" {
				return fmt.Errorf("tag updates do not rewrite Compose %s declarations; update their source manually", node.Content[i].Value)
			}
		}
	}
	for _, child := range node.Content {
		if err := validateImageUpdateSource(child); err != nil {
			return err
		}
	}
	return nil
}

func PrepareProjectServiceImages(source []byte, effective *types.Project, changes map[string]updatertypes.ServiceImageChange) ([]byte, []string, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, fmt.Errorf("parse Compose source: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("tag updates require a single YAML document")
	}
	if len(document.Content) != 1 {
		return nil, nil, errors.New("Compose source must contain a mapping") //nolint:staticcheck // Preserve the existing error message.
	}
	if err := validateImageUpdateSource(document.Content[0]); err != nil {
		return nil, nil, err
	}
	if composeImageField(document.Content[0], "include") != nil {
		return nil, nil, errors.New("tag updates do not rewrite Compose includes; update their source manually")
	}
	servicesNode := composeImageField(document.Content[0], "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return nil, nil, errors.New("Compose source has no services mapping") //nolint:staticcheck // Preserve the existing error message.
	}
	serviceNames := make([]string, 0, len(changes))
	// Rewrite image scalars in place; re-encoding the node tree drops blank lines and operator formatting.
	edits := make([]composeImageEdit, 0, len(changes))
	for name, change := range changes {
		service, ok := effective.Services[name]
		if !ok {
			return nil, nil, fmt.Errorf("service %s is not active in the Compose project", name)
		}
		expected, target := refs.NormalizeImageUpdateRef(change.ExpectedRef), refs.NormalizeImageUpdateRef(change.TargetRef)
		if expected == "" || target == "" || refs.IsImageIDLikeReference(change.ExpectedRef) || refs.IsImageIDLikeReference(change.TargetRef) {
			return nil, nil, fmt.Errorf("service %s requires mutable image references", name)
		}
		if refs.NormalizeImageUpdateRef(service.Image) != expected && refs.NormalizeImageUpdateRef(service.Image) != target {
			return nil, nil, fmt.Errorf("service %s image changed since update check: expected %s, found %s", name, change.ExpectedRef, service.Image)
		}
		serviceNode := composeImageField(servicesNode, name)
		if composeImageField(serviceNode, "extends") != nil {
			return nil, nil, fmt.Errorf("tag updates do not rewrite extended service %s; update its source manually", name)
		}
		imageNode := composeImageField(serviceNode, "image")
		if imageNode == nil || imageNode.Kind != yaml.ScalarNode || imageNode.Tag != "!!str" {
			return nil, nil, fmt.Errorf("service %s requires an explicit image scalar in the authoritative Compose file", name)
		}
		if !strings.Contains(imageNode.Value, "$") && refs.NormalizeImageUpdateRef(imageNode.Value) != expected && refs.NormalizeImageUpdateRef(imageNode.Value) != target {
			return nil, nil, fmt.Errorf("service %s source image changed since Compose was loaded", name)
		}
		edit, err := composeImageSourceEdit(source, imageNode, change.TargetRef)
		if err != nil {
			return nil, nil, fmt.Errorf("service %s: %w", name, err)
		}
		edits = append(edits, edit)
		serviceNames = append(serviceNames, name)
	}
	slices.Sort(serviceNames)
	// Splice from the end so earlier spans keep their offsets; distinct scalars never overlap.
	slices.SortFunc(edits, func(a, b composeImageEdit) int { return b.start - a.start })
	updated := slices.Clone(source)
	for _, edit := range edits {
		updated = slices.Concat(updated[:edit.start], edit.text, updated[edit.end:])
	}
	return updated, serviceNames, nil
}

// composeImageSourceEdit finds an image scalar's single-line source span using YAML's line-break rules
// and renders the target in the scalar's original style.
func composeImageSourceEdit(source []byte, node *yaml.Node, target string) (composeImageEdit, error) {
	const lineBreaks = "\r\n\u0085\u2028\u2029"
	edit := composeImageEdit{start: len(source) - len(bytes.TrimPrefix(source, []byte("\xEF\xBB\xBF")))}
	for line, column := 1, 1; line < node.Line || (line == node.Line && column < node.Column); column++ {
		if edit.start >= len(source) {
			return edit, errors.New("image scalar position is outside the Compose source")
		}
		r, size := utf8.DecodeRune(source[edit.start:])
		edit.start += size
		if r == '\r' && bytes.HasPrefix(source[edit.start:], []byte("\n")) {
			edit.start++
		}
		if strings.ContainsRune(lineBreaks, r) {
			line, column = line+1, 0
		}
	}
	// The shortest same-line span that decodes to the parsed value is the scalar's source text, including tags and escapes.
	found := false
	for edit.end = edit.start; !found && edit.end < len(source); {
		r, size := utf8.DecodeRune(source[edit.end:])
		if strings.ContainsRune(lineBreaks, r) {
			break
		}
		edit.end += size
		var decoded string
		found = yaml.Unmarshal(source[edit.start:edit.end], &decoded) == nil && decoded == node.Value
	}
	if !found {
		return edit, errors.New("tag updates require a single-line image scalar")
	}
	text, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: node.Style, Value: target})
	edit.text = bytes.TrimSuffix(text, []byte("\n"))
	if err != nil || bytes.ContainsAny(edit.text, "\r\n") {
		return edit, fmt.Errorf("image %s cannot be written as a single-line scalar", target)
	}
	return edit, nil
}

// EnsureProjectEnvReadable rejects operator-driven file writes into a project
// whose .env exists but cannot be read by the runtime user.
func EnsureProjectEnvReadable(ctx context.Context, projectsDirectory, projectPath string) error {
	cfgErr := projects.CheckProjectEnvAccess(ctx, projectsDirectory, projectPath)
	if cfgErr == nil || !cfgErr.BlocksOperations {
		return nil
	}
	return common.Classify(
		common.ErrProjectEnvUnreadable,
		fmt.Errorf(
			"%s is not readable by the runtime user (uid %d, gid %d); fix its ownership/read permission or set PUID/PGID to a user that can read it",
			cfgErr.Path,
			cfgErr.UID,
			cfgErr.GID,
		),
	)
}

// ImageChanges discovers newer image tags allowed by each service's update
// policy. GitOps-managed projects cannot have their source edited.
func (s *Service) ImageChanges(ctx context.Context, effective *types.Project, gitOpsManaged bool) (map[string]updatertypes.ServiceImageChange, error) {
	policy := updater.DefaultLabelPolicy()
	changes := make(map[string]updatertypes.ServiceImageChange)
	for _, key := range effective.ServiceNames() {
		service := effective.Services[key]
		name := service.Name
		configuredPolicy := policy.TagPolicy(service.Labels)
		if service.Build != nil && (configuredPolicy.Strategy == "" || configuredPolicy.Strategy == "auto") && configuredPolicy.Constraint == "" && configuredPolicy.TagPattern == "" {
			continue
		}
		immutable := refs.IsDigestPinnedReference(service.Image) || refs.IsImageIDLikeReference(service.Image)
		if configuredPolicy.Strategy == "tag" && immutable {
			return nil, fmt.Errorf("service %s has an immutable image reference", name)
		}
		tagPolicy, err := tagpolicy.Resolve(service.Image, configuredPolicy)
		if err != nil {
			return nil, fmt.Errorf("resolve service %s update policy: %w", name, err)
		}
		if tagPolicy.Strategy == "digest" {
			continue
		}
		switch {
		case policy.IsUpdateDisabled(service.Labels):
			return nil, fmt.Errorf("updates are disabled for service %s", name)
		case service.Build != nil:
			return nil, fmt.Errorf("tag discovery is unsupported for locally built service %s", name)
		case gitOpsManaged:
			return nil, errors.New("tag updates cannot edit a GitOps-managed project; update image tags in the source repository")
		case immutable:
			return nil, fmt.Errorf("service %s has an immutable image reference", name)
		}
		parsed, err := refs.NormalizeReference(service.Image)
		if err != nil {
			return nil, fmt.Errorf("parse service %s image: %w", name, err)
		}
		if s.registryService == nil {
			return nil, errors.New("registry service unavailable for tag updates")
		}
		credentials, err := s.credentials(ctx)
		if err != nil {
			return nil, err
		}
		tags, err := s.registryService.ListImageTags(ctx, service.Image, credentials)
		if err != nil {
			return nil, fmt.Errorf("list service %s image tags: %w", name, err)
		}
		selected, err := tagpolicy.Select(parsed.Tag, tags, tagPolicy)
		if err != nil {
			return nil, fmt.Errorf("select service %s image tag: %w", name, err)
		}
		if selected != parsed.Tag {
			changes[key] = updatertypes.ServiceImageChange{ExpectedRef: service.Image, TargetRef: parsed.RegistryHost + "/" + parsed.Repository + ":" + selected}
		}
	}
	return changes, nil
}

// ApplyImageChanges rewrites the image fields of the single authoritative
// Compose file and returns the services whose images changed.
func (s *Service) ApplyImageChanges(ctx context.Context, projectPath string, effective *types.Project, changes map[string]updatertypes.ServiceImageChange) ([]string, error) {
	if len(effective.ComposeFiles) != 1 {
		return nil, errors.New("tag updates require one authoritative Compose file; multi-file selections and overrides must be updated manually")
	}
	logical, err := acfs.LogicalPath(projectPath, effective.ComposeFiles[0])
	if err != nil {
		return nil, fmt.Errorf("tag update Compose file must be inside the project directory: %w", err)
	}
	original, err := acfs.ReadFile(ctx, projectPath, logical)
	if err != nil {
		return nil, fmt.Errorf("read Compose source for tag update: %w", err)
	}
	updated, services, err := PrepareProjectServiceImages(original, effective, changes)
	if err != nil {
		return nil, err
	}
	if persistErr := PersistProjectServiceImages(ctx, projectPath, logical, original, updated); persistErr != nil {
		return nil, persistErr
	}
	return services, nil
}
