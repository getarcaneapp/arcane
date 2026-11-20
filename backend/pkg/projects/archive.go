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
			linkTarget, inside, err := LinkTargetInside(ctx, root, entry.Path, resolvedRoot)
			if err != nil {
				return err
			}
			if inside {
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

// LinkTargetInside reports whether the link at logicalPath in root leads into the directory resolvedRoot, and returns
// the target a copy of that directory keeps for it. A target that leaves root on the way, absolute or through "..",
// becomes the relative path to the same file, so the copy's link leads to the copy's file, not the original's.
func LinkTargetInside(ctx context.Context, root acfs.Root, logicalPath, resolvedRoot string) (target string, inside bool, err error) {
	// A link that doesn't resolve leads to no file to keep.
	resolvedPath, resolveErr := filepath.EvalSymlinks(filepath.Join(root.Path, logicalPath))
	if resolveErr != nil || !IsSafeSubdirectory(resolvedRoot, resolvedPath) {
		return "", false, nil
	}
	target, err = root.Readlink(ctx, logicalPath)
	if err != nil {
		return "", false, err
	}
	if !staysInside(logicalPath, target) {
		target, err = filepath.Rel(filepath.Dir(filepath.Join(resolvedRoot, logicalPath)), resolvedPath)
	}
	return target, err == nil, err
}

// ReadExternalEnvFile reads the file the project's .env links to, with its permissions, when the link leads outside
// the project directory, so a copy of the directory alone would keep only the link. external is false for a regular
// .env, a link inside the directory, or no .env at all.
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

// staysInside reports whether target, of the link at logicalPath, names a path in the link's root without leaving it.
// It is checked without the root's own name, which a target like "../web/x" passes through on its way back in.
func staysInside(logicalPath, target string) bool {
	return !filepath.IsAbs(target) && filepath.IsLocal(filepath.Join(strings.TrimPrefix(path.Dir(logicalPath), "/"), target))
}

// OwnLinks points each link in the copy at projectPath that leads into one of sourceDirs, the directories the copy was
// taken from, at the copy's own path for the same file. Targets are read lexically, since the original may be gone. A
// link in a folder Arcane can't change keeps its target, and report names every such link once.
func OwnLinks(ctx context.Context, projectPath string, sourceDirs []string, report func(ctx context.Context, message string)) error {
	resolvedRoot, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	root := acfs.Root{Path: projectPath}
	var readOnly []string
	// A directory Arcane can't read, such as a database's data, keeps its links as they are rather than failing.
	walkErr := filepath.WalkDir(resolvedRoot, func(hostPath string, entry fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrPermission):
			return fs.SkipDir
		case err != nil:
			return err
		case entry.IsDir() && IsInternalScratchDirName(entry.Name()):
			return fs.SkipDir
		case entry.Type()&fs.ModeSymlink == 0:
			return nil
		}
		relative, relErr := filepath.Rel(resolvedRoot, hostPath)
		if relErr != nil {
			return relErr
		}
		logicalPath := "/" + filepath.ToSlash(relative)
		target, linkErr := root.Readlink(ctx, logicalPath)
		if errors.Is(linkErr, fs.ErrPermission) {
			readOnly = append(readOnly, relative)
			return nil
		}
		if linkErr != nil || staysInside(logicalPath, target) {
			return linkErr
		}
		linkDir := filepath.Dir(relative)
		for index, dir := range sourceDirs {
			named := target
			if !filepath.IsAbs(target) {
				// The system follows ".." from where the directory really is, the last of the recorded directories.
				if index != len(sourceDirs)-1 {
					continue
				}
				named = filepath.Join(dir, linkDir, target)
			}
			if !IsSafeSubdirectory(dir, named) {
				continue
			}
			own, ownErr := filepath.Rel(filepath.Join(dir, linkDir), named)
			if ownErr != nil {
				return ownErr
			}
			linkErr = root.Remove(ctx, logicalPath)
			if linkErr == nil {
				linkErr = root.Symlink(ctx, own, logicalPath)
			}
			if errors.Is(linkErr, fs.ErrPermission) {
				readOnly = append(readOnly, relative)
				return nil
			}
			return linkErr
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("point the restored links at the project's own files: %w", walkErr)
	}
	if len(readOnly) > 0 {
		report(ctx, "Arcane can't change links in read-only folders, so these still lead into the original project: "+strings.Join(readOnly, ", "))
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

func IsProjectBundle(r io.Reader) bool {
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return false
	}
	defer func() {
		_ = gzipReader.Close()
	}()
	_, bundled, _ := readProjectBundleManifest(tar.NewReader(gzipReader))
	return bundled
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
	var probe struct {
		Kind          string `json:"kind"`
		FormatVersion int    `json:"formatVersion"`
	}
	if json.Unmarshal(content, &probe) != nil || probe.Kind != types.ProjectManifestKindExport || probe.FormatVersion == 0 {
		return types.ProjectManifest{}, false, nil
	}
	var manifest types.ProjectManifest
	if decodeErr := json.Unmarshal(content, &manifest); decodeErr != nil {
		return types.ProjectManifest{}, true, fmt.Errorf("read %s: %w", ProjectBundleManifest, decodeErr)
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

// SelectBackupVolumes returns the requested volume keys, or every included one when the request names none; the
// backup must include each requested key.
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
