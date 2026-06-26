package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
)

const internalVolumeHelperDefaultPath = "/volume"

type internalVolumeHelperProbeOutput struct {
	Path           string `json:"path"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type internalVolumeHelperFileKey struct {
	dev string
	ino uint64
}

var internalVolumeHelperProbePath string

var internalVolumeHelperCmd = &cobra.Command{
	Use:    "internal-volume-helper",
	Hidden: true,
}

var internalVolumeHelperProbeCmd = &cobra.Command{
	Use:    "probe",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInternalVolumeHelperProbeInternal(cmd.Context(), internalVolumeHelperProbePath, cmd.OutOrStdout())
	},
}

func runInternalVolumeHelperProbeInternal(ctx context.Context, probePath string, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}

	result, err := probeVolumeFilesystemInternal(ctx, probePath)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(output).Encode(result); err != nil {
		return fmt.Errorf("encode volume probe output: %w", err)
	}
	return nil
}

func probeVolumeFilesystemInternal(ctx context.Context, probePath string) (*internalVolumeHelperProbeOutput, error) {
	if probePath == "" {
		probePath = internalVolumeHelperDefaultPath
	}

	cleanPath := filepath.Clean(probePath)
	availableBytes, err := statfsAvailableBytesInternal(cleanPath)
	if err != nil {
		return nil, err
	}

	allocatedBytes, err := allocatedBytesForPathInternal(ctx, cleanPath)
	if err != nil {
		return nil, err
	}

	return &internalVolumeHelperProbeOutput{
		Path:           cleanPath,
		AllocatedBytes: allocatedBytes,
		AvailableBytes: availableBytes,
	}, nil
}

func statfsAvailableBytesInternal(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}

	if stat.Bavail == 0 || stat.Bsize <= 0 {
		return 0, nil
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func allocatedBytesForPathInternal(ctx context.Context, root string) (uint64, error) {
	seen := map[internalVolumeHelperFileKey]struct{}{}
	var total uint64

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if internalVolumeHelperSkippableWalkError(walkErr) {
				slog.Warn("skipping inaccessible volume probe path", "path", path, "error", walkErr)
				return nil
			}
			return walkErr
		}

		info, err := entry.Info()
		if err != nil {
			if internalVolumeHelperSkippableWalkError(err) {
				slog.Warn("skipping inaccessible volume probe path", "path", path, "error", err)
				return nil
			}
			return err
		}

		allocated, key, linked := allocatedBytesForFileInternal(info)
		if linked {
			if _, ok := seen[key]; ok {
				return nil
			}
			seen[key] = struct{}{}
		}
		total += allocated
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, err
		}
		return 0, fmt.Errorf("walk %s: %w", root, err)
	}

	return total, nil
}

func internalVolumeHelperSkippableWalkError(err error) bool {
	return os.IsPermission(err) ||
		errors.Is(err, os.ErrPermission) ||
		errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.ENOENT)
}

func allocatedBytesForFileInternal(info os.FileInfo) (uint64, internalVolumeHelperFileKey, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return fileSizeBytesInternal(info), internalVolumeHelperFileKey{}, false
	}

	key := internalVolumeHelperFileKey{dev: strconv.FormatInt(int64(stat.Dev), 10), ino: stat.Ino}
	linked := stat.Nlink > 1
	if stat.Blocks > 0 {
		return uint64(stat.Blocks) * 512, key, linked
	}
	return fileSizeBytesInternal(info), key, linked
}

func fileSizeBytesInternal(info os.FileInfo) uint64 {
	size := info.Size()
	if size <= 0 {
		return 0
	}
	return uint64(size)
}

func init() {
	internalVolumeHelperProbeCmd.Flags().StringVar(&internalVolumeHelperProbePath, "path", internalVolumeHelperDefaultPath, "Volume mount path to probe")
	internalVolumeHelperCmd.AddCommand(internalVolumeHelperProbeCmd)
	rootCmd.AddCommand(internalVolumeHelperCmd)
}
