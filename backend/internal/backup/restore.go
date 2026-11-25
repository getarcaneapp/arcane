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

	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/common"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	RestoreState struct {
		Redeploy *bool                        `json:"redeploy,omitempty"`
		SafetyID string                       `json:"safetyId,omitempty"`
		Create   []client.VolumeCreateOptions `json:"create,omitempty"`
		Created  []string                     `json:"created,omitempty"`
		MayStop  bool                         `json:"mayStop"`
		Running  bool                         `json:"running"`
	}

	RestoreSteps struct {
		Docker       *dockerInternal.DockerClientService
		DeleteVolume func(ctx context.Context, name string, user types.Actor) error
		Report       func(ctx context.Context, message string)
		Record       func(ctx context.Context, state RestoreState) error
		Hold         func(ctx context.Context) (context.Context, func(), error)
		Stop         func(ctx context.Context, state RestoreState) error
		Deploy       func(ctx context.Context) error
		Safety       func(ctx context.Context) (string, error)
		InUse        func(ctx context.Context) error
		Write        func(ctx context.Context, safetyID string) error
		Written      func() []string
		Verify       func(ctx context.Context) error
		Discard      func(ctx context.Context) (bool, []string)
		Subject      string
		User         types.Actor
	}

	UnchangedError struct {
		Err error
	}

	RestorePart struct {
		Key      string
		Snapshot string
		Target   string
	}
)

const RestoreRoot = "/dest"

func (r RestoreSteps) Apply(ctx context.Context, state RestoreState) error {
	held, release, err := r.hold(ctx)
	if err != nil {
		return err
	}
	defer release()
	subject := cmp.Or(r.Subject, "the project")
	if r.Stop != nil {
		if state.Running {
			r.report(held, "Stopping "+subject)
		}
		if err = r.Stop(held, state); err != nil {
			return r.Rollback(held, state, fmt.Errorf("stop %s: %w", subject, err))
		}
	}
	if r.Safety != nil {
		r.report(held, "Taking a safety backup")
		var safetyID string
		if safetyID, err = r.Safety(held); err != nil {
			return r.Rollback(held, state, fmt.Errorf("take a safety backup: %w", err))
		}
		if r.InUse != nil {
			if err = r.InUse(held); err != nil {
				return r.Rollback(held, state, err)
			}
		}
		state.SafetyID = safetyID
		if err = r.record(held, state); err != nil {
			return r.Rollback(held, state, err)
		}
	}
	r.report(held, "Restoring the backup")
	for _, options := range state.Create {
		if _, err = r.Docker.Call(held, func(ctx context.Context, dockerClient *client.Client) (struct{}, error) {
			return struct{}{}, projects.CreateNewVolume(ctx, dockerClient, options)
		}); err != nil {
			return r.Rollback(held, state, fmt.Errorf("create the missing volumes: %w", err))
		}
		state.Created = append(state.Created, options.Name)
		if err = r.record(held, state); err != nil {
			return r.Rollback(held, state, err)
		}
	}
	err = r.Write(held, "")
	if r.Written != nil {
		state.Created = append(state.Created, r.Written()...)
	}
	if err == nil && r.Verify != nil {
		err = r.Verify(held)
	}
	if unchanged := (UnchangedError{}); errors.As(err, &unchanged) {
		state.SafetyID = ""
	}
	if err != nil {
		return r.Rollback(held, state, err)
	}
	release()
	if !*cmp.Or(state.Redeploy, new(state.Running)) {
		return nil
	}
	r.report(ctx, "Deploying "+subject)
	state.MayStop = true
	if err = r.record(ctx, state); err != nil {
		return r.Rollback(ctx, state, err)
	}
	if err = r.Deploy(ctx); err != nil {
		return r.Rollback(ctx, state, fmt.Errorf("start %s again: %w", subject, err))
	}
	return nil
}

func (r RestoreSteps) Rollback(ctx context.Context, state RestoreState, cause error) error {
	ctx = context.WithoutCancel(ctx)
	subject := cmp.Or(r.Subject, "the project")
	if r.Discard != nil {
		leftovers := r.removeAll(ctx, state.Created)
		discarded, left := r.Discard(ctx)
		leftovers = append(leftovers, left...)
		switch {
		case len(leftovers) > 0:
			return fmt.Errorf("%w; could not remove %s", cause, strings.Join(leftovers, ", "))
		case !discarded && len(state.Created) == 0:
			return cause
		}
		return fmt.Errorf("%w; the new project and its volumes were removed", cause)
	}
	if state.SafetyID == "" {
		leftovers := r.removeAll(ctx, state.Created)
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
	held, release, err := r.hold(ctx)
	if err != nil {
		return fmt.Errorf("%w; the rollback could not start (%w); restore the safety backup %s by hand to undo the restore", cause, err, state.SafetyID)
	}
	defer release()
	if err = r.Stop(held, state); err != nil {
		return fmt.Errorf("%w; %s could not be stopped for the rollback (%w), so it was left as is; restore the safety backup %s by hand to undo the restore", cause, subject, err, state.SafetyID)
	}
	if r.InUse != nil {
		if err = r.InUse(held); err != nil {
			return fmt.Errorf("%w; the rollback was not written because %w; restore the safety backup %s by hand to undo the restore", cause, err, state.SafetyID)
		}
	}
	undoErr := r.Write(held, state.SafetyID)
	var removeErr error
	if leftovers := r.removeAll(held, state.Created); len(leftovers) > 0 {
		removeErr = fmt.Errorf("could not remove %s", strings.Join(leftovers, ", "))
	}
	release()
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

func (r RestoreSteps) hold(ctx context.Context) (context.Context, func(), error) {
	if r.Hold == nil {
		return ctx, func() {}, nil
	}
	return r.Hold(ctx)
}

func (r RestoreSteps) removeAll(ctx context.Context, created []string) []string {
	if len(created) == 0 {
		return nil
	}
	listed, listErr := r.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
		return dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	})
	if listErr != nil {
		return []string{fmt.Sprintf("volumes %s (%v)", strings.Join(created, ", "), listErr)}
	}
	var users []string
	for _, name := range created {
		users = append(users, docker.FilterContainersUsingVolume(listed.Items, name)...)
	}
	var leftovers []string
	for _, id := range slices.Compact(slices.Sorted(slices.Values(users))) {
		if _, removeErr := r.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		}); removeErr != nil {
			leftovers = append(leftovers, fmt.Sprintf("container %s (%v)", id, removeErr))
		}
	}
	for _, name := range created {
		if err := r.DeleteVolume(ctx, name, r.User); err != nil {
			leftovers = append(leftovers, fmt.Sprintf("volume %s (%v)", name, err))
		}
	}
	return leftovers
}

func (e UnchangedError) Error() string { return e.Err.Error() }

func (e UnchangedError) Unwrap() error { return e.Err }

func (r RestoreSteps) record(ctx context.Context, state RestoreState) error {
	if r.Record == nil {
		return nil
	}
	return r.Record(ctx, state)
}

func (r RestoreSteps) report(ctx context.Context, message string) {
	if r.Report != nil {
		r.Report(ctx, message)
	}
}

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
