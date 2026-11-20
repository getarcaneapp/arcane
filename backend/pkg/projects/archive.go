package projects

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/docker/go-units"
	"github.com/shirou/gopsutil/v4/disk"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

type (
	ProjectBundleWriter struct {
		gzip *gzip.Writer
		tar  *tar.Writer
	}

	ProjectBundleVisitor func(name string, size int64, body io.Reader) error
)

const (
	ProjectBundleManifest = "arcane-project.json"
	ProjectBundleProject  = "project.tar.gz"

	maxArchiveEntries       = 200_000
	unpackReserveBytes      = 1 << 30
	maxProjectManifestBytes = 1 << 20
	maxVolumeLabels         = 256
	maxVolumeLabelBytes     = 64 << 10
	projectBundleVolumes    = "volumes/"
)

var (
	composeVolumeKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	dockerVolumeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)
)

func PackProjectDirectory(ctx context.Context, projectPath string, w io.Writer) error {
	resolvedRoot, rootErr := filepath.EvalSymlinks(projectPath)
	if rootErr != nil {
		return fmt.Errorf("resolve project directory: %w", rootErr)
	}
	root := acfs.Root{Path: projectPath}
	gzipWriter := gzip.NewWriter(w)
	tarWriter := tar.NewWriter(gzipWriter)
	visit := func(entry acfstypes.Entry) error {
		relativePath := strings.TrimPrefix(entry.Path, "/")
		mode := os.FileMode(entry.UnixMode)
		switch {
		case entry.IsSymlink:
			resolvedPath, resolveErr := filepath.EvalSymlinks(filepath.Join(projectPath, relativePath))
			if resolveErr == nil && IsSafeSubdirectory(resolvedRoot, resolvedPath) {
				linkTarget, err := root.Readlink(ctx, entry.Path)
				if err == nil && filepath.IsAbs(linkTarget) {
					linkTarget, err = filepath.Rel(filepath.Dir(filepath.Join(resolvedRoot, relativePath)), resolvedPath)
				}
				if err != nil {
					return err
				}
				return tarWriter.WriteHeader(&tar.Header{
					Typeflag: tar.TypeSymlink,
					Name:     relativePath,
					Linkname: linkTarget,
					Mode:     int64(mode.Perm()),
					ModTime:  entry.ModTime,
				})
			}
			if relativePath != EffectiveEnvFileName {
				return nil
			}
			envPath, perm, _, err := resolveEnvFileWriteTarget(ctx, filepath.Join(projectPath, relativePath))
			if err != nil {
				return err
			}
			content, err := acfs.Host{}.ReadFile(ctx, envPath)
			if err != nil {
				return err
			}
			if headerErr := tarWriter.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg,
				Name:     relativePath,
				Mode:     int64(perm),
				Size:     int64(len(content)),
				ModTime:  entry.ModTime,
			}); headerErr != nil {
				return headerErr
			}
			_, err = tarWriter.Write(content)
			return err
		case entry.IsDirectory && IsInternalScratchDirName(entry.Name):
			return fs.SkipDir
		case entry.IsDirectory:
			return tarWriter.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     relativePath + "/",
				Mode:     int64(mode.Perm()),
				ModTime:  entry.ModTime,
			})
		case relativePath == GitSourceEnvFileName || !mode.IsRegular():
			return nil
		}
		reader, _, err := root.OpenRead(ctx, entry.Path, entry.Size)
		if err != nil {
			return err
		}
		defer func() {
			// Opened only for reading; closing it can't lose data, and read errors are returned as they happen.
			_ = reader.Close()
		}()
		if headerErr := tarWriter.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     relativePath,
			Mode:     int64(mode.Perm()),
			Size:     entry.Size,
			ModTime:  entry.ModTime,
		}); headerErr != nil {
			return headerErr
		}
		_, err = io.CopyN(tarWriter, reader, entry.Size)
		return err
	}
	if _, walkErr := root.Walk(ctx, "/", acfstypes.WalkOptions{}, visit); walkErr != nil {
		return fmt.Errorf("pack project directory: %w", walkErr)
	}
	if closeErr := tarWriter.Close(); closeErr != nil {
		return fmt.Errorf("finish project archive: %w", closeErr)
	}
	if closeErr := gzipWriter.Close(); closeErr != nil {
		return fmt.Errorf("finish project archive compression: %w", closeErr)
	}
	return nil
}

func UnpackProjectDirectory(ctx context.Context, root string, r io.Reader) error {
	destination := acfs.Root{Path: root}
	usage, err := disk.UsageWithContext(ctx, root)
	if err != nil {
		return fmt.Errorf("check free space for the project archive: %w", err)
	}
	reserve := max(unpackReserveBytes, int64(usage.Total/20))
	budget := int64(usage.Free) - reserve
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open project archive: %w", err)
	}
	defer func() {
		// A gzip reader's Close leaves the source open and says nothing about data already read, whose errors are
		// returned as they happen.
		_ = gzipReader.Close()
	}()

	tarReader := tar.NewReader(gzipReader)
	var directories, symlinks []tar.Header
	var entries, written int64
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read project archive: %w", nextErr)
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("project archive has more than %d entries", maxArchiveEntries)
		}
		if header.Typeflag == tar.TypeReg && header.Size > budget-written {
			return fmt.Errorf(
				"project archive needs more than the %s that can be unpacked while keeping %s free on the destination filesystem",
				units.BytesSize(float64(max(budget, 0))),
				units.BytesSize(float64(reserve)),
			)
		}
		relativePath, pathErr := archiveEntryPath(header)
		if pathErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", header.Name, pathErr)
		}
		size, unpackErr := unpackProjectEntry(ctx, destination, tarReader, header, relativePath)
		if unpackErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", header.Name, unpackErr)
		}
		written += size
		if header.Typeflag == tar.TypeDir {
			directories = append(directories, tar.Header{
				Name: relativePath,
				Mode: header.Mode,
			})
		}
		if header.Typeflag == tar.TypeSymlink {
			symlinks = append(symlinks, tar.Header{
				Name:     relativePath,
				Linkname: header.Linkname,
			})
		}
	}

	// A later link can change how an earlier target resolves, so recheck each
	// link against the final tree.
	for _, link := range symlinks {
		logicalPath := "/" + link.Name
		if removeErr := destination.Remove(ctx, logicalPath); removeErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", link.Name, removeErr)
		}
		if linkErr := destination.Symlink(ctx, link.Linkname, logicalPath); linkErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", link.Name, linkErr)
		}
	}

	for _, directory := range slices.Backward(directories) {
		if chmodErr := destination.Chmod(ctx, "/"+directory.Name, os.FileMode(directory.Mode).Perm()); chmodErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", directory.Name, chmodErr)
		}
	}
	return nil
}

// archiveEntryPath validates an entry name without rewriting it, so every
// exported filename is imported unchanged.
func archiveEntryPath(header *tar.Header) (string, error) {
	name := header.Name
	if header.Typeflag == tar.TypeDir {
		name = strings.TrimSuffix(name, "/")
	}
	switch {
	case name == "":
		return "", errors.New("path is required")
	case strings.ContainsRune(name, 0):
		return "", errors.New("path contains a null byte")
	case strings.Contains(name, `\`):
		return "", errors.New("path must use forward slashes")
	case path.IsAbs(name):
		return "", errors.New("absolute paths are not allowed")
	case slices.ContainsFunc(strings.Split(name, "/"), func(component string) bool { return component == "." || component == ".." }):
		return "", errors.New("path traversal is not allowed")
	case path.Clean(name) != name:
		return "", errors.New("path is not in canonical form")
	}
	return name, nil
}

func unpackProjectEntry(ctx context.Context, destination acfs.Root, reader io.Reader, header *tar.Header, relativePath string) (int64, error) {
	logicalPath := "/" + relativePath
	switch header.Typeflag {
	case tar.TypeDir:
		return 0, destination.MkdirAll(ctx, logicalPath, 0o755)
	case tar.TypeReg:
		if err := destination.MkdirAll(ctx, path.Dir(logicalPath), 0o755); err != nil {
			return 0, err
		}
		return destination.WriteFrom(ctx, logicalPath, io.LimitReader(reader, header.Size), acfs.WriteFromOptions{
			Size:      header.Size,
			Mode:      os.FileMode(header.Mode).Perm(),
			Exclusive: true,
		})
	case tar.TypeSymlink:
		if err := destination.MkdirAll(ctx, path.Dir(logicalPath), 0o755); err != nil {
			return 0, err
		}
		return 0, destination.Symlink(ctx, header.Linkname, logicalPath)
	case tar.TypeLink:
		return 0, fmt.Errorf("hard links are not supported (target %q)", header.Linkname)
	}
	return 0, fmt.Errorf("unsupported entry type %q", header.Typeflag)
}

func ProjectBundleVolumeArchive(key string) (string, error) {
	if !composeVolumeKeyPattern.MatchString(key) || key == "." || key == ".." {
		return "", fmt.Errorf("volume key %q cannot name a bundle entry", key)
	}
	return projectBundleVolumes + key + ".tar.gz", nil
}

func NewProjectBundleWriter(w io.Writer, manifest types.ProjectManifest) (*ProjectBundleWriter, error) {
	content, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode bundle manifest: %w", err)
	}
	if len(content) > maxProjectManifestBytes {
		return nil, fmt.Errorf("bundle manifest exceeds %d bytes", maxProjectManifestBytes)
	}
	gzipWriter, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	writer := &ProjectBundleWriter{gzip: gzipWriter, tar: tar.NewWriter(gzipWriter)}
	if addErr := writer.add(ProjectBundleManifest, int64(len(content)), bytes.NewReader(content)); addErr != nil {
		return nil, addErr
	}
	return writer, nil
}

func (w *ProjectBundleWriter) Add(name string, size int64, body io.Reader) error {
	if name != ProjectBundleProject && !isProjectBundleVolumeArchive(name) {
		return fmt.Errorf("unexpected bundle entry %q", name)
	}
	return w.add(name, size, body)
}

func (w *ProjectBundleWriter) add(name string, size int64, body io.Reader) error {
	if err := w.tar.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     0o600,
		Size:     size,
	}); err != nil {
		return fmt.Errorf("write bundle entry %s: %w", name, err)
	}
	if _, err := io.CopyN(w.tar, body, size); err != nil {
		return fmt.Errorf("write bundle entry %s: %w", name, err)
	}
	return nil
}

func (w *ProjectBundleWriter) Close() error {
	if err := w.tar.Close(); err != nil {
		return fmt.Errorf("finish project bundle: %w", err)
	}
	if err := w.gzip.Close(); err != nil {
		return fmt.Errorf("finish project bundle compression: %w", err)
	}
	return nil
}

func isProjectBundleVolumeArchive(name string) bool {
	key, ok := strings.CutPrefix(name, projectBundleVolumes)
	if !ok {
		return false
	}
	key, ok = strings.CutSuffix(key, ".tar.gz")
	archive, err := ProjectBundleVolumeArchive(key)
	return ok && err == nil && archive == name
}

func ReadProjectBundle(r io.Reader, visit ProjectBundleVisitor) (types.ProjectManifest, bool, error) {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return types.ProjectManifest{}, false, nil
	}
	defer func() {
		// A gzip reader's Close leaves the source open and says nothing about data already read, whose errors are
		// returned as they happen.
		_ = gzipReader.Close()
	}()
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil || header.Name != ProjectBundleManifest {
		return types.ProjectManifest{}, false, nil
	}
	if header.Typeflag != tar.TypeReg || header.Size > maxProjectManifestBytes {
		return types.ProjectManifest{}, true, fmt.Errorf("%s must be a file of at most %d bytes", ProjectBundleManifest, maxProjectManifestBytes)
	}
	var manifest types.ProjectManifest
	if decodeErr := json.UnmarshalRead(io.LimitReader(tarReader, maxProjectManifestBytes), &manifest); decodeErr != nil {
		return types.ProjectManifest{}, true, fmt.Errorf("read %s: %w", ProjectBundleManifest, decodeErr)
	}
	expected, err := projectBundleVolumeArchives(manifest)
	if err != nil {
		return manifest, true, err
	}
	projectSeen := false
	for {
		header, err = tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return manifest, true, fmt.Errorf("read project bundle: %w", err)
		}
		name, pathErr := archiveEntryPath(header)
		if pathErr != nil {
			return manifest, true, fmt.Errorf("bundle entry %q: %w", header.Name, pathErr)
		}
		seen, isVolume := expected[name]
		switch {
		case header.Typeflag != tar.TypeReg:
			return manifest, true, fmt.Errorf("bundle entry %q is not a regular file", name)
		case name == ProjectBundleProject && !projectSeen:
			projectSeen = true
		case isVolume && projectSeen && !seen:
			expected[name] = true
		default:
			return manifest, true, fmt.Errorf("unexpected bundle entry %q", name)
		}
		if visit != nil {
			if visitErr := visit(name, header.Size, tarReader); visitErr != nil {
				return manifest, true, visitErr
			}
		}
	}
	if !projectSeen {
		return manifest, true, fmt.Errorf("project bundle has no %s", ProjectBundleProject)
	}
	for _, name := range slices.Sorted(maps.Keys(expected)) {
		if !expected[name] {
			return manifest, true, fmt.Errorf("project bundle has no %s", name)
		}
	}
	return manifest, true, nil
}

func projectBundleVolumeArchives(manifest types.ProjectManifest) (map[string]bool, error) {
	if manifest.FormatVersion != types.ProjectManifestFormatVersion {
		return nil, fmt.Errorf("unsupported project bundle format version %d", manifest.FormatVersion)
	}
	owners := map[string]string{}
	keys := map[string]struct{}{}
	for _, volume := range manifest.Volumes {
		if _, duplicate := keys[volume.Key]; duplicate {
			return nil, fmt.Errorf("project bundle lists volume %q twice", volume.Key)
		}
		keys[volume.Key] = struct{}{}
		archive, err := ProjectBundleVolumeArchive(volume.Key)
		if err != nil {
			return nil, err
		}
		switch {
		case !volume.Included && volume.Archive != "":
			return nil, fmt.Errorf("volume %q is not included but names archive %q", volume.Key, volume.Archive)
		case len(volume.DockerLabels) > 0 && (!volume.Included || volume.Kind != types.ProjectVolumeExternal):
			return nil, fmt.Errorf("volume %q records Docker labels but is not an included external volume", volume.Key)
		case !volume.Included:
		case !dockerVolumeNamePattern.MatchString(volume.Name):
			return nil, fmt.Errorf("volume %q has invalid name %q", volume.Key, volume.Name)
		case volume.Archive == archive:
			owners[archive] = volume.Name
		}
		if labelErr := ValidateVolumeLabels(volume.DockerLabels); labelErr != nil {
			return nil, fmt.Errorf("volume %q: %w", volume.Key, labelErr)
		}
	}
	expected := map[string]bool{}
	for _, volume := range manifest.Volumes {
		if !volume.Included {
			continue
		}
		if owner, ok := owners[volume.Archive]; !ok || owner != volume.Name {
			archive, _ := ProjectBundleVolumeArchive(volume.Key)
			return nil, fmt.Errorf("volume %q must be archived at %s or share the archive of another volume named %s, not %q", volume.Key, archive, volume.Name, volume.Archive)
		}
		expected[volume.Archive] = false
	}
	return expected, nil
}

func ValidateVolumeLabels(labels map[string]string) error {
	if len(labels) > maxVolumeLabels {
		return fmt.Errorf("more than %d labels", maxVolumeLabels)
	}
	size := 0
	for key, value := range labels {
		if key == "" {
			return errors.New("a label has an empty key")
		}
		size += len(key) + len(value)
	}
	if size > maxVolumeLabelBytes {
		return fmt.Errorf("labels exceed %d bytes", maxVolumeLabelBytes)
	}
	return nil
}
