package backup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"getarcane.dev/arcane/v3/internal/common"
)

type (
	// RestoreState is what a restore knows about its target; Apply records the safety backup it takes.
	RestoreState struct {
		Redeploy *bool                        `json:"redeploy,omitempty"`
		SafetyID string                       `json:"safetyId,omitempty"`
		Create   []client.VolumeCreateOptions `json:"create,omitempty"`
		// MayStop records that the target ran when the restore was requested, so the caller's right to stop it was checked.
		MayStop bool `json:"mayStop"`
		Running bool `json:"running"`
	}

	// RestoreSteps are the operations a restore applies to its target, bound to its run; tests pass fakes. A restore
	// into a new target has no Stop, Safety or InUse: a failure discards what it created instead of undoing it.
	RestoreSteps struct {
		Report func(ctx context.Context, message string)
		Stop   func(ctx context.Context, state RestoreState) error
		Deploy func(ctx context.Context) error
		Safety func(ctx context.Context) (string, error)
		InUse  func(ctx context.Context) error
		Create func(ctx context.Context, options client.VolumeCreateOptions) error
		// Write restores the backup or, given a safety backup's ID, that safety backup.
		Write  func(ctx context.Context, safetyID string) error
		Verify func(ctx context.Context) error
		// Remove removes the volumes the restore created.
		Remove func(ctx context.Context, names []string) error
		// Discard removes the new target a failed restore leaves behind, reporting whether there was one and naming what
		// it could not remove.
		Discard func(ctx context.Context) (bool, []string)
		// Subject names the target in messages; it defaults to "the project".
		Subject string
	}

	// RestorePart pairs a part of a capture with the mount it is restored into.
	RestorePart struct {
		Key string
		// Snapshot names the part under volumes/ in the capture.
		Snapshot string
		Target   string
	}
)

// RestoreRoot is where a restore mounts its targets: project/ and volumes/<name>/, as a capture lays them out.
const RestoreRoot = "/dest"

// Apply stops the target, takes a safety backup and restores the backup; on any failure the target is rolled back and
// deployed again if it ran, or discarded when the restore created it.
func (r RestoreSteps) Apply(ctx context.Context, state RestoreState) error {
	subject := r.subject()
	if r.Stop != nil {
		if state.Running {
			r.report(ctx, "Stopping "+subject)
		}
		// A stopped container can be started on its own, without the project lock the restore holds, so any running one
		// is stopped.
		if err := r.Stop(ctx, state); err != nil {
			return r.rollback(ctx, state, nil, fmt.Errorf("stop %s: %w", subject, err))
		}
	}
	if r.Safety != nil {
		r.report(ctx, "Taking a safety backup")
		safetyID, err := r.Safety(ctx)
		if err != nil {
			return r.rollback(ctx, state, nil, fmt.Errorf("take a safety backup: %w", err))
		}
		// A container started during the safety backup could be using a volume now; nothing has been written yet, so
		// the rollback leaves the volumes alone.
		if r.InUse != nil {
			if err = r.InUse(ctx); err != nil {
				return r.rollback(ctx, state, nil, err)
			}
		}
		state.SafetyID = safetyID
	}
	r.report(ctx, "Restoring the backup")
	// Only the volumes this restore created are removed on rollback.
	var created []string
	for _, options := range state.Create {
		if err := r.Create(ctx, options); err != nil {
			return r.rollback(ctx, state, created, fmt.Errorf("create the missing volumes: %w", err))
		}
		created = append(created, options.Name)
	}
	err := r.Write(ctx, "")
	if err == nil && r.Verify != nil {
		err = r.Verify(ctx)
	}
	if err != nil {
		return r.rollback(ctx, state, created, err)
	}
	// Deployed when asked, by default when it ran before the restore.
	if !*cmp.Or(state.Redeploy, new(state.Running)) {
		return nil
	}
	r.report(ctx, "Deploying "+subject)
	// The project the restore deploys is its own to stop again on rollback.
	state.MayStop = true
	if err = r.Deploy(ctx); err != nil {
		return r.rollback(ctx, state, created, fmt.Errorf("start %s again: %w", subject, err))
	}
	return nil
}

// rollback restores the safety backup, removes the volumes the restore created, and deploys the target if it ran; a
// target whose safety backup could not be restored is left down rather than started on half-restored data, and one
// that could not be stopped is left as is. A new target is discarded.
func (r RestoreSteps) rollback(ctx context.Context, state RestoreState, created []string, cause error) error {
	ctx = context.WithoutCancel(ctx)
	subject := r.subject()
	if r.Discard != nil {
		leftovers := r.removeAll(ctx, created)
		discarded, left := r.Discard(ctx)
		leftovers = append(leftovers, left...)
		switch {
		case len(leftovers) > 0:
			return fmt.Errorf("%w; could not remove %s", cause, strings.Join(leftovers, ", "))
		case !discarded && len(created) == 0:
			return cause
		}
		return fmt.Errorf("%w; the new project and its volumes were removed", cause)
	}
	if state.SafetyID == "" {
		// Without a safety backup, only volumes this restore created can have been written; removing them undoes it.
		leftovers := r.removeAll(ctx, created)
		if state.Running {
			if err := r.Deploy(ctx); err != nil {
				leftovers = append(leftovers, fmt.Sprintf("%s could not be started again (%v)", subject, err))
			}
		}
		if len(leftovers) > 0 {
			return fmt.Errorf("%w; nothing was restored, but %s", cause, strings.Join(leftovers, ", "))
		}
		return fmt.Errorf("%w; nothing was restored", cause)
	}
	r.report(ctx, "Rolling back to the safety backup")
	if err := r.Stop(ctx, state); err != nil {
		return fmt.Errorf("%w; %s could not be stopped for the rollback (%w), so it was left as is; restore the safety backup %s by hand to undo the restore", cause, subject, err, state.SafetyID)
	}
	undoErr := r.Write(ctx, state.SafetyID)
	removeErr := r.Remove(ctx, created)
	if undoErr != nil {
		return fmt.Errorf("%w; the rollback failed too (%w), so %s was left down; restore the safety backup %s to undo the restore", cause, errors.Join(undoErr, removeErr), subject, state.SafetyID)
	}
	var deployErr error
	if state.Running {
		deployErr = r.Deploy(ctx)
	}
	if failed := errors.Join(removeErr, deployErr); failed != nil {
		return fmt.Errorf("%w; %s was rolled back to the safety backup %s, but the rollback hit errors: %w", cause, subject, state.SafetyID, failed)
	}
	return fmt.Errorf("%w; %s was rolled back to the safety backup %s", cause, subject, state.SafetyID)
}

// removeAll removes the volumes the restore created one by one and names those it could not remove.
func (r RestoreSteps) removeAll(ctx context.Context, created []string) []string {
	var leftovers []string
	for _, name := range created {
		if err := r.Remove(ctx, []string{name}); err != nil {
			leftovers = append(leftovers, fmt.Sprintf("volume %s (%v)", name, err))
		}
	}
	return leftovers
}

func (r RestoreSteps) subject() string { return cmp.Or(r.Subject, "the project") }

func (r RestoreSteps) report(ctx context.Context, message string) {
	if r.Report != nil {
		r.Report(ctx, message)
	}
}

// RestoreLayout mounts the targets under /dest as the capture laid the parts out under SnapshotRoot and restores the
// capture's root, so each target root keeps its owner and mode; globs skip the rest, and /dest holds only targets, so
// --delete is safe. projectMounts, when set, restore project/; captured names every volume in the capture.
func RestoreLayout(projectMounts []mount.Mount, parts []RestorePart, captured []string) (mount.Mount, RestoreOptions, error) {
	mounts := slices.Clone(projectMounts)
	globs := []string{"!/meta", "!/meta/**"}
	if len(projectMounts) == 0 {
		globs = append(globs, "!/project", "!/project/**")
	}
	restored := map[string]string{}
	for _, part := range parts {
		into, seen := restored[part.Snapshot]
		switch {
		case !slices.Contains(captured, part.Snapshot):
			return mount.Mount{}, RestoreOptions{}, common.Classify(common.ErrValidation, fmt.Errorf("the backup holds no data for volume %s", part.Key))
		case seen && into != part.Target:
			return mount.Mount{}, RestoreOptions{}, common.Classify(common.ErrValidation, fmt.Errorf("the backup's volume %s cannot be restored into both %s and %s", part.Snapshot, into, part.Target))
		case !seen && slices.Contains(slices.Collect(maps.Values(restored)), part.Target):
			return mount.Mount{}, RestoreOptions{}, common.Classify(common.ErrValidation, fmt.Errorf("two of the backup's volumes would be restored into %s", part.Target))
		case seen:
			continue
		}
		restored[part.Snapshot] = part.Target
		mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: part.Target, Target: path.Join(RestoreRoot, "volumes", part.Snapshot)})
	}
	for _, name := range captured {
		if _, ok := restored[name]; !ok {
			globs = append(globs, "!/volumes/"+name, "!/volumes/"+name+"/**")
		}
	}
	if len(mounts) == 0 {
		return mount.Mount{}, RestoreOptions{}, common.Classify(common.ErrValidation, errors.New("nothing to restore"))
	}
	return mounts[0], RestoreOptions{DestinationPath: RestoreRoot, ExtraMounts: mounts[1:], Globs: globs, DeleteExtra: true}, nil
}
