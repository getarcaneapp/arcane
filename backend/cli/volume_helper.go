package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	dev uint64
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

	if stat.Bavail <= 0 || stat.Bsize <= 0 {
		return 0, nil
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func allocatedBytesForPathInternal(ctx context.Context, root string) (uint64, error) {
	seen := map[internalVolumeHelperFileKey]struct{}{}
	var total uint64

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
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

func allocatedBytesForFileInternal(info os.FileInfo) (uint64, internalVolumeHelperFileKey, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		if info.Size() <= 0 {
			return 0, internalVolumeHelperFileKey{}, false
		}
		return uint64(info.Size()), internalVolumeHelperFileKey{}, false
	}

	key := internalVolumeHelperFileKey{dev: uint64(stat.Dev), ino: uint64(stat.Ino)}
	linked := stat.Nlink > 1
	if stat.Blocks > 0 {
		return uint64(stat.Blocks) * 512, key, linked
	}
	if info.Size() <= 0 {
		return 0, key, linked
	}
	return uint64(info.Size()), key, linked
}

func init() {
	internalVolumeHelperProbeCmd.Flags().StringVar(&internalVolumeHelperProbePath, "path", internalVolumeHelperDefaultPath, "Volume mount path to probe")
	internalVolumeHelperCmd.AddCommand(internalVolumeHelperProbeCmd)
	rootCmd.AddCommand(internalVolumeHelperCmd)
}
