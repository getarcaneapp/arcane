package update

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/template"
	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/updater"
	"kit.getarcane.app/updater/pkg/utils/tagpolicy"
	"kit.getarcane.app/updater/refs"
	updatertypes "kit.getarcane.app/updater/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/pkg/imageref"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	// Project rows stay with the parent, which supplies their state through callbacks.
	Service struct {
		kv              *kv.KVService
		dockerService   *docker.DockerClientService
		registryService *registry.ContainerRegistryService
		credentials     func(ctx context.Context) ([]types.ContainerRegistryCredential, error)
		projectState    func(ctx context.Context, projectID string) (name, path string, found bool, err error)
		restoreState    func(ctx context.Context, journal *types.ProjectRenameJournal) error
		resolveEnv      func(ctx context.Context, projectPath string, effective *composeTypes.Project, values map[string]string) (*composeTypes.Project, string, error)
		persistEnv      func(ctx context.Context, projectPath, content string) error
	}

	Params struct {
		KVService       *kv.KVService
		DockerService   *docker.DockerClientService
		RegistryService *registry.ContainerRegistryService
		Credentials     func(ctx context.Context) ([]types.ContainerRegistryCredential, error)
		ProjectState    func(ctx context.Context, projectID string) (name, path string, found bool, err error)
		RestoreState    func(ctx context.Context, journal *types.ProjectRenameJournal) error
		ResolveEnv      func(ctx context.Context, projectPath string, effective *composeTypes.Project, values map[string]string) (*composeTypes.Project, string, error)
		PersistEnv      func(ctx context.Context, projectPath, content string) error
	}

	ImageEdits struct {
		Compose   []byte
		Env       map[string]string
		Followers []string
		Services  []string
	}

	SyncState struct {
		SkipDiscoveredPaths map[string]struct{}
		ProtectSeenPaths    map[string]struct{}
	}

	RenameRequest struct {
		Migration         types.VolumeMigration
		OldDirName        *string
		Name              *string
		ProjectID         string
		OldName           string
		OldPath           string
		ProjectsDirectory string
	}
)

const imageVariableMarker = "\x00"

func New(p Params) *Service {
	return &Service{
		kv:              p.KVService,
		dockerService:   p.DockerService,
		registryService: p.RegistryService,
		credentials:     p.Credentials,
		projectState:    p.ProjectState,
		restoreState:    p.RestoreState,
		resolveEnv:      p.ResolveEnv,
		persistEnv:      p.PersistEnv,
	}
}

func (s *Service) SyncState(ctx context.Context) SyncState {
	state := SyncState{
		SkipDiscoveredPaths: make(map[string]struct{}),
		ProtectSeenPaths:    make(map[string]struct{}),
	}
	entries, err := s.kv.ListByPrefix(ctx, types.ProjectRenameJournalKeyPrefix)
	if err != nil {
		slog.WarnContext(ctx, "failed to list project rename journals during filesystem sync", "error", err)
		return state
	}

	for _, entry := range entries {
		var journal types.ProjectRenameJournal
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

func (s *Service) Prepare(req RenameRequest) *types.ProjectRenameJournal {
	newName := strings.TrimSpace(kit.FromPtr(req.Name))
	if newName == "" || req.OldName == newName {
		return nil
	}

	newDirName := strings.TrimSpace(projects.SanitizeProjectName(newName))
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return nil
	}

	return &types.ProjectRenameJournal{
		ProjectID:  req.ProjectID,
		OldName:    req.OldName,
		NewName:    newName,
		OldPath:    filepath.Clean(req.OldPath),
		NewPath:    filepath.Clean(filepath.Join(req.ProjectsDirectory, newDirName)),
		OldDirName: mo.PointerToOption(req.OldDirName).ToPointer(),
		NewDirName: newDirName,
		Phase:      types.ProjectRenameJournalPhaseStarted,
		Volumes: func() []types.VolumeJournalVolume {
			if source, ok := req.Migration.(types.VolumeJournalSource); ok {
				return source.JournalVolumes()
			}
			return nil
		}(),
	}
}

func (s *Service) WriteJournal(ctx context.Context, journal *types.ProjectRenameJournal, phase string) error {
	if journal == nil {
		return nil
	}
	*journal = types.ProjectRenameJournal{ //exhaustruct:enforce
		ProjectID:  journal.ProjectID,
		OldName:    journal.OldName,
		NewName:    journal.NewName,
		OldPath:    journal.OldPath,
		NewPath:    journal.NewPath,
		OldDirName: journal.OldDirName,
		NewDirName: journal.NewDirName,
		Phase:      phase,
		Volumes:    journal.Volumes,
		UpdatedAt:  time.Now().UTC(),
	}

	payload, err := json.Marshal(journal)
	if err != nil {
		return fmt.Errorf("marshal project rename journal: %w", err)
	}

	if setErr := s.kv.Set(ctx, types.ProjectRenameJournalKeyPrefix+journal.ProjectID, string(payload)); setErr != nil {
		return fmt.Errorf("write project rename journal: %w", setErr)
	}
	return nil
}

func (s *Service) clearRollbackCleanup(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kv.Delete(ctx, types.ProjectRenameRollbackCleanupKeyPrefix+projectID)
}

func (s *Service) RecoverAll(ctx context.Context) error {
	entries, err := s.kv.ListByPrefix(ctx, types.ProjectRenameJournalKeyPrefix)
	if err != nil {
		return err
	}

	var recoverErr error
	for _, entry := range entries {
		var journal types.ProjectRenameJournal
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &journal); unmarshalErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("decode project rename journal %s: %w", entry.Key, unmarshalErr))
			continue
		}
		if strings.TrimSpace(journal.ProjectID) == "" {
			continue
		}
		name, path, found, stateErr := s.projectState(ctx, journal.ProjectID)
		if stateErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename journal %s: load project for rename recovery: %w", entry.Key, stateErr))
			continue
		}
		if journalErr := projects.RecoverRenameJournal(
			ctx,
			&journal,
			found && (name == journal.NewName || filepath.Clean(path) == filepath.Clean(journal.NewPath)),
			s.Operations(),
		); journalErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename journal %s: %w", entry.Key, journalErr))
		}
	}

	cleanups, err := s.kv.ListByPrefix(ctx, types.ProjectRenameRollbackCleanupKeyPrefix)
	if err != nil {
		return errors.Join(recoverErr, err)
	}
	for _, entry := range cleanups {
		var cleanup types.ProjectRenameRollbackCleanup
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &cleanup); unmarshalErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("decode project rename rollback cleanup %s: %w", entry.Key, unmarshalErr))
			continue
		}
		if cleanupErr := func() error {
			if strings.TrimSpace(cleanup.ProjectID) == "" {
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
			return projects.CleanupRenameRollbackTargets(ctx, &cleanup, s.Operations())
		}(); cleanupErr != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename rollback cleanup %s: %w", entry.Key, cleanupErr))
		}
	}
	return recoverErr
}

func (s *Service) RecoverProject(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return nil
	}

	raw, ok, err := s.kv.Get(ctx, types.ProjectRenameJournalKeyPrefix+projectID)
	if err != nil || !ok {
		return err
	}

	var journal types.ProjectRenameJournal
	if unmarshalErr := json.Unmarshal([]byte(raw), &journal); unmarshalErr != nil {
		return fmt.Errorf("decode project rename journal: %w", unmarshalErr)
	}
	if strings.TrimSpace(journal.ProjectID) == "" {
		return nil
	}
	name, path, found, stateErr := s.projectState(ctx, journal.ProjectID)
	if stateErr != nil {
		return fmt.Errorf("load project for rename recovery: %w", stateErr)
	}
	return projects.RecoverRenameJournal(ctx, &journal, found && (name == journal.NewName || filepath.Clean(path) == filepath.Clean(journal.NewPath)), s.Operations())
}

func (s *Service) RollbackFailedUpdate(
	ctx context.Context,
	projectID, projectsDirectory, originalPath, currentPath string,
	backup *projects.ProjectUpdateBackup,
	journalActive bool,
	err error,
) error {
	renameErr := func() error {
		switch {
		case currentPath == originalPath:
			return nil
		case filepath.Dir(originalPath) == filepath.Dir(currentPath):
			return acfs.Root{Path: filepath.Dir(originalPath)}.Rename(context.WithoutCancel(ctx), "/"+filepath.Base(currentPath), "/"+filepath.Base(originalPath))
		}
		return acfs.Host{}.Rename(context.WithoutCancel(ctx), currentPath, originalPath)
	}()
	if renameErr != nil {
		slog.WarnContext(ctx, "failed to rollback project directory rename", "from", currentPath, "to", originalPath, "error", renameErr)
	}
	if backup != nil {
		if restoreErr := projects.RestoreProjectDirectoryBackup(ctx, projectsDirectory, kit.Ternary(renameErr == nil, originalPath, currentPath), backup); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to restore project files after update failure: %w", restoreErr))
		}
	}
	if journalActive {
		if recoverErr := s.RecoverProject(ctx, projectID); recoverErr != nil {
			err = errors.Join(err, fmt.Errorf("project rename recovery failed: %w", recoverErr))
		}
	}
	return err
}

func (s *Service) Operations() types.ProjectRenameRecoveryOperations {
	return types.ProjectRenameRecoveryOperations{
		Docker: func(ctx context.Context, dockerRequired bool) (*client.Client, error) {
			if !dockerRequired {
				return nil, nil
			}
			dockerClient, err := s.dockerService.GetClient(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to Docker: %w", err)
			}
			return dockerClient, nil
		},
		InvalidateVolumeUsage: s.dockerService.VolumeUsage.Invalidate,
		WriteJournal:          s.WriteJournal,
		ClearJournal: func(ctx context.Context, projectID string) error {
			if strings.TrimSpace(projectID) == "" {
				return nil
			}
			return s.kv.Delete(ctx, types.ProjectRenameJournalKeyPrefix+projectID)
		},
		ClearRollbackCleanup: s.clearRollbackCleanup,
		WriteRollbackCleanup: func(ctx context.Context, journal *types.ProjectRenameJournal) error {
			if strings.TrimSpace(kit.FromPtr(journal).ProjectID) == "" || len(journal.Volumes) == 0 {
				return nil
			}
			payload, err := json.Marshal(types.ProjectRenameRollbackCleanup{
				ProjectID: journal.ProjectID,
				OldName:   journal.OldName,
				OldPath:   filepath.Clean(journal.OldPath),
				NewName:   journal.NewName,
				NewPath:   filepath.Clean(journal.NewPath),
				Volumes:   journal.Volumes,
				UpdatedAt: time.Now().UTC(),
			})
			if err != nil {
				return fmt.Errorf("marshal project rename rollback cleanup: %w", err)
			}
			if setErr := s.kv.Set(ctx, types.ProjectRenameRollbackCleanupKeyPrefix+journal.ProjectID, string(payload)); setErr != nil {
				return fmt.Errorf("write project rename rollback cleanup: %w", setErr)
			}
			return nil
		},
		RestoreState: s.restoreState,
	}
}

func PersistProjectServiceImages(ctx context.Context, projectPath, logical string, original, updated []byte) error {
	mode, err := checkProjectServiceImagesSource(ctx, projectPath, logical, original)
	if err != nil {
		return err
	}
	if writeErr := (acfs.Root{Path: projectPath}).Write(ctx, logical, updated, acfs.WriteOptions{Mode: mode}); writeErr != nil {
		return fmt.Errorf("persist Compose image changes: %w", writeErr)
	}
	return nil
}

func checkProjectServiceImagesSource(ctx context.Context, projectPath, logical string, original []byte) (os.FileMode, error) {
	entry, err := acfs.Root{Path: projectPath}.Stat(ctx, logical, acfstypes.StatOptions{})
	if err != nil {
		return 0, fmt.Errorf("inspect Compose source: %w", err)
	}
	if entry.IsSymlink {
		return 0, fmt.Errorf("tag updates refuse a symlinked Compose source: %s", filepath.Base(logical))
	}
	current, err := acfs.Root{Path: projectPath}.ReadFile(ctx, logical)
	if err != nil {
		return 0, fmt.Errorf("recheck Compose source: %w", err)
	}
	if !bytes.Equal(current, original) {
		return 0, errors.New("Compose source changed during the image update; check updates again") //nolint:staticcheck // Preserve the existing error message.
	}
	return os.FileMode(entry.UnixMode).Perm(), nil
}

func composeImageField(node *yaml.Node, key string) *yaml.Node {
	if kit.FromPtr(node).Kind != yaml.MappingNode {
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

func PrepareProjectServiceImages(source []byte, effective *composeTypes.Project, changes map[string]updatertypes.ServiceImageChange) (ImageEdits, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	if err := decoder.Decode(&document); err != nil {
		return ImageEdits{}, fmt.Errorf("parse Compose source: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ImageEdits{}, errors.New("tag updates require a single YAML document")
	}
	if len(document.Content) != 1 {
		return ImageEdits{}, errors.New("Compose source must contain a mapping") //nolint:staticcheck // Preserve the existing error message.
	}
	if err := validateImageUpdateSource(document.Content[0]); err != nil {
		return ImageEdits{}, err
	}
	if composeImageField(document.Content[0], "include") != nil {
		return ImageEdits{}, errors.New("tag updates do not rewrite Compose includes; update their source manually")
	}
	servicesNode := composeImageField(document.Content[0], "services")
	if kit.FromPtr(servicesNode).Kind != yaml.MappingNode {
		return ImageEdits{}, errors.New("Compose source has no services mapping") //nolint:staticcheck // Preserve the existing error message.
	}
	serviceNames := make([]string, 0, len(changes))
	env := make(map[string]string)
	envOwners := make(map[string]string)
	// Rewrite image scalars in place; re-encoding the node tree drops blank lines and operator formatting.
	edits := make([]projects.ScalarEdit, 0, len(changes))
	for _, name := range slices.Sorted(maps.Keys(changes)) {
		change := changes[name]
		service, ok := effective.Services[name]
		if !ok {
			return ImageEdits{}, fmt.Errorf("service %s is not active in the Compose project", name)
		}
		expected, target := refs.NormalizeImageUpdateRef(change.ExpectedRef), refs.NormalizeImageUpdateRef(change.TargetRef)
		if expected == "" || target == "" || refs.IsImageIDLikeReference(change.ExpectedRef) || refs.IsImageIDLikeReference(change.TargetRef) {
			return ImageEdits{}, fmt.Errorf("service %s requires mutable image references", name)
		}
		if refs.NormalizeImageUpdateRef(service.Image) != expected && refs.NormalizeImageUpdateRef(service.Image) != target {
			return ImageEdits{}, fmt.Errorf("service %s image changed since update check: expected %s, found %s", name, change.ExpectedRef, service.Image)
		}
		serviceNode := composeImageField(servicesNode, name)
		if composeImageField(serviceNode, "extends") != nil {
			return ImageEdits{}, fmt.Errorf("tag updates do not rewrite extended service %s; update its source manually", name)
		}
		imageNode := composeImageField(serviceNode, "image")
		if kit.FromPtr(imageNode).Kind != yaml.ScalarNode || imageNode.Tag != "!!str" {
			return ImageEdits{}, fmt.Errorf("service %s requires an explicit image scalar in the authoritative Compose file", name)
		}
		rendered, err := substituteImage(imageNode.Value, effective.Environment, nil)
		if err != nil || (refs.NormalizeImageUpdateRef(rendered) != expected && refs.NormalizeImageUpdateRef(rendered) != target) {
			return ImageEdits{}, fmt.Errorf("service %s source image changed since Compose was loaded", name)
		}
		scalar := refs.PreserveConfiguredRef(service.Image, change.TargetRef)
		if len(template.ExtractVariables(map[string]any{"image": imageNode.Value}, nil)) > 0 {
			if variable, value, found := imageTagVariable(imageNode.Value, effective.Environment, scalar); found {
				if owner, taken := envOwners[variable]; taken && env[variable] != value {
					return ImageEdits{}, fmt.Errorf(
						"services %s and %s both take their image tag from %s but would set it to %s and %s; set %s in the project .env manually",
						owner, name, variable, env[variable], value, variable,
					)
				}
				env[variable], envOwners[variable] = value, name
				serviceNames = append(serviceNames, name)
				continue
			}
			retagged, retagErr := retagImageSource(imageNode.Value, service.Image, scalar, effective.Environment)
			if retagErr != nil {
				return ImageEdits{}, fmt.Errorf("cannot update service %s to %s: %w", name, scalar, retagErr)
			}
			scalar = retagged
		}
		edit, err := projects.NewScalarEdit(source, imageNode, scalar)
		if err != nil {
			return ImageEdits{}, fmt.Errorf("service %s image: %w", name, err)
		}
		edits = append(edits, edit)
		serviceNames = append(serviceNames, name)
	}
	return ImageEdits{
		Compose:   projects.ApplyScalarEdits(source, edits),
		Env:       env,
		Followers: imageFollowers(servicesNode, effective, changes, env),
		Services:  serviceNames,
	}, nil
}

func imageFollowers(servicesNode *yaml.Node, effective *composeTypes.Project, changes map[string]updatertypes.ServiceImageChange, env map[string]string) []string {
	var followers []string
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		name := servicesNode.Content[i].Value
		imageNode := composeImageField(servicesNode.Content[i+1], "image")
		if _, changed := changes[name]; changed || kit.FromPtr(imageNode).Kind != yaml.ScalarNode {
			continue
		}
		for variable := range env {
			if marked, err := substituteImage(imageNode.Value, effective.Environment, map[string]string{variable: imageVariableMarker}); err == nil && strings.Contains(marked, imageVariableMarker) {
				followers = append(followers, name)
				break
			}
		}
	}
	return followers
}

func substituteImage(raw string, env composeTypes.Mapping, overrides map[string]string) (string, error) {
	return template.SubstituteWithOptions(raw, func(name string) (string, bool) {
		if value, ok := overrides[name]; ok {
			return value, true
		}
		value, ok := env[name]
		return value, ok
	}, template.WithoutLogging)
}

func imageTagVariable(raw string, env composeTypes.Mapping, target string) (variable, value string, found bool) {
	parsed, err := refs.NormalizeReference(target)
	if err != nil {
		return "", "", false
	}
	matches := 0
	for name := range template.ExtractVariables(map[string]any{"image": raw}, nil) {
		marked, markErr := substituteImage(raw, env, map[string]string{name: imageVariableMarker})
		prefix, suffix, ok := strings.Cut(marked, imageVariableMarker)
		if markErr != nil || !ok || strings.Contains(suffix, imageVariableMarker) || len(suffix) >= len(parsed.Tag) ||
			len(target) <= len(prefix)+len(suffix) || !strings.HasPrefix(target, prefix) || !strings.HasSuffix(target, suffix) {
			continue
		}
		candidate := target[len(prefix) : len(target)-len(suffix)]
		if rendered, renderErr := substituteImage(raw, env, map[string]string{name: candidate}); renderErr != nil || rendered != target {
			continue
		}
		variable, value = name, candidate
		matches++
	}
	return variable, value, matches == 1
}

func retagImageSource(raw, current, target string, env composeTypes.Mapping) (string, error) {
	currentRef, currentErr := refs.NormalizeReference(current)
	targetRef, targetErr := refs.NormalizeReference(target)
	if currentErr == nil && targetErr == nil {
		if repository, ok := strings.CutSuffix(raw, ":"+currentRef.Tag); ok {
			scalar := repository + ":" + targetRef.Tag
			if rendered, err := substituteImage(scalar, env, nil); err == nil && rendered == target {
				return scalar, nil
			}
		}
	}
	return "", fmt.Errorf("the new tag does not fit the variables in its image %q; set the tag in the project .env or Compose file manually", raw)
}

func EnsureProjectEnvReadable(ctx context.Context, projectsDirectory, projectPath string) error {
	cfgErr := projects.CheckProjectEnvAccess(ctx, projectsDirectory, projectPath)
	if !kit.FromPtr(cfgErr).BlocksOperations {
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

func (s *Service) ImageChanges(ctx context.Context, effective *composeTypes.Project, servicesToUpdate []string, gitOpsManaged bool) (map[string]updatertypes.ServiceImageChange, error) {
	policy := updater.DefaultLabelPolicy()
	selected := projects.NormalizeBuildSelections(servicesToUpdate)
	changes := make(map[string]updatertypes.ServiceImageChange)
	for _, key := range effective.ServiceNames() {
		service := effective.Services[key]
		if !projects.ServiceSelected(selected, key) || imageref.IsUpdateCheckDisabled(service.Labels) {
			continue
		}
		configuredPolicy := policy.TagPolicy(service.Labels)
		if service.Build != nil && (configuredPolicy.Strategy == "" || configuredPolicy.Strategy == "auto") && configuredPolicy.Constraint == "" && configuredPolicy.TagPattern == "" {
			continue
		}
		if configuredPolicy.Strategy == "tag" && (refs.IsDigestPinnedReference(service.Image) || refs.IsImageIDLikeReference(service.Image)) {
			return nil, fmt.Errorf("service %s has an immutable image reference", service.Name)
		}
		tagPolicy, err := tagpolicy.Resolve(service.Image, configuredPolicy)
		if err != nil {
			return nil, fmt.Errorf("resolve service %s update policy: %w", service.Name, err)
		}
		if tagPolicy.Strategy == "digest" {
			continue
		}
		switch {
		case service.Build != nil:
			return nil, fmt.Errorf("tag discovery is unsupported for locally built service %s", service.Name)
		case gitOpsManaged:
			return nil, errors.New("tag updates cannot edit a GitOps-managed project; update image tags in the source repository")
		case refs.IsDigestPinnedReference(service.Image) || refs.IsImageIDLikeReference(service.Image):
			return nil, fmt.Errorf("service %s has an immutable image reference", service.Name)
		}
		parsed, tags, err := s.imageTags(ctx, service.Name, service.Image)
		if err != nil {
			return nil, err
		}
		selected, err := tagpolicy.Select(parsed.Tag, tags, tagPolicy)
		if err != nil {
			return nil, fmt.Errorf("select service %s image tag: %w", service.Name, err)
		}
		if selected != parsed.Tag {
			changes[key] = updatertypes.ServiceImageChange{
				ExpectedRef: service.Image,
				TargetRef:   parsed.RegistryHost + "/" + parsed.Repository + ":" + selected,
			}
		}
	}
	return changes, nil
}

func (s *Service) ApplyImageChanges(ctx context.Context, projectPath string, effective *composeTypes.Project, changes map[string]updatertypes.ServiceImageChange) ([]string, error) {
	if len(effective.ComposeFiles) != 1 {
		return nil, errors.New("tag updates require one authoritative Compose file; multi-file selections and overrides must be updated manually")
	}
	logical, err := acfs.Root{Path: projectPath}.LogicalPath(effective.ComposeFiles[0])
	if err != nil {
		return nil, fmt.Errorf("tag update Compose file must be inside the project directory: %w", err)
	}
	original, err := acfs.Root{Path: projectPath}.ReadFile(ctx, logical)
	if err != nil {
		return nil, fmt.Errorf("read Compose source for tag update: %w", err)
	}
	edits, err := PrepareProjectServiceImages(original, effective, changes)
	if err != nil {
		return nil, err
	}
	services := edits.Services
	if len(edits.Env) > 0 {
		updated, content, resolveErr := s.resolveEnv(ctx, projectPath, effective, edits.Env)
		if resolveErr != nil {
			return nil, resolveErr
		}
		followers, followerErr := s.followerImages(ctx, effective, updated, changes, edits)
		if followerErr != nil {
			return nil, followerErr
		}
		services = slices.Sorted(slices.Values(slices.Concat(services, followers)))
		if _, sourceErr := checkProjectServiceImagesSource(ctx, projectPath, logical, original); sourceErr != nil {
			return nil, sourceErr
		}
		if persistErr := s.persistEnv(ctx, projectPath, content); persistErr != nil {
			return nil, persistErr
		}
	}
	if persistErr := PersistProjectServiceImages(ctx, projectPath, logical, original, edits.Compose); persistErr != nil {
		return nil, persistErr
	}
	return services, nil
}

func (s *Service) followerImages(ctx context.Context, effective, updated *composeTypes.Project, changes map[string]updatertypes.ServiceImageChange, edits ImageEdits) ([]string, error) {
	assignments := make([]string, 0, len(edits.Env))
	for _, name := range slices.Sorted(maps.Keys(edits.Env)) {
		assignments = append(assignments, name+"="+edits.Env[name])
	}
	edit := strings.Join(assignments, ", ")
	checked := make([]string, 0, len(changes))
	for _, change := range changes {
		checked = append(checked, refs.NormalizeImageUpdateRef(change.TargetRef))
	}
	policy := updater.DefaultLabelPolicy()
	var followers []string
	for _, name := range slices.Sorted(maps.Keys(updated.Services)) {
		service := updated.Services[name]
		previous, active := effective.Services[name]
		if !active {
			previous = effective.DisabledServices[name]
		}
		if _, changed := changes[name]; changed || (service.Image == previous.Image && !slices.Contains(edits.Followers, name)) {
			continue
		}
		followers = append(followers, name)
		if service.Build != nil || refs.IsDigestPinnedReference(service.Image) {
			continue
		}
		parsed, err := refs.NormalizeReference(service.Image)
		if err != nil {
			return nil, fmt.Errorf("cannot set %s: service %s also uses it, and its image %s is not valid: %w", edit, name, service.Image, err)
		}
		if limits := policy.TagPolicy(service.Labels); limits.Constraint != "" || limits.TagPattern != "" {
			current, currentErr := refs.NormalizeReference(previous.Image)
			if currentErr != nil {
				return nil, fmt.Errorf("parse service %s image: %w", name, currentErr)
			}
			if allowed, selectErr := tagpolicy.Select(current.Tag, []string{parsed.Tag}, limits); selectErr != nil || allowed != parsed.Tag {
				return nil, fmt.Errorf(
					"cannot set %s: service %s also uses it, and its update policy does not allow %s; update %s manually",
					edit, name, service.Image, name,
				)
			}
		}
		if slices.Contains(checked, refs.NormalizeImageUpdateRef(service.Image)) {
			continue
		}
		_, tags, err := s.imageTags(ctx, name, service.Image)
		if err != nil {
			return nil, fmt.Errorf(
				"cannot set %s: service %s also uses it, and Arcane could not check that %s is published: %w",
				edit, name, service.Image, err,
			)
		}
		if !slices.Contains(tags, parsed.Tag) {
			return nil, fmt.Errorf(
				"cannot set %s: service %s also uses it, and %s is not published yet; update once it is available",
				edit, name, service.Image,
			)
		}
		checked = append(checked, refs.NormalizeImageUpdateRef(service.Image))
	}
	return followers, nil
}

func (s *Service) imageTags(ctx context.Context, name, image string) (*refs.Reference, []string, error) {
	parsed, err := refs.NormalizeReference(image)
	if err != nil {
		return nil, nil, fmt.Errorf("parse service %s image: %w", name, err)
	}
	credentials, err := s.credentials(ctx)
	if err != nil {
		return nil, nil, err
	}
	tags, err := s.registryService.ListImageTags(ctx, image, credentials)
	if err != nil {
		return nil, nil, fmt.Errorf("list service %s image tags: %w", name, err)
	}
	return parsed, tags, nil
}
