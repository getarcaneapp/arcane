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
	"uuid"

	kit "kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

type (
	SwapPlan struct {
		root       acfs.Root
		staging    string
		trash      string
		removals   []string
		placements []swapPlacement
	}

	swapPlacement struct {
		folder  *acfstypes.Entry
		path    string
		replace bool
	}
)

const (
	SwapStagingPrefix = ".arcane-snapshot-restore-"
	SwapTrashPrefix   = ".arcane-snapshot-trash-"
)

var ErrSwapNotApplied = errors.New("the live data was left as it was")

func RemoveSwapLeftovers(ctx context.Context, directory, prefix string) error {
	root := acfs.Root{Path: directory}
	entries, err := root.List(ctx, "/")
	if err != nil {
		return fmt.Errorf("list %s for leftover restore folders: %w", directory, err)
	}
	var removeErr error
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, prefix) {
			removeErr = errors.Join(removeErr, root.RemoveAll(ctx, "/"+entry.Name))
		}
	}
	return removeErr
}

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
		trash:   SwapTrashPrefix + swap.ID + "-" + uuid.New().String(),
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
	maps.DeleteFunc(parents, func(parent string, _ struct{}) bool {
		return slices.ContainsFunc(swap.Preserve, func(candidate string) bool { return kit.FilePathMatches(parent, candidate) })
	})
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

func (p *SwapPlan) visit(ctx context.Context, relative string, preserved, parents map[string]struct{}) error {
	live, err := p.children(ctx, relative)
	if err != nil {
		return err
	}
	staged, err := p.children(ctx, path.Join(p.staging, relative))
	if err != nil {
		return err
	}
	names := slices.Concat(slices.Collect(maps.Keys(live)), slices.Collect(maps.Keys(staged)))
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

func (p SwapPlan) Apply(ctx context.Context) (err error) {
	if err = p.root.Mkdir(ctx, "/"+p.trash, 0o700); err != nil {
		return fmt.Errorf("create the restore trash folder: %w; %w", err, ErrSwapNotApplied)
	}
	undoCtx := context.WithoutCancel(ctx)
	var undo []func() error
	defer func() {
		if err == nil {
			return
		}
		var undoErr error
		for _, step := range slices.Backward(undo) {
			undoErr = errors.Join(undoErr, step())
		}
		if undoErr == nil {
			undoErr = p.root.Remove(undoCtx, "/"+p.trash)
		}
		if undoErr != nil {
			err = errors.Join(err, fmt.Errorf("undo the partial swap: %w", undoErr))
			return
		}
		err = fmt.Errorf("%w; %w", err, ErrSwapNotApplied)
	}()
	move := func(source, target string) error {
		if renameErr := p.root.Rename(ctx, "/"+source, "/"+target); renameErr != nil {
			return renameErr
		}
		undo = append(undo, func() error { return p.root.Rename(undoCtx, "/"+target, "/"+source) })
		return nil
	}
	discarded := 0
	discard := func(entryPath string) error {
		discarded++
		if discardErr := move(entryPath, path.Join(p.trash, strconv.Itoa(discarded))); discardErr != nil {
			return fmt.Errorf("move %s aside: %w", entryPath, discardErr)
		}
		return nil
	}
	for _, entryPath := range p.removals {
		if discardErr := discard(entryPath); discardErr != nil {
			return discardErr
		}
	}
	for _, placement := range p.placements {
		if placement.folder != nil {
			mode := os.FileMode(placement.folder.UnixMode)
			if err = p.root.Mkdir(ctx, "/"+placement.path, mode.Perm()); err != nil {
				return fmt.Errorf("create folder %s: %w", placement.path, err)
			}
			undo = append(undo, func() error { return p.root.Remove(undoCtx, "/"+placement.path) })
			err = errors.Join(
				p.root.Chmod(ctx, "/"+placement.path, mode&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky)),
				p.root.Lchown(ctx, "/"+placement.path, placement.folder.UID, placement.folder.GID),
			)
			if err != nil {
				return fmt.Errorf("create folder %s: %w", placement.path, err)
			}
			continue
		}
		if placement.replace {
			if discardErr := discard(placement.path); discardErr != nil {
				return discardErr
			}
		}
		if err = move(path.Join(p.staging, placement.path), placement.path); err != nil {
			return fmt.Errorf("move restored %s into place: %w", placement.path, err)
		}
	}
	if cleanupErr := errors.Join(p.root.RemoveAll(ctx, "/"+p.staging), RemoveSwapLeftovers(ctx, p.root.Path, SwapTrashPrefix)); cleanupErr != nil {
		slog.WarnContext(ctx, "Restored data is in place, but the staging folders could not be removed", "directory", p.root.Path, "error", cleanupErr)
	}
	return nil
}
