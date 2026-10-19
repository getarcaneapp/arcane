package recovery

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/refs"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
)

// errNoFrozenPlan reports a run without a persisted frozen plan.
var errNoFrozenPlan = errors.New("interrupted update has no frozen target plan")

// Service freezes update targets before any pull, pins pulls to the frozen
// digests, and confirms or resumes interrupted updates.
type Service struct {
	dockerService  *docker.DockerClientService
	images         remoteDigester
	pendingUpdates func(ctx context.Context) ([]updater.ImageUpdateRecord, error)
	activityID     func(ctx context.Context) string
	acquireUpdate  func(ctx context.Context) (context.Context, func(), error)
	applyPending   func(ctx context.Context, options arcaneupdater.Options) (*arcaneupdater.Result, error)
}

// remoteDigester resolves registry digests; the image service implements it.
type remoteDigester interface {
	RemoteDigest(ctx context.Context, imageRef string, credentials []containerregistry.Credential) (string, error)
}

type (
	frozenPendingKey struct{}
	frozenSingleKey  struct{}
	frozenUpdatePlan struct {
		Records []updater.ImageUpdateRecord
		Targets []arcaneupdater.FrozenUpdateTarget
	}
)

type frozenSingle struct {
	Target  *arcaneupdater.FrozenUpdateTarget
	Persist func() error
}

func NewService(
	dockerService *docker.DockerClientService,
	images remoteDigester,
	pendingUpdates func(ctx context.Context) ([]updater.ImageUpdateRecord, error),
	activityID func(ctx context.Context) string,
	acquireUpdate func(ctx context.Context) (context.Context, func(), error),
	applyPending func(ctx context.Context, options arcaneupdater.Options) (*arcaneupdater.Result, error),
) *Service {
	return &Service{
		dockerService:  dockerService,
		images:         images,
		pendingUpdates: pendingUpdates,
		activityID:     activityID,
		acquireUpdate:  acquireUpdate,
		applyPending:   applyPending,
	}
}

// FrozenRecords returns the frozen batch records carried by ctx, if any.
func (s *Service) FrozenRecords(ctx context.Context) ([]updater.ImageUpdateRecord, bool) {
	plan, ok := ctx.Value(frozenPendingKey{}).(*frozenUpdatePlan)
	if !ok {
		return nil, false
	}
	return slices.Clone(plan.Records), true
}

// WithSingleTarget scopes pulls in ctx to one accepted container update.
func (s *Service) WithSingleTarget(ctx context.Context, target *arcaneupdater.FrozenUpdateTarget, persist func() error) context.Context {
	return context.WithValue(ctx, frozenSingleKey{}, &frozenSingle{Target: target, Persist: persist})
}

// VerifyResult confirms a reported success against the frozen batch plan.
func (s *Service) VerifyResult(ctx context.Context, resourceID string) error {
	plan, ok := ctx.Value(frozenPendingKey{}).(*frozenUpdatePlan)
	if !ok {
		return nil
	}
	index := slices.IndexFunc(plan.Targets, func(target arcaneupdater.FrozenUpdateTarget) bool { return target.ContainerID == resourceID })
	if index < 0 {
		return nil
	}
	confirmed, _, err := s.ConfirmTarget(ctx, plan.Targets[index])
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("updater result does not confirm the frozen desired image")
	}
	return nil
}

// FreezePending persists identities and desired images before any pull. A run
// that already holds a plan keeps it. Image refs and container IDs in excluded are left
// out, and a target that cannot be frozen fails alone instead of aborting the batch.
// Candidates that deferred reports are skipped without failing, so a later run picks them up.
func (s *Service) FreezePending(ctx context.Context, run scheduler.Run, excluded map[string]bool, deferred func(updater.ImageUpdateRecord, container.Summary) bool) (context.Context, error) {
	if _, ok := ctx.Value(frozenPendingKey{}).(*frozenUpdatePlan); ok {
		return ctx, nil
	}
	if plan, found, err := frozenPlan(run); err != nil || found {
		return context.WithValue(ctx, frozenPendingKey{}, &plan), err
	}
	records, err := s.pendingUpdates(ctx)
	if err != nil {
		return ctx, err
	}
	records = slices.DeleteFunc(records, func(record updater.ImageUpdateRecord) bool {
		return !record.HasUpdate || excluded[refs.NormalizeImageUpdateRef(record.ImageRef())] || excluded[record.ContainerID]
	})
	failures := map[string]error{}
	digests := map[string]string{}
	selected := make([]updater.ImageUpdateRecord, 0, len(records))
	var targets []arcaneupdater.FrozenUpdateTarget
	if len(records) > 0 {
		listed, listErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
			return dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
		})
		if listErr != nil {
			return ctx, listErr
		}
		// Resolve each candidate's configured image once, not per record.
		candidateRefs := make(map[string]string, len(listed.Items))
		for _, candidate := range listed.Items {
			configured, _ := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (string, error) {
				return imageref.ConfiguredImage(ctx, dockerClient, candidate), nil
			})
			candidateRefs[candidate.ID] = refs.NormalizeImageUpdateRef(configured)
		}
		for _, record := range records {
			for _, candidate := range listed.Items {
				if excluded[candidate.ID] || (record.ContainerID != "" && record.ContainerID != candidate.ID) {
					continue
				}
				if record.ContainerID == "" && candidateRefs[candidate.ID] != refs.NormalizeImageUpdateRef(record.ImageRef()) {
					continue
				}
				if deferred(record, candidate) {
					continue
				}
				target, frozenRecord, freezeRecordTargetErr := s.freezeRecordTarget(ctx, record, candidate.ID, digests)
				if freezeRecordTargetErr != nil {
					failures[candidate.ID] = freezeRecordTargetErr
					continue
				}
				selected = append(selected, frozenRecord)
				targets = append(targets, *target)
			}
		}
	}
	plan := frozenUpdatePlan{Records: selected, Targets: targets}
	raw, err := json.Marshal(plan)
	if err != nil {
		return ctx, err
	}
	activityID := s.activityID(ctx)
	// Failures are recorded before the plan, since a delivery that finds the plan never plans again.
	for containerID, freezeErr := range failures {
		if failedProgressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
			ID:           containerID,
			ResourceType: "container",
			Status:       scheduler.Failed,
			Message:      "Update could not be planned: " + freezeErr.Error(),
			ActivityID:   activityID,
		}); failedProgressErr != nil {
			return ctx, failedProgressErr
		}
	}
	if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
		ID:           "auto-update",
		ResourceType: "update-batch",
		Status:       scheduler.Running,
		RecoveryData: raw,
		ActivityID:   activityID,
	}); progressErr != nil {
		return ctx, progressErr
	}
	for _, target := range plan.Targets {
		if queuedProgressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
			ID:           target.ContainerID,
			ResourceType: "container",
			Status:       scheduler.Queued,
		}); queuedProgressErr != nil {
			return ctx, queuedProgressErr
		}
	}
	return context.WithValue(ctx, frozenPendingKey{}, &plan), nil
}

func (s *Service) freezeRecordTarget(
	ctx context.Context,
	record updater.ImageUpdateRecord,
	containerID string,
	digests map[string]string,
) (*arcaneupdater.FrozenUpdateTarget, updater.ImageUpdateRecord, error) {
	frozen, err := s.FreezeContainer(ctx, containerID)
	if err != nil {
		return nil, record, err
	}
	digest, err := func() (string, error) {
		if pinned := kit.Ternary(record.IsTagUpdate(), "", kit.FromPtr(record.LatestDigest)); pinned != "" {
			return pinned, nil
		}
		if resolved, ok := digests[record.NewImageRef()]; ok {
			return resolved, nil
		}
		return s.images.RemoteDigest(ctx, record.NewImageRef(), nil)
	}()
	if err != nil {
		return nil, record, err
	}
	if digest == "" {
		return nil, record, errors.New("registry returned no desired digest")
	}
	digests[record.NewImageRef()] = digest
	return &arcaneupdater.FrozenUpdateTarget{ //exhaustruct:enforce
			ContainerID:          frozen.ContainerID,
			ContainerName:        frozen.ContainerName,
			ComposeProject:       frozen.ComposeProject,
			ComposeService:       frozen.ComposeService,
			ComposeNumber:        frozen.ComposeNumber,
			BaselineImageID:      frozen.BaselineImageID,
			BaselineStartedAt:    frozen.BaselineStartedAt,
			BaselineRestartCount: frozen.BaselineRestartCount,
			BaselineRunning:      frozen.BaselineRunning,
			DesiredImageRef:      record.NewImageRef(),
			DesiredDigest:        digest,
		},
		updater.ImageUpdateRecord{ //exhaustruct:enforce
			ContainerID:    containerID,
			ID:             record.ID,
			Repository:     record.Repository,
			Tag:            record.Tag,
			HasUpdate:      record.HasUpdate,
			UpdateType:     record.UpdateType,
			CurrentVersion: record.CurrentVersion,
			LatestVersion:  record.LatestVersion,
			CurrentDigest:  record.CurrentDigest,
			LatestDigest:   &digest,
			CheckTime:      record.CheckTime,
			LastError:      record.LastError,
		}, nil
}

// FreezeContainer captures the identity evidence of one container.
func (s *Service) FreezeContainer(ctx context.Context, id string) (*arcaneupdater.FrozenUpdateTarget, error) {
	result, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, id, client.ContainerInspectOptions{})
	})
	if err != nil {
		return nil, err
	}
	return &arcaneupdater.FrozenUpdateTarget{
		ContainerID:          result.Container.ID,
		ContainerName:        strings.TrimPrefix(result.Container.Name, "/"),
		ComposeProject:       kit.FromPtr(result.Container.Config).Labels["com.docker.compose.project"],
		ComposeService:       kit.FromPtr(result.Container.Config).Labels["com.docker.compose.service"],
		ComposeNumber:        kit.FromPtr(result.Container.Config).Labels["com.docker.compose.container-number"],
		BaselineImageID:      result.Container.Image,
		BaselineStartedAt:    kit.FromPtr(result.Container.State).StartedAt,
		BaselineRestartCount: result.Container.RestartCount,
		BaselineRunning:      kit.FromPtr(result.Container.State).Running,
	}, nil
}

func (s *Service) ResumeTarget(ctx context.Context, target arcaneupdater.FrozenUpdateTarget) (arcaneupdater.FrozenUpdateTarget, bool, error) {
	current, err := s.FreezeContainer(ctx, target.ContainerID)
	if err != nil {
		return target, false, err
	}
	if !target.BaselineRunning || current.BaselineRunning {
		return target, false, nil
	}
	if _, startErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, target.ContainerID, client.ContainerStartOptions{})
	}); startErr != nil {
		return target, false, startErr
	}
	started, err := s.FreezeContainer(ctx, target.ContainerID)
	if err != nil {
		return target, false, err
	}
	return arcaneupdater.FrozenUpdateTarget{ //exhaustruct:enforce
		ContainerID:          target.ContainerID,
		ContainerName:        target.ContainerName,
		ComposeProject:       target.ComposeProject,
		ComposeService:       target.ComposeService,
		ComposeNumber:        target.ComposeNumber,
		BaselineImageID:      target.BaselineImageID,
		BaselineStartedAt:    started.BaselineStartedAt,
		BaselineRestartCount: started.BaselineRestartCount,
		BaselineRunning:      target.BaselineRunning,
		DesiredImageRef:      target.DesiredImageRef,
		DesiredDigest:        target.DesiredDigest,
	}, true, nil
}

// PreparePull commits the exact image before the engine changes Docker.
func (s *Service) PreparePull(ctx context.Context, imageRef string) (string, error) {
	if single, ok := ctx.Value(frozenSingleKey{}).(*frozenSingle); ok {
		if single.Target.DesiredImageRef == "" {
			digest, err := s.images.RemoteDigest(ctx, imageRef, nil)
			if err != nil {
				return "", err
			}
			*single.Target = arcaneupdater.FrozenUpdateTarget{ //exhaustruct:enforce
				ContainerID:          single.Target.ContainerID,
				ContainerName:        single.Target.ContainerName,
				ComposeProject:       single.Target.ComposeProject,
				ComposeService:       single.Target.ComposeService,
				ComposeNumber:        single.Target.ComposeNumber,
				BaselineImageID:      single.Target.BaselineImageID,
				BaselineStartedAt:    single.Target.BaselineStartedAt,
				BaselineRestartCount: single.Target.BaselineRestartCount,
				BaselineRunning:      single.Target.BaselineRunning,
				DesiredImageRef:      imageRef,
				DesiredDigest:        digest,
			}
			if persistErr := single.Persist(); persistErr != nil {
				return "", persistErr
			}
		}
		if refs.NormalizeImageUpdateRef(single.Target.DesiredImageRef) != refs.NormalizeImageUpdateRef(imageRef) {
			return "", errors.New("selected image changed after the update was accepted")
		}
		return immutableImage(*single.Target)
	}
	plan, ok := ctx.Value(frozenPendingKey{}).(*frozenUpdatePlan)
	if !ok {
		return imageRef, nil
	}
	immutable := ""
	for _, target := range plan.Targets {
		if refs.NormalizeImageUpdateRef(target.DesiredImageRef) != refs.NormalizeImageUpdateRef(imageRef) {
			continue
		}
		selected, err := immutableImage(target)
		if err != nil {
			return "", err
		}
		if immutable != "" && immutable != selected {
			return "", errors.New("conflicting frozen digests for one image reference")
		}
		immutable = selected
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
			ID:           target.ContainerID,
			ResourceType: "container",
			Status:       scheduler.Running,
		}); progressErr != nil {
			return "", progressErr
		}
	}
	if immutable == "" {
		return "", errors.New("updater attempted to pull an image outside its frozen plan")
	}
	return immutable, nil
}

// ConfirmTarget returns whether the desired effect is confirmed,
// or whether the exact original container is unchanged and safe to resume.
func (s *Service) ConfirmTarget(ctx context.Context, target arcaneupdater.FrozenUpdateTarget) (bool, bool, error) {
	listed, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
		return dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	})
	if err != nil {
		return false, false, err
	}
	desiredID := func() string {
		immutable, imageErr := immutableImage(target)
		if imageErr != nil {
			return ""
		}
		inspected, imageInspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
			return dockerClient.ImageInspect(ctx, immutable)
		})
		if imageInspectErr != nil {
			return ""
		}
		return inspected.ID
	}()
	for _, candidate := range listed.Items {
		if !slices.ContainsFunc(candidate.Names, func(name string) bool { return strings.TrimPrefix(name, "/") == target.ContainerName }) {
			continue
		}
		result, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, dockerClient, candidate.ID, client.ContainerInspectOptions{})
		})
		if inspectErr != nil {
			return false, false, inspectErr
		}
		if result.Container.Config == nil {
			return false, false, nil
		}
		if result.Container.Config.Labels["com.docker.compose.project"] != target.ComposeProject ||
			result.Container.Config.Labels["com.docker.compose.service"] != target.ComposeService ||
			result.Container.Config.Labels["com.docker.compose.container-number"] != target.ComposeNumber {
			return false, false, nil
		}
		if desiredID != "" && result.Container.Image == desiredID {
			switch {
			case !target.BaselineRunning || kit.FromPtr(result.Container.State).Running:
				return true, false, nil
			case result.Container.ID != target.ContainerID && kit.FromPtr(result.Container.State).Status == container.StateCreated:
				if _, startErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
					return dockerClient.ContainerStart(ctx, result.Container.ID, client.ContainerStartOptions{})
				}); startErr != nil {
					return false, false, startErr
				}
				return true, false, nil
			}
		}
		unchanged := result.Container.ID == target.ContainerID &&
			result.Container.Image == target.BaselineImageID &&
			result.Container.RestartCount == target.BaselineRestartCount &&
			result.Container.State != nil &&
			result.Container.State.StartedAt == target.BaselineStartedAt
		return false, unchanged, nil
	}
	return false, false, nil
}

// ReconcilePending resumes only frozen targets whose original container is unchanged.
func (s *Service) ReconcilePending(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
	ctx, release, err := s.acquireUpdate(ctx)
	if err != nil {
		return scheduler.Outcome{Status: scheduler.Waiting}, err
	}
	defer release()
	ctx, remaining, unresolved, err := s.ResumePlan(ctx, run)
	if errors.Is(err, errNoFrozenPlan) {
		return scheduler.Outcome{
			Status:  scheduler.Failed,
			Message: "Interrupted update has no frozen target plan",
			Targets: run.Outcome.Targets,
		}, nil
	}
	if err != nil {
		return scheduler.Outcome{Status: scheduler.Waiting}, err
	}
	if remaining > 0 {
		result, applyPendingErr := s.applyPending(ctx, arcaneupdater.Options{})
		if applyPendingErr != nil || result == nil || result.Failed > 0 {
			return scheduler.Outcome{Status: scheduler.Failed, Message: "Frozen update recovery could not confirm completion"}, applyPendingErr
		}
	}
	// Containers that could not be planned were never updated, so recovery reports them as finalize does.
	unplanned := s.Unplanned(run)
	switch {
	case unresolved:
		return scheduler.Outcome{
			Status:  scheduler.Failed,
			Message: "Some update effects could not be confirmed",
			Targets: unplanned,
		}, nil
	case len(unplanned) > 0:
		return scheduler.Outcome{
			Status:  scheduler.Partial,
			Message: "Some updates failed",
			Targets: unplanned,
		}, nil
	}
	return scheduler.Outcome{
		Status:  scheduler.Succeeded,
		Message: "Frozen update targets confirmed",
		Targets: unplanned,
	}, nil
}

// ResumePlan confirms each unsettled frozen target before anything repeats and
// scopes ctx to the targets still to apply. Targets that changed without
// reaching the desired image are marked for review and reported as unresolved.
func (s *Service) ResumePlan(ctx context.Context, run scheduler.Run) (context.Context, int, bool, error) {
	plan, found, err := frozenPlan(run)
	if err != nil {
		return ctx, 0, false, err
	}
	if !found {
		return ctx, 0, false, errNoFrozenPlan
	}
	var (
		targets    []arcaneupdater.FrozenUpdateTarget
		records    []updater.ImageUpdateRecord
		unresolved bool
	)
	for position, target := range plan.Targets {
		if slices.ContainsFunc(run.Outcome.Targets, func(outcome scheduler.TargetOutcome) bool {
			return outcome.ID == target.ContainerID && (outcome.Status == scheduler.Succeeded || outcome.Status == scheduler.Skipped)
		}) {
			continue
		}
		confirmed, unchanged, confirmErr := s.ConfirmTarget(ctx, target)
		if confirmErr != nil {
			return ctx, 0, false, confirmErr
		}
		if !confirmed && unchanged {
			resumed, started, resumeErr := s.ResumeTarget(ctx, target)
			if resumeErr != nil {
				return ctx, 0, false, resumeErr
			}
			plan.Targets[position] = resumed
			if started {
				raw, marshalErr := json.Marshal(plan)
				if marshalErr != nil {
					return ctx, 0, false, marshalErr
				}
				if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
					ID:           "auto-update",
					ResourceType: "update-batch",
					Status:       scheduler.Running,
					RecoveryData: raw,
					ActivityID:   s.activityID(ctx),
				}); progressErr != nil {
					return ctx, 0, false, progressErr
				}
			}
			targets = append(targets, resumed)
			if index := slices.IndexFunc(plan.Records, func(record updater.ImageUpdateRecord) bool { return record.ContainerID == target.ContainerID }); index >= 0 {
				records = append(records, plan.Records[index])
			}
			continue
		}
		unresolved = unresolved || !confirmed
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{
			ID:           target.ContainerID,
			ResourceType: "container",
			Status:       kit.Ternary(confirmed, scheduler.Succeeded, scheduler.NeedsAttention),
			Message: kit.Ternary(
				confirmed,
				"Frozen desired image confirmed",
				"Update effect could not be confirmed: recreate "+target.ContainerName+" from "+target.DesiredImageRef+" (previous image "+target.BaselineImageID+")",
			),
		}); progressErr != nil {
			return ctx, 0, false, progressErr
		}
	}
	return context.WithValue(ctx, frozenPendingKey{}, &frozenUpdatePlan{Records: records, Targets: targets}), len(records), unresolved, nil
}

// Planned reports whether the run already holds a frozen plan, as a retry does.
func (s *Service) Planned(run scheduler.Run) bool {
	_, found, err := frozenPlan(run)
	return found && err == nil
}

// Started reports whether applying the frozen plan began: a planned container moved past queued.
func (s *Service) Started(run scheduler.Run) bool {
	plan, found, err := frozenPlan(run)
	if err != nil || !found {
		return false
	}
	return slices.ContainsFunc(plan.Targets, func(planned arcaneupdater.FrozenUpdateTarget) bool {
		return slices.ContainsFunc(run.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
			return target.ID == planned.ContainerID && target.Status != scheduler.Queued
		})
	})
}

// Unsettled counts the frozen targets a retry would still resume.
func (s *Service) Unsettled(run scheduler.Run) (int, error) {
	plan, found, err := frozenPlan(run)
	if err != nil || !found {
		return 0, cmp.Or(err, errNoFrozenPlan)
	}
	unsettled := 0
	for _, target := range plan.Targets {
		if !slices.ContainsFunc(run.Outcome.Targets, func(outcome scheduler.TargetOutcome) bool {
			return outcome.ID == target.ContainerID && (outcome.Status == scheduler.Succeeded || outcome.Status == scheduler.Skipped)
		}) {
			unsettled++
		}
	}
	return unsettled, nil
}

// Unplanned lists the images and containers that never reached the frozen plan: failed checks and failed freezes.
func (s *Service) Unplanned(run scheduler.Run) []scheduler.TargetOutcome {
	plan, found, err := frozenPlan(run)
	if err != nil || !found {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(run.Outcome.Targets), func(target scheduler.TargetOutcome) bool {
		return (target.ResourceType != "container" && target.ResourceType != "image") || target.Status != scheduler.Failed ||
			slices.ContainsFunc(plan.Targets, func(planned arcaneupdater.FrozenUpdateTarget) bool { return planned.ContainerID == target.ID })
	})
}

// frozenPlan decodes the plan persisted on the run's batch target.
func frozenPlan(run scheduler.Run) (frozenUpdatePlan, bool, error) {
	var plan frozenUpdatePlan
	for _, target := range run.Outcome.Targets {
		if target.ID == "auto-update" && len(target.RecoveryData) > 0 {
			err := json.Unmarshal(target.RecoveryData, &plan)
			return plan, err == nil, err
		}
	}
	return plan, false, nil
}

func immutableImage(target arcaneupdater.FrozenUpdateTarget) (string, error) {
	reference, err := refs.NormalizeReference(target.DesiredImageRef)
	if err != nil {
		return "", err
	}
	if target.DesiredDigest == "" {
		return "", errors.New("desired update digest is unavailable")
	}
	return reference.RegistryHost + "/" + reference.Repository + "@" + target.DesiredDigest, nil
}
