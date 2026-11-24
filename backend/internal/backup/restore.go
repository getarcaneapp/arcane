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
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
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
		// Docker creates the volumes the restore is missing.
		Docker *dockerInternal.DockerClientService
		// DeleteVolume removes a volume the restore created, as User.
		DeleteVolume func(ctx context.Context, name string, user types.Actor) error
		Report       func(ctx context.Context, message string)
		// Hold waits for the repository the restore and its safety backup use, before the target stops, so the target
		// stays stopped only for this restore's own copies; nil when nothing needs waiting for.
		Hold   func(ctx context.Context) (context.Context, func(), error)
		Stop   func(ctx context.Context, state RestoreState) error
		Deploy func(ctx context.Context) error
		Safety func(ctx context.Context) (string, error)
		InUse  func(ctx context.Context) error
		// Write restores the backup or, given a safety backup's ID, that safety backup.
		Write func(ctx context.Context, safetyID string) error
		// Written names the volumes Write created itself, when it creates them, so a failure removes them too.
		Written func() []string
		Verify  func(ctx context.Context) error
		// Discard removes the new target a failed restore leaves behind, reporting whether there was one and naming what
		// it could not remove.
		Discard func(ctx context.Context) (bool, []string)
		User    types.Actor
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
	held, release, err := r.hold(ctx)
	if err != nil {
		return err
	}
	defer release()
	if r.Stop != nil {
		if state.Running {
			r.report(held, "Stopping the project")
		}
		// A stopped container can be started on its own, without the project lock the restore holds, so any running one
		// is stopped.
		if err = r.Stop(held, state); err != nil {
			return r.Rollback(held, state, nil, fmt.Errorf("stop the project: %w", err))
		}
	}
	if r.Safety != nil {
		r.report(held, "Taking a safety backup")
		var safetyID string
		if safetyID, err = r.Safety(held); err != nil {
			return r.Rollback(held, state, nil, fmt.Errorf("take a safety backup: %w", err))
		}
		// A container started during the safety backup could be using a volume now; nothing has been written yet, so
		// the rollback leaves the volumes alone.
		if r.InUse != nil {
			if err = r.InUse(held); err != nil {
				return r.Rollback(held, state, nil, err)
			}
		}
		state.SafetyID = safetyID
	}
	r.report(held, "Restoring the backup")
	// Only the volumes this restore created are removed on rollback.
	var created []string
	for _, options := range state.Create {
		if err = CreateVolume(held, r.Docker, options); err != nil {
			return r.Rollback(held, state, created, fmt.Errorf("create the missing volumes: %w", err))
		}
		created = append(created, options.Name)
	}
	err = r.Write(held, "")
	if r.Written != nil {
		created = append(created, r.Written()...)
	}
	if err == nil && r.Verify != nil {
		err = r.Verify(held)
	}
	if err != nil {
		return r.Rollback(held, state, created, err)
	}
	release()
	// Deployed when asked, by default when it ran before the restore.
	if !*cmp.Or(state.Redeploy, new(state.Running)) {
		return nil
	}
	r.report(ctx, "Deploying the project")
	// The project the restore deploys is its own to stop again on rollback.
	state.MayStop = true
	if err = r.Deploy(ctx); err != nil {
		return r.Rollback(ctx, state, created, fmt.Errorf("deploy the restored project: %w", err))
	}
	return nil
}

// Rollback restores the safety backup, removes the volumes the restore created, and deploys the project if it ran; a
// project whose safety backup could not be restored is left down rather than started on half-restored data, and one
// that could not be stopped is left as is. A new target is discarded. Callers use it for failures before Apply.
func (r RestoreSteps) Rollback(ctx context.Context, state RestoreState, created []string, cause error) error {
	ctx = context.WithoutCancel(ctx)
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
		if state.Running {
			if err := r.Deploy(ctx); err != nil {
				return fmt.Errorf("%w; nothing was restored, but the project could not be deployed again: %w", cause, err)
			}
		}
		return fmt.Errorf("%w; nothing was restored", cause)
	}
	r.report(ctx, "Rolling back to the safety backup")
	// As in Apply, the repository is waited for before the project stops; a caller holding it keeps it.
	held, release, err := r.hold(ctx)
	if err != nil {
		return fmt.Errorf("%w; the rollback could not start (%w); restore the safety backup %s by hand to undo the restore", cause, err, state.SafetyID)
	}
	defer release()
	if err = r.Stop(held, state); err != nil {
		return fmt.Errorf("%w; the project could not be stopped for the rollback (%w), so it was left as is; restore the safety backup %s by hand to undo the restore", cause, err, state.SafetyID)
	}
	undoErr := r.Write(held, state.SafetyID)
	var removeErr error
	for _, name := range created {
		removeErr = errors.Join(removeErr, r.DeleteVolume(held, name, r.User))
	}
	release()
	if undoErr != nil {
		return fmt.Errorf("%w; the rollback failed too (%w), so the project was left down; restore the safety backup %s to undo the restore", cause, errors.Join(undoErr, removeErr), state.SafetyID)
	}
	var deployErr error
	if state.Running {
		deployErr = r.Deploy(ctx)
	}
	if failed := errors.Join(removeErr, deployErr); failed != nil {
		return fmt.Errorf("%w; the project was rolled back to the safety backup %s, but the rollback hit errors: %w", cause, state.SafetyID, failed)
	}
	return fmt.Errorf("%w; the project was rolled back to the safety backup %s", cause, state.SafetyID)
}

// hold takes Hold, or passes the context through when the restore waits for nothing.
func (r RestoreSteps) hold(ctx context.Context) (context.Context, func(), error) {
	if r.Hold == nil {
		return ctx, func() {}, nil
	}
	return r.Hold(ctx)
}

// removeAll removes the volumes the restore created one by one and names those it could not remove.
func (r RestoreSteps) removeAll(ctx context.Context, created []string) []string {
	var leftovers []string
	for _, name := range created {
		if err := r.DeleteVolume(ctx, name, r.User); err != nil {
			leftovers = append(leftovers, fmt.Sprintf("volume %s (%v)", name, err))
		}
	}
	return leftovers
}

// CreateVolume creates a volume a restore writes into, refusing one that exists, within Docker's API timeout.
func CreateVolume(ctx context.Context, dockerService *dockerInternal.DockerClientService, options client.VolumeCreateOptions) error {
	_, err := dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (struct{}, error) {
		return struct{}{}, projects.CreateNewVolume(ctx, dockerClient, options)
	})
	return err
}

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
