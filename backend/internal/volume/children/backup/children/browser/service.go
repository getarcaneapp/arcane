package browser

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/client"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/backupbrowser"
)

// Dependencies are the backup storage and repository operations the browser reads through.
type Dependencies struct {
	Docker          *docker.DockerClientService
	Engine          *backup.Engine
	Run             func(ctx context.Context, backupID string) (*types.VolumeBackup, error)
	Repository      func(ctx context.Context, dockerClient *client.Client, entry *types.VolumeBackup) (backup.Repository, string, error)
	Password        func(ctx context.Context, dockerClient *client.Client, repositories ...backup.Repository) (string, error)
	SanitizePath    func(input string) (string, error)
	ArchiveFilename func(backupID string) (string, error)
	TempContainer   func(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (string, func(), error)
	Exec            func(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error)
}

// Service lists and browses the files stored in volume backups.
type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) ListBackupFiles(ctx context.Context, backupID string) ([]string, error) {
	entry, err := s.deps.Run(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if entry.Format == types.VolumeBackupFormatArchive {
		return s.listArchiveBackupFiles(ctx, backupID)
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	repository, snapshotID, err := s.deps.Repository(ctx, dockerClient, entry)
	if err != nil {
		return nil, err
	}
	password, err := s.deps.Password(ctx, dockerClient, repository)
	if err != nil {
		return nil, err
	}
	return s.deps.Engine.ListSnapshotFiles(ctx, dockerClient, repository, password, snapshotID, "", true)
}

// BrowseBackupFiles returns one lazy-loaded page from a volume backup tree.
func (s *Service) BrowseBackupFiles(ctx context.Context, backupID, requestedPath string, params pagination.QueryParams) ([]types.BackupFileEntry, pagination.Response, error) {
	listPath, recursive, err := backupbrowser.ListScope(requestedPath, params)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("%w: %w", common.ErrInvalidBackupSelection, err)
	}
	entry, err := s.deps.Run(ctx, backupID)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	entries, err := s.backupFileEntries(ctx, entry, listPath, recursive)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	items, page := backupbrowser.Browse(entries, params)
	return items, page, nil
}

func (s *Service) backupFileEntries(ctx context.Context, entry *types.VolumeBackup, browsePath string, recursive bool) ([]types.BackupFileEntry, error) {
	if entry.Format == types.VolumeBackupFormatArchive {
		paths, err := s.ArchivePaths(ctx, entry.ID)
		if err != nil {
			return nil, err
		}
		return backupbrowser.BuildEntries(paths, browsePath, recursive), nil
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	repository, snapshotID, err := s.deps.Repository(ctx, dockerClient, entry)
	if err != nil {
		return nil, err
	}
	password, err := s.deps.Password(ctx, dockerClient, repository)
	if err != nil {
		return nil, err
	}
	listed, err := s.deps.Engine.ListSnapshotFiles(ctx, dockerClient, repository, password, snapshotID, browsePath+"/", recursive)
	if err != nil {
		return nil, err
	}
	return backupbrowser.BuildEntries(listed, browsePath, recursive), nil
}

func (s *Service) BackupHasPath(ctx context.Context, backupID, filePath string) (bool, error) {
	cleaned, err := s.deps.SanitizePath(filePath)
	if err != nil {
		return false, err
	}
	files, err := s.ListBackupFiles(ctx, backupID)
	if err != nil {
		return false, err
	}
	for _, candidate := range files {
		if strings.TrimPrefix(candidate, "/") == cleaned {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) listArchiveBackupFiles(ctx context.Context, backupID string) ([]string, error) {
	paths, err := s.ArchivePaths(ctx, backupID)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(paths))
	for _, candidate := range paths {
		if !strings.HasSuffix(candidate, "/") {
			files = append(files, candidate)
		}
	}
	return files, nil
}

// ArchivePaths lists every member of a legacy tar.gz volume backup.
func (s *Service) ArchivePaths(ctx context.Context, backupID string) ([]string, error) {
	filename, err := s.deps.ArchiveFilename(backupID)
	if err != nil {
		return nil, err
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	containerID, cleanup, err := s.deps.TempContainer(ctx, dockerClient, "/volume", true)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	stdout, _, err := s.deps.Exec(ctx, containerID, "", []string{"tar", "-tzf", path.Join("/volume", filename)})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if clean := strings.TrimPrefix(strings.TrimSpace(line), "./"); clean != "" {
			files = append(files, clean)
		}
	}
	return kit.Unique(files), nil
}
