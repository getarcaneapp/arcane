package backup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

type (
	// SwapPlan moves a staged restore into a data folder without entering its preserved paths.
	SwapPlan struct {
		root       acfs.Root
		staging    string
		trash      string
		removals   []string
		placements []swapPlacement
	}

	swapPlacement struct {
		// folder is set for a missing protected parent, which is created rather than moved in.
		folder  *acfstypes.Entry
		path    string
		replace bool
	}
)

const (
	SwapStagingPrefix = ".arcane-snapshot-restore-"
	SwapTrashPrefix   = ".arcane-snapshot-trash-"
)

// RemoveSwapLeftovers removes the staging and trash folders an earlier staged restore left in directory.
func RemoveSwapLeftovers(ctx context.Context, directory string) error {
	root := acfs.Root{Path: directory}
	entries, err := root.List(ctx, "/")
	if err != nil {
		return fmt.Errorf("list %s for leftover restore folders: %w", directory, err)
	}
	var removeErr error
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, SwapStagingPrefix) || strings.HasPrefix(entry.Name, SwapTrashPrefix) {
			removeErr = errors.Join(removeErr, root.RemoveAll(ctx, "/"+entry.Name))
		}
	}
	return removeErr
}

// PlanSwap checks the staged restore of swap.Directory and lists every change before any is made.
func PlanSwap(ctx context.Context, swap types.RecoveryRestoreSwap) (SwapPlan, error) {
	if !filepath.IsAbs(swap.Directory) {
		return SwapPlan{}, fmt.Errorf("the restore folder %q is not absolute", swap.Directory)
	}
	if swap.ID == "" || strings.ContainsAny(swap.ID, `/\`) || strings.TrimSpace(swap.ID) != swap.ID {
		return SwapPlan{}, fmt.Errorf("invalid staged restore id %q", swap.ID)
	}
	plan := SwapPlan{
		root:    acfs.Root{Path: swap.Directory},
		staging: SwapStagingPrefix + swap.ID,
		trash:   SwapTrashPrefix + swap.ID,
	}
	staged, err := plan.root.Stat(ctx, "/"+plan.staging, acfstypes.StatOptions{})
	if err != nil {
		return SwapPlan{}, fmt.Errorf("open the staged restore: %w", err)
	}
	if !staged.IsDirectory || staged.IsSymlink {
		return SwapPlan{}, fmt.Errorf("the staged restore %s is not a folder", plan.staging)
	}
	preserved := make(map[string]struct{}, len(swap.Preserve))
	parents := make(map[string]struct{})
	for _, candidate := range swap.Preserve {
		cleaned, normalizeErr := kit.NormalizeRelativePath(candidate)
		if normalizeErr != nil || cleaned != candidate {
			return SwapPlan{}, fmt.Errorf("invalid preserved path %q", candidate)
		}
		if first, _, _ := strings.Cut(cleaned, "/"); strings.HasPrefix(first, SwapStagingPrefix) || strings.HasPrefix(first, SwapTrashPrefix) {
			return SwapPlan{}, fmt.Errorf("invalid preserved path %q", candidate)
		}
		preserved[cleaned] = struct{}{}
		for parent := path.Dir(cleaned); parent != "."; parent = path.Dir(parent) {
			parents[parent] = struct{}{}
		}
	}
	// A preserved path is never entered, so folders inside it are not parents to protect.
	maps.DeleteFunc(parents, func(parent string, _ struct{}) bool {
		return slices.ContainsFunc(slices.Collect(maps.Keys(preserved)), func(candidate string) bool { return kit.FilePathMatches(parent, candidate) })
	})
	// A protected parent must be a real folder on both sides, so the swap never follows a link out of the data folder.
	for _, parent := range slices.Sorted(maps.Keys(parents)) {
		for _, logical := range []string{"/" + parent, "/" + path.Join(plan.staging, parent)} {
			entry, statErr := plan.root.Stat(ctx, logical, acfstypes.StatOptions{})
			if errors.Is(statErr, fs.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return SwapPlan{}, statErr
			}
			if !entry.IsDirectory || entry.IsSymlink {
				return SwapPlan{}, fmt.Errorf("%s must be a folder because it holds a projects folder", strings.TrimPrefix(logical, "/"))
			}
		}
	}
	if visitErr := plan.visit(ctx, "", preserved, parents); visitErr != nil {
		return SwapPlan{}, visitErr
	}
	return plan, nil
}

// visit plans the folder at relative, which is the data folder itself or a protected parent.
func (p *SwapPlan) visit(ctx context.Context, relative string, preserved, parents map[string]struct{}) error {
	live, err := p.children(ctx, relative)
	if err != nil {
		return err
	}
	staged, err := p.children(ctx, path.Join(p.staging, relative))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(live)+len(staged))
	for name := range live {
		names = append(names, name)
	}
	for name := range staged {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		entryPath := path.Join(relative, name)
		if relative == "" && (strings.HasPrefix(name, SwapStagingPrefix) || strings.HasPrefix(name, SwapTrashPrefix)) {
			continue
		}
		if _, skip := preserved[entryPath]; skip {
			continue
		}
		_, inLive := live[name]
		stagedEntry, inStaged := staged[name]
		if _, parent := parents[entryPath]; parent {
			if !inLive && inStaged {
				p.placements = append(p.placements, swapPlacement{path: entryPath, folder: &stagedEntry})
			}
			if inLive || inStaged {
				if visitErr := p.visit(ctx, entryPath, preserved, parents); visitErr != nil {
					return visitErr
				}
			}
			continue
		}
		switch {
		case inStaged:
			p.placements = append(p.placements, swapPlacement{path: entryPath, replace: inLive})
		case inLive:
			p.removals = append(p.removals, entryPath)
		}
	}
	return nil
}

func (p *SwapPlan) children(ctx context.Context, relative string) (map[string]acfstypes.Entry, error) {
	entries, err := p.root.List(ctx, "/"+relative)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", cmp.Or(relative, "the data folder"), err)
	}
	children := make(map[string]acfstypes.Entry, len(entries))
	for _, entry := range entries {
		children[entry.Name] = entry
	}
	return children, nil
}

// Apply removes live entries missing from the backup first, so stale SQLite sidecars are gone before a database moves
// in, then moves each staged entry into place; replaced entries wait in the trash until the swap finishes.
func (p SwapPlan) Apply(ctx context.Context) error {
	if err := p.root.Mkdir(ctx, "/"+p.trash, 0o700); err != nil {
		return fmt.Errorf("create the restore trash folder: %w", err)
	}
	discarded := 0
	discard := func(entryPath string) error {
		discarded++
		if err := p.root.Rename(ctx, "/"+entryPath, "/"+path.Join(p.trash, strconv.Itoa(discarded))); err != nil {
			return fmt.Errorf("move %s aside: %w", entryPath, err)
		}
		return nil
	}
	for _, entryPath := range p.removals {
		if err := discard(entryPath); err != nil {
			return err
		}
	}
	for _, placement := range p.placements {
		if placement.folder != nil {
			mode := os.FileMode(placement.folder.UnixMode)
			err := errors.Join(
				p.root.Mkdir(ctx, "/"+placement.path, mode.Perm()),
				p.root.Chmod(ctx, "/"+placement.path, mode&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky)),
				p.root.Lchown(ctx, "/"+placement.path, placement.folder.UID, placement.folder.GID),
			)
			if err != nil {
				return fmt.Errorf("create folder %s: %w", placement.path, err)
			}
			continue
		}
		if placement.replace {
			if err := discard(placement.path); err != nil {
				return err
			}
		}
		if err := p.root.Rename(ctx, "/"+path.Join(p.staging, placement.path), "/"+placement.path); err != nil {
			return fmt.Errorf("move restored %s into place: %w", placement.path, err)
		}
	}
	if err := errors.Join(p.root.RemoveAll(ctx, "/"+p.staging), p.root.RemoveAll(ctx, "/"+p.trash)); err != nil {
		slog.WarnContext(ctx, "Restored data is in place, but the staging folders could not be removed", "directory", p.root.Path, "error", err)
	}
	return nil
}
