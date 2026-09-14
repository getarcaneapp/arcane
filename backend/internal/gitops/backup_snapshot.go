package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"emperror.dev/errors"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
)

const backupSnapshotAttemptsInternal = 3

var (
	errBackupUnreadableInternal = errors.Sentinel("backup selection is unreadable")
	errBackupLimitsInternal     = errors.Sentinel("backup selection exceeds sync limits")
	errBackupSnapshotInternal   = errors.Sentinel("backup selection is invalid")
)

// backupSnapshotInternal is the set of project files a backup run commits.
type backupSnapshotInternal struct {
	files        []git.CommitFile
	hashes       map[string]string
	composeFiles []string
}

// remoteBackupStateInternal describes the backup directory as it exists on the branch.
type remoteBackupStateInternal struct {
	manifest *gitops.BackupManifest
	hashes   map[string]string
	occupied bool
}

// backupAnalysisInternal is the decision for one backup run.
type backupAnalysisInternal struct {
	state     string
	changes   []gitops.BackupFileChange
	conflicts []gitops.BackupFileChange
	remove    []string
}

const (
	backupPreviewClean     = "clean"
	backupPreviewChanges   = "changes"
	backupPreviewConflict  = "conflict"
	backupPreviewOccupied  = "destination_occupied"
	backupChangeAdded      = "added"
	backupChangeModified   = "modified"
	backupChangeRemoved    = "removed"
	backupManifestVersion  = 1
	backupSnapshotMaxRetry = backupSnapshotAttemptsInternal
)

func hashBackupContentInternal(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// isBackupEnvFileInternal reports whether a base name looks like an environment
// file; these are excluded from directory expansion and only backed up when
// listed explicitly.
func isBackupEnvFileInternal(name string) bool {
	return name == projects.EffectiveEnvFileName || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env")
}

// isBackupReservedFileInternal reports whether a base name is Arcane bookkeeping
// that never belongs in a backup.
func isBackupReservedFileInternal(name string) bool {
	return name == projects.GitSourceEnvFileName || name == projects.GlobalEnvFileName || name == gitops.BackupManifestFileName || name == ".git"
}

func containsGitSegmentInternal(relative string) bool {
	for segment := range strings.SplitSeq(relative, "/") {
		if segment == ".git" {
			return true
		}
	}
	return false
}

func normalizeBackupDirectoryInternal(raw string) (string, error) {
	normalized, err := utils.NormalizeRelativePath(raw)
	if err != nil {
		return "", err
	}
	if containsGitSegmentInternal(normalized) {
		return "", errors.New("backup directory must not contain a .git segment")
	}
	return normalized, nil
}

func normalizeBackupPathsInternal(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	normalized := make([]string, 0, len(raw))
	for _, entry := range raw {
		cleaned, err := utils.NormalizeRelativePath(entry)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid backup path %q", entry)
		}
		if isBackupReservedFileInternal(path.Base(cleaned)) || containsGitSegmentInternal(cleaned) {
			return nil, errors.Errorf("backup path %q is reserved", entry)
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	sort.Strings(normalized)
	return normalized, nil
}

// backupSelectionCoversInternal reports whether the selection includes file
// directly or through one of its parent directories.
func backupSelectionCoversInternal(paths []string, file string) bool {
	for _, selected := range paths {
		if selected == file || strings.HasPrefix(file, selected+"/") {
			return true
		}
	}
	return false
}

func backupDirectoriesOverlapInternal(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// backupComposeFilesInternal resolves the project's primary compose file and any
// sibling overrides as project-relative paths.
func (s *GitOpsSyncService) backupComposeFilesInternal(ctx context.Context, project *projectpkg.Project) (string, []string, error) {
	composePath, err := s.projectService.ResolveProjectComposeFile(ctx, project)
	if err != nil {
		return "", nil, err
	}
	relative, err := filepath.Rel(project.Path, composePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, errors.Errorf("compose file %s is outside the project directory", composePath)
	}
	primary := filepath.ToSlash(relative)
	composeFiles := []string{primary}
	composeDir := path.Dir(primary)
	for _, candidate := range projects.ComposeOverrideFileCandidates() {
		candidatePath := path.Join(composeDir, candidate)
		if exists, existsErr := acfs.Exists(ctx, project.Path, "/"+candidatePath); existsErr == nil && exists {
			composeFiles = append(composeFiles, candidatePath)
		}
	}
	return primary, composeFiles, nil
}

// buildBackupSnapshotInternal reads the selected project files, verifying that
// none changed while being read.
func (s *GitOpsSyncService) buildBackupSnapshotInternal(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project) (*backupSnapshotInternal, error) {
	paths := []string(sync.BackupPaths)
	if len(paths) == 0 {
		return nil, errors.WrapIf(errBackupSnapshotInternal, "no files are selected for backup")
	}
	primary, composeFiles, err := s.backupComposeFilesInternal(ctx, project)
	if err != nil {
		return nil, errors.WrapIf(errBackupSnapshotInternal, err.Error())
	}
	maxFiles, maxTotalSize, _ := s.getEffectiveSyncLimits(ctx, sync)

	var lastErr error
	for range backupSnapshotMaxRetry {
		snapshot, err := collectBackupFilesInternal(ctx, project.Path, paths, maxFiles, maxTotalSize)
		if err != nil {
			return nil, err
		}
		if _, ok := snapshot.hashes[primary]; !ok {
			return nil, errors.WrapIff(errBackupSnapshotInternal, "compose file %s is not included in the backup selection", primary)
		}
		stable, err := backupSnapshotStableInternal(ctx, project.Path, snapshot)
		if err != nil {
			return nil, err
		}
		if stable {
			for _, composeFile := range composeFiles {
				if _, ok := snapshot.hashes[composeFile]; ok {
					snapshot.composeFiles = append(snapshot.composeFiles, composeFile)
				}
			}
			return snapshot, nil
		}
		lastErr = errors.WrapIf(errBackupSnapshotInternal, "project files changed while the backup snapshot was being taken")
	}
	return nil, lastErr
}

// backupCollectorInternal accumulates the selected project files under the sync limits.
type backupCollectorInternal struct {
	ctx          context.Context
	projectPath  string
	maxFiles     int
	maxTotalSize int64
	totalSize    int64
	snapshot     *backupSnapshotInternal
}

func collectBackupFilesInternal(ctx context.Context, projectPath string, paths []string, maxFiles int, maxTotalSize int64) (*backupSnapshotInternal, error) {
	collector := &backupCollectorInternal{
		ctx:          ctx,
		projectPath:  projectPath,
		maxFiles:     maxFiles,
		maxTotalSize: maxTotalSize,
		snapshot:     &backupSnapshotInternal{hashes: make(map[string]string)},
	}
	for _, selected := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := collector.addPathInternal(selected); err != nil {
			return nil, err
		}
	}
	if len(collector.snapshot.files) == 0 {
		return nil, errors.WrapIf(errBackupSnapshotInternal, "the backup selection contains no files")
	}
	sort.Slice(collector.snapshot.files, func(i, j int) bool { return collector.snapshot.files[i].Path < collector.snapshot.files[j].Path })
	return collector.snapshot, nil
}

func (c *backupCollectorInternal) addPathInternal(selected string) error {
	logical := "/" + selected
	entry, err := acfs.Stat(c.ctx, c.projectPath, logical, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.WrapIff(errBackupUnreadableInternal, "selected path %s does not exist", selected)
		}
		return errors.WrapIff(errBackupUnreadableInternal, "cannot inspect %s: %v", selected, err)
	}
	if entry.IsSymlink {
		return errors.WrapIff(errBackupSnapshotInternal, "%s is a symbolic link and cannot be backed up", selected)
	}
	if !entry.IsDirectory {
		return c.addFileInternal(entry)
	}
	walkErr := acfs.Walk(c.ctx, c.projectPath, logical, c.visitDirectoryEntryInternal)
	if walkErr == nil {
		return nil
	}
	if errors.Is(walkErr, errBackupUnreadableInternal) || errors.Is(walkErr, errBackupLimitsInternal) || errors.Is(walkErr, errBackupSnapshotInternal) {
		return walkErr
	}
	return errors.WrapIff(errBackupUnreadableInternal, "cannot walk %s: %v", selected, walkErr)
}

func (c *backupCollectorInternal) visitDirectoryEntryInternal(child acfstypes.Entry) error {
	if child.IsDirectory {
		if child.Name == ".git" || projects.IsInternalScratchDirName(child.Name) {
			return fs.SkipDir
		}
		return nil
	}
	if child.IsSymlink || isBackupReservedFileInternal(child.Name) || isBackupEnvFileInternal(child.Name) {
		return nil
	}
	return c.addFileInternal(child)
}

func (c *backupCollectorInternal) addFileInternal(entry acfstypes.Entry) error {
	relative := strings.TrimPrefix(entry.Path, "/")
	if _, seen := c.snapshot.hashes[relative]; seen {
		return nil
	}
	if c.maxFiles > 0 && len(c.snapshot.files) >= c.maxFiles {
		return errors.WrapIff(errBackupLimitsInternal, "file count limit exceeded (max %d files)", c.maxFiles)
	}
	content, err := acfs.ReadFile(c.ctx, c.projectPath, entry.Path)
	if err != nil {
		return errors.WrapIff(errBackupUnreadableInternal, "cannot read %s: %v", relative, err)
	}
	if isBinaryContentForBackupInternal(content) {
		return errors.WrapIff(errBackupSnapshotInternal, "%s is not a text file; only text configuration files can be backed up", relative)
	}
	c.totalSize += int64(len(content))
	if c.maxTotalSize > 0 && c.totalSize > c.maxTotalSize {
		return errors.WrapIff(errBackupLimitsInternal, "total size limit exceeded (max %d bytes)", c.maxTotalSize)
	}
	c.snapshot.files = append(c.snapshot.files, git.CommitFile{
		Path:       relative,
		Content:    content,
		Executable: os.FileMode(entry.UnixMode)&0o111 != 0,
	})
	c.snapshot.hashes[relative] = hashBackupContentInternal(content)
	return nil
}

func backupSnapshotStableInternal(ctx context.Context, projectPath string, snapshot *backupSnapshotInternal) (bool, error) {
	for _, file := range snapshot.files {
		content, err := acfs.ReadFile(ctx, projectPath, "/"+file.Path)
		if err != nil {
			return false, errors.WrapIff(errBackupUnreadableInternal, "cannot re-read %s: %v", file.Path, err)
		}
		if hashBackupContentInternal(content) != snapshot.hashes[file.Path] {
			return false, nil
		}
	}
	return true, nil
}

func isBinaryContentForBackupInternal(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	return slices.Contains(content[:min(len(content), 8000)], 0)
}

func buildBackupManifestInternal(sync *projectpkg.GitOpsSync, projectName string, snapshot *backupSnapshotInternal, now time.Time) ([]byte, error) {
	manifest := gitops.BackupManifest{
		Version:       backupManifestVersion,
		SyncID:        sync.ID,
		ProjectName:   projectName,
		EnvironmentID: sync.EnvironmentID,
		GeneratedAt:   now.UTC(),
		ComposeFiles:  snapshot.composeFiles,
		Files:         snapshot.hashes,
	}
	if manifest.ComposeFiles == nil {
		manifest.ComposeFiles = []string{}
	}
	data, err := json.Marshal(manifest, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.WrapIf(err, "failed to encode backup manifest")
	}
	return append(data, '\n'), nil
}

// readRemoteBackupStateInternal inspects the backup directory in a checkout.
func readRemoteBackupStateInternal(ctx context.Context, repoPath, directory string) (*remoteBackupStateInternal, error) {
	state := &remoteBackupStateInternal{hashes: make(map[string]string)}
	manifestPath := "/" + path.Join(directory, gitops.BackupManifestFileName)
	data, err := acfs.ReadFile(ctx, repoPath, manifestPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, errors.WrapIf(err, "failed to read remote backup manifest")
		}
		entries, listErr := acfs.List(ctx, repoPath, "/"+directory)
		if listErr != nil {
			if errors.Is(listErr, fs.ErrNotExist) {
				return state, nil
			}
			return nil, errors.WrapIf(listErr, "failed to inspect remote backup directory")
		}
		state.occupied = len(entries) > 0
		return state, nil
	}

	var manifest gitops.BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, errors.WrapIf(err, "remote backup manifest is not valid")
	}
	state.manifest = &manifest
	for file := range manifest.Files {
		relative, err := utils.NormalizeRelativePath(file)
		if err != nil {
			continue
		}
		content, readErr := acfs.ReadFile(ctx, repoPath, "/"+path.Join(directory, relative))
		if readErr != nil {
			if errors.Is(readErr, fs.ErrNotExist) {
				continue
			}
			return nil, errors.WrapIff(readErr, "failed to read remote backup file %s", relative)
		}
		state.hashes[relative] = hashBackupContentInternal(content)
	}
	return state, nil
}

func diffBackupHashesInternal(local, remote map[string]string) []gitops.BackupFileChange {
	var changes []gitops.BackupFileChange
	for file, hash := range local {
		remoteHash, ok := remote[file]
		switch {
		case !ok:
			changes = append(changes, gitops.BackupFileChange{Path: file, Change: backupChangeAdded})
		case remoteHash != hash:
			changes = append(changes, gitops.BackupFileChange{Path: file, Change: backupChangeModified})
		}
	}
	for file := range remote {
		if _, ok := local[file]; !ok {
			changes = append(changes, gitops.BackupFileChange{Path: file, Change: backupChangeRemoved})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if changes == nil {
		changes = []gitops.BackupFileChange{}
	}
	return changes
}

// analyzeBackupInternal decides whether the run is a no-op, a commit, or needs
// attention. baseline is the last snapshot Arcane successfully pushed; adopt
// skips the conflict and occupancy checks.
func analyzeBackupInternal(snapshot *backupSnapshotInternal, remote *remoteBackupStateInternal, baseline map[string]string, adopt bool) backupAnalysisInternal {
	analysis := backupAnalysisInternal{
		changes:   diffBackupHashesInternal(snapshot.hashes, remote.hashes),
		conflicts: []gitops.BackupFileChange{},
	}
	for file := range remote.hashes {
		if _, ok := snapshot.hashes[file]; !ok {
			analysis.remove = append(analysis.remove, file)
		}
	}
	sort.Strings(analysis.remove)

	if len(analysis.changes) == 0 {
		analysis.state = backupPreviewClean
		return analysis
	}
	if adopt {
		analysis.state = backupPreviewChanges
		return analysis
	}
	if remote.manifest == nil {
		if remote.occupied {
			analysis.state = backupPreviewOccupied
		} else {
			analysis.state = backupPreviewChanges
		}
		return analysis
	}
	if baseline == nil {
		analysis.conflicts = diffBackupHashesInternal(remote.hashes, map[string]string{})
		analysis.state = backupPreviewConflict
		return analysis
	}
	analysis.conflicts = diffBackupHashesInternal(remote.hashes, baseline)
	if len(analysis.conflicts) > 0 {
		analysis.state = backupPreviewConflict
		return analysis
	}
	analysis.state = backupPreviewChanges
	return analysis
}

func parseBackupSnapshotInternal(raw *string) map[string]string {
	if raw == nil || *raw == "" {
		return nil
	}
	var hashes map[string]string
	if err := json.Unmarshal([]byte(*raw), &hashes); err != nil {
		return nil
	}
	return hashes
}

func marshalBackupSnapshotInternal(hashes map[string]string) *string {
	data, err := json.Marshal(hashes, json.Deterministic(true))
	if err != nil {
		return nil
	}
	return new(string(data))
}
