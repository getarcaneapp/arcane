package projects

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/go-units"
	"github.com/shirou/gopsutil/v4/disk"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/acfs/types"
)

const (
	maxArchiveEntries  = 200_000
	unpackReserveBytes = 1 << 30
)

func PackProjectDirectory(ctx context.Context, projectPath string, w io.Writer) error {
	resolvedRoot, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	root := acfs.Root{Path: projectPath}
	gzipWriter := gzip.NewWriter(w)
	tarWriter := tar.NewWriter(gzipWriter)
	visit := func(entry types.Entry) error {
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
			if err := tarWriter.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg,
				Name:     relativePath,
				Mode:     int64(perm),
				Size:     int64(len(content)),
				ModTime:  entry.ModTime,
			}); err != nil {
				return err
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
		if err := tarWriter.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     relativePath,
			Mode:     int64(mode.Perm()),
			Size:     entry.Size,
			ModTime:  entry.ModTime,
		}); err != nil {
			return err
		}
		_, err = io.CopyN(tarWriter, reader, entry.Size)
		return err
	}
	if _, walkErr := root.Walk(ctx, "/", types.WalkOptions{}, visit); walkErr != nil {
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
		_ = gzipReader.Close()
	}()

	tarReader := tar.NewReader(gzipReader)
	var directories []tar.Header
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
			return fmt.Errorf("project archive needs more than the %s that can be unpacked while keeping %s free on the destination filesystem", units.BytesSize(float64(max(budget, 0))), units.BytesSize(float64(reserve)))
		}
		relativePath, normalizeErr := kit.NormalizeRelativePath(header.Name)
		if normalizeErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", header.Name, normalizeErr)
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
	}

	for _, directory := range slices.Backward(directories) {
		if chmodErr := destination.Chmod(ctx, "/"+directory.Name, os.FileMode(directory.Mode).Perm()); chmodErr != nil {
			return fmt.Errorf("unpack archive entry %q: %w", directory.Name, chmodErr)
		}
	}
	return nil
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
		return 0, destination.Symlink(ctx, path.Clean(header.Linkname), logicalPath)
	case tar.TypeLink:
		return 0, fmt.Errorf("hard links are not supported (target %q)", header.Linkname)
	}
	return 0, fmt.Errorf("unsupported entry type %q", header.Typeflag)
}
