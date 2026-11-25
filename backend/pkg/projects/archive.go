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

	ProjectBundleVisitor func(manifest types.ProjectManifest, name string, size int64, body io.Reader) error
)

const (
	ProjectBundleManifest = "arcane-project.json"
	ProjectBundleProject  = "project.tar.gz"

	maxArchiveEntries       = 200_000
	unpackReserveBytes      = 1 << 30
	maxProjectManifestBytes = 1 << 20
	projectBundleVolumes    = "volumes/"
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
			content, perm, external, err := ReadExternalEnvFile(ctx, projectPath)
			if err != nil || !external {
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

func ReadExternalEnvFile(ctx context.Context, projectPath string) (content []byte, perm os.FileMode, external bool, err error) {
	envPath, perm, isSymlink, err := resolveEnvFileWriteTarget(ctx, filepath.Join(projectPath, EffectiveEnvFileName))
	if err != nil || !isSymlink {
		return nil, 0, false, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return nil, 0, false, fmt.Errorf("resolve project directory: %w", err)
	}
	if IsSafeSubdirectory(resolvedRoot, envPath) {
		return nil, 0, false, nil
	}
	content, err = acfs.Host{}.ReadFile(ctx, envPath)
	if err != nil {
		return nil, 0, false, err
	}
	return content, perm, true, nil
}

func OwnLinks(ctx context.Context, projectPath string, sourceDirs []string, report func(ctx context.Context, message string)) error {
	if len(sourceDirs) == 0 {
		return nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	var readOnly []string
	walkErr := filepath.WalkDir(resolvedRoot, func(hostPath string, entry fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrPermission):
			return fs.SkipDir
		case err != nil:
			return err
		case hostPath != resolvedRoot && entry.IsDir() && IsInternalScratchDirName(entry.Name()):
			return fs.SkipDir
		case entry.Type()&fs.ModeSymlink == 0:
			return nil
		}
		relative, err := filepath.Rel(resolvedRoot, hostPath)
		if err != nil {
			return err
		}
		target, err := os.Readlink(hostPath)
		switch {
		case err != nil:
		case filepath.IsAbs(target):
			err = ownLink(hostPath, resolvedRoot, target, sourceDirs)
		case !filepath.IsLocal(filepath.Join(filepath.Dir(relative), target)):
			err = ownLink(hostPath, resolvedRoot, filepath.Join(sourceDirs[len(sourceDirs)-1], filepath.Dir(relative), target), sourceDirs)
		}
		if errors.Is(err, fs.ErrPermission) {
			readOnly = append(readOnly, relative)
			return nil
		}
		return err
	})
	if walkErr != nil {
		return fmt.Errorf("point the restored links at the project's own files: %w", walkErr)
	}
	if len(readOnly) > 0 {
		report(ctx, "Arcane can't change links in read-only folders, so these still lead into the original project: "+strings.Join(readOnly, ", "))
	}
	return nil
}

func ownLink(hostPath, resolvedRoot, named string, sourceDirs []string) error {
	for _, dir := range sourceDirs {
		inside, err := filepath.Rel(dir, named)
		if err != nil || !filepath.IsLocal(inside) {
			continue
		}
		own, err := filepath.Rel(filepath.Dir(hostPath), filepath.Join(resolvedRoot, inside))
		if err != nil {
			return err
		}
		if removeErr := os.Remove(hostPath); removeErr != nil {
			return removeErr
		}
		return os.Symlink(own, hostPath)
	}
	return nil
}

func FreeSpaceBudget(ctx context.Context, dir string) (budget, reserve int64, err error) {
	usage, err := disk.UsageWithContext(ctx, dir)
	if err != nil {
		return 0, 0, err
	}
	reserve = max(unpackReserveBytes, int64(usage.Total/20))
	return int64(usage.Free) - reserve, reserve, nil
}

func UnpackProjectDirectory(ctx context.Context, root string, r io.Reader) error {
	destination := acfs.Root{Path: root}
	budget, reserve, err := FreeSpaceBudget(ctx, root)
	if err != nil {
		return fmt.Errorf("check free space for the project archive: %w", err)
	}
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open project archive: %w", err)
	}
	defer func() {
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

func ProjectBundleVolumeArchive(key string) string {
	return projectBundleVolumes + key + ".tar.gz"
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
	if addErr := writer.Add(ProjectBundleManifest, int64(len(content)), bytes.NewReader(content)); addErr != nil {
		return nil, addErr
	}
	return writer, nil
}

func (w *ProjectBundleWriter) Add(name string, size int64, body io.Reader) error {
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

func ReadProjectBundleManifest(r io.Reader) (types.ProjectManifest, bool) {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return types.ProjectManifest{}, false
	}
	defer func() {
		_ = gzipReader.Close()
	}()
	manifest, bundled, _ := readProjectBundleManifest(tar.NewReader(gzipReader))
	return manifest, bundled
}

func readProjectBundleManifest(tarReader *tar.Reader) (types.ProjectManifest, bool, error) {
	header, err := tarReader.Next()
	if err != nil || header.Name != ProjectBundleManifest || header.Typeflag != tar.TypeReg || header.Size > maxProjectManifestBytes {
		return types.ProjectManifest{}, false, nil
	}
	content, err := io.ReadAll(tarReader)
	if err != nil {
		return types.ProjectManifest{}, true, fmt.Errorf("read %s: %w", ProjectBundleManifest, err)
	}
	var manifest types.ProjectManifest
	if json.Unmarshal(content, &manifest) != nil || manifest.Kind != types.ProjectManifestKindExport || manifest.FormatVersion == 0 {
		return types.ProjectManifest{}, false, nil
	}
	return manifest, true, nil
}

func ReadProjectBundle(r io.Reader, visit ProjectBundleVisitor) (types.ProjectManifest, bool, error) {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return types.ProjectManifest{}, false, nil
	}
	defer func() {
		_ = gzipReader.Close()
	}()
	tarReader := tar.NewReader(gzipReader)
	manifest, bundled, err := readProjectBundleManifest(tarReader)
	if !bundled || err != nil {
		return manifest, bundled, err
	}
	expected, err := projectBundleVolumeArchives(manifest)
	if err != nil {
		return manifest, true, err
	}
	projectSeen := false
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return manifest, true, fmt.Errorf("read project bundle: %w", nextErr)
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
			if visitErr := visit(manifest, name, header.Size, tarReader); visitErr != nil {
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
		archive := ProjectBundleVolumeArchive(volume.Key)
		switch {
		case !volume.Included && volume.Archive != "":
			return nil, fmt.Errorf("volume %q is not included but names archive %q", volume.Key, volume.Archive)
		case volume.Included && volume.Archive == archive:
			owners[archive] = volume.Name
		}
	}
	expected := map[string]bool{}
	for _, volume := range manifest.Volumes {
		if !volume.Included {
			continue
		}
		if owner, ok := owners[volume.Archive]; !ok || owner != volume.Name {
			return nil, fmt.Errorf("volume %q must be archived at %s or share the archive of another volume named %s, not %q", volume.Key, ProjectBundleVolumeArchive(volume.Key), volume.Name, volume.Archive)
		}
		expected[volume.Archive] = false
	}
	return expected, nil
}

func SelectBackupVolumes(included, requested []string) ([]string, error) {
	if requested == nil {
		return included, nil
	}
	for _, key := range requested {
		if !slices.Contains(included, key) {
			return nil, fmt.Errorf("the backup does not include the data of volume %s", key)
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(requested))), nil
}
