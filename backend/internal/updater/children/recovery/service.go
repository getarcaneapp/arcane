package recovery

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/refs"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
)

// Service freezes update targets before any pull, pins pulls to the frozen
// digests, and confirms or resumes interrupted updates.
type Service struct {
	dockerClient   func(ctx context.Context) (*client.Client, error)
	digestResolver func() updater.RegistryDigestResolver
	pendingUpdates func(ctx context.Context) ([]updater.ImageUpdateRecord, error)
	activityID     func(ctx context.Context) string
	acquireUpdate  func(ctx context.Context) (context.Context, func(), error)
	applyPending   func(ctx context.Context, options arcaneupdater.Options) (*arcaneupdater.Result, error)
}

func NewService(
	dockerClient func(ctx context.Context) (*client.Client, error),
	digestResolver func() updater.RegistryDigestResolver,
	pendingUpdates func(ctx context.Context) ([]updater.ImageUpdateRecord, error),
	activityID func(ctx context.Context) string,
	acquireUpdate func(ctx context.Context) (context.Context, func(), error),
	applyPending func(ctx context.Context, options arcaneupdater.Options) (*arcaneupdater.Result, error),
) *Service {
	return &Service{
		dockerClient:   dockerClient,
		digestResolver: digestResolver,
		pendingUpdates: pendingUpdates,
		activityID:     activityID,
		acquireUpdate:  acquireUpdate,
		applyPending:   applyPending,
	}
}

// FrozenRecords returns the frozen batch records carried by ctx, if any.
func (s *Service) FrozenRecords(ctx context.Context) ([]updater.ImageUpdateRecord, bool) {
	plan, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal)
	if !ok {
		return nil, false
	}
	return slices.Clone(plan.Records), true
}

// WithSingleTarget scopes pulls in ctx to one accepted container update.
func (s *Service) WithSingleTarget(ctx context.Context, target *arcaneupdater.FrozenUpdateTarget, persist func() error) context.Context {
	return context.WithValue(ctx, frozenSingleKeyInternal{}, &frozenSingleInternal{Target: target, Persist: persist})
}

// VerifyResult confirms a reported success against the frozen batch plan.
func (s *Service) VerifyResult(ctx context.Context, resourceID string) error {
	plan, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal)
	if !ok {
		return nil
	}
	for _, target := range plan.Targets {
		if target.ContainerID != resourceID {
			continue
		}
		confirmed, _, err := s.ConfirmTarget(ctx, target)
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("updater result does not confirm the frozen desired image")
		}
		return nil
	}
	return nil
}

type (
	frozenPendingKeyInternal struct{}
	frozenSingleKeyInternal  struct{}
	frozenUpdatePlanInternal struct {
		Records []updater.ImageUpdateRecord
		Targets []arcaneupdater.FrozenUpdateTarget
	}
)

type frozenSingleInternal struct {
	Target  *arcaneupdater.FrozenUpdateTarget
	Persist func() error
}

// errNoFrozenPlanInternal reports a run without a persisted frozen plan.
var errNoFrozenPlanInternal = errors.New("interrupted update has no frozen target plan")

// FreezePending persists identities and desired images before any pull. A run
// that already holds a plan keeps it. Image refs and container IDs in excluded are left
// out, and a target that cannot be frozen fails alone instead of aborting the batch.
func (s *Service) FreezePending(ctx context.Context, run scheduler.Run, excluded map[string]bool) (context.Context, error) {
	if _, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal); ok {
		return ctx, nil
	}
	if plan, found, err := frozenPlanInternal(run); err != nil || found {
		return context.WithValue(ctx, frozenPendingKeyInternal{}, &plan), err
	}
	records, err := s.pendingUpdates(ctx)
	if err != nil {
		return ctx, err
	}
	records = slices.DeleteFunc(records, func(record updater.ImageUpdateRecord) bool {
		return excluded[refs.NormalizeImageUpdateRef(record.ImageRef())] || excluded[record.ContainerID]
	})
	plan, failures, err := s.buildFrozenPlanInternal(ctx, records, excluded)
	if err != nil {
		return ctx, err
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return ctx, err
	}
	activityID := s.activityID(ctx)
	// Failures are recorded before the plan, since a delivery that finds the plan never plans again.
	for containerID, freezeErr := range failures {
		failure := scheduler.TargetOutcome{ID: containerID, ResourceType: "container", Status: scheduler.Failed, Message: "Update could not be planned: " + freezeErr.Error(), ActivityID: activityID}
		if failedProgressErr := jobcontext.Progress(ctx, failure); failedProgressErr != nil {
			return ctx, failedProgressErr
		}
	}
	batch := scheduler.TargetOutcome{ID: "auto-update", ResourceType: "update-batch", Status: scheduler.Running, RecoveryData: raw, ActivityID: activityID}
	if progressErr := jobcontext.Progress(ctx, batch); progressErr != nil {
		return ctx, progressErr
	}
	for _, target := range plan.Targets {
		if queuedProgressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: target.ContainerID, ResourceType: "container", Status: scheduler.Queued}); queuedProgressErr != nil {
			return ctx, queuedProgressErr
		}
	}
	return context.WithValue(ctx, frozenPendingKeyInternal{}, &plan), nil
}

func (s *Service) buildFrozenPlanInternal(ctx context.Context, records []updater.ImageUpdateRecord, excluded map[string]bool) (frozenUpdatePlanInternal, map[string]error, error) {
	plan := frozenUpdatePlanInternal{Records: make([]updater.ImageUpdateRecord, 0, len(records))}
	failures := map[string]error{}
	if len(records) == 0 {
		return plan, failures, nil
	}
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return plan, nil, err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return plan, nil, err
	}
	for _, record := range records {
		if !record.HasUpdate {
			continue
		}
		for _, candidate := range listed.Items {
			if excluded[candidate.ID] || (record.ContainerID != "" && record.ContainerID != candidate.ID) {
				continue
			}
			if record.ContainerID == "" && refs.NormalizeImageUpdateRef(candidate.Image) != refs.NormalizeImageUpdateRef(record.ImageRef()) {
				continue
			}
			target, selected, freezeRecordTargetErr := s.freezeRecordTargetInternal(ctx, record, candidate.ID)
			if freezeRecordTargetErr != nil {
				failures[candidate.ID] = freezeRecordTargetErr
				continue
			}
			plan.Records = append(plan.Records, selected)
			plan.Targets = append(plan.Targets, *target)
		}
	}
	return plan, failures, nil
}

func (s *Service) freezeRecordTargetInternal(ctx context.Context, record updater.ImageUpdateRecord, containerID string) (*arcaneupdater.FrozenUpdateTarget, updater.ImageUpdateRecord, error) {
	target, err := s.FreezeContainer(ctx, containerID)
	if err != nil {
		return nil, record, err
	}
	target.DesiredImageRef = record.NewImageRef()
	if record.LatestDigest != nil && !record.IsTagUpdate() {
		target.DesiredDigest = *record.LatestDigest
	}
	if target.DesiredDigest == "" {
		resolver := s.digestResolver()
		if resolver == nil {
			return nil, record, errors.New("cannot freeze update without registry digest resolver")
		}
		target.DesiredDigest, err = resolver.ImageDigest(ctx, target.DesiredImageRef)
		if err != nil {
			return nil, record, err
		}
	}
	if target.DesiredDigest == "" {
		return nil, record, errors.New("registry returned no desired digest")
	}
	record.ContainerID = containerID
	digest := target.DesiredDigest
	record.LatestDigest = &digest
	return target, record, nil
}

// FreezeContainer captures the identity evidence of one container.
func (s *Service) FreezeContainer(ctx context.Context, id string) (*arcaneupdater.FrozenUpdateTarget, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, err
	}
	result, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	inspected := result.Container
	target := &arcaneupdater.FrozenUpdateTarget{
		ContainerID: inspected.ID,
		ContainerName: strings.TrimPrefix(
			inspected.Name,
			"/",
		),
		BaselineImageID:      inspected.Image,
		BaselineRestartCount: inspected.RestartCount,
	}
	if inspected.State != nil {
		target.BaselineStartedAt = inspected.State.StartedAt
	}
	if inspected.Config != nil {
		target.ComposeProject = inspected.Config.Labels["com.docker.compose.project"]
		target.ComposeService = inspected.Config.Labels["com.docker.compose.service"]
		target.ComposeNumber = inspected.Config.Labels["com.docker.compose.container-number"]
	}
	return target, nil
}

// PreparePull commits the exact image before the engine changes Docker.
func (s *Service) PreparePull(ctx context.Context, imageRef string) (string, error) {
	if single, ok := ctx.Value(frozenSingleKeyInternal{}).(*frozenSingleInternal); ok {
		return s.prepareSinglePullInternal(ctx, imageRef, single)
	}
	if plan, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal); ok {
		return prepareBatchPullInternal(ctx, imageRef, plan)
	}
	return imageRef, nil
}

func (s *Service) prepareSinglePullInternal(ctx context.Context, imageRef string, single *frozenSingleInternal) (string, error) {
	target := single.Target
	if target.DesiredImageRef == "" {
		target.DesiredImageRef = imageRef
		resolver := s.digestResolver()
		if resolver == nil {
			return "", errors.New("cannot freeze update without registry digest resolver")
		}
		digest, err := resolver.ImageDigest(ctx, imageRef)
		if err != nil {
			return "", err
		}
		target.DesiredDigest = digest
		if persistErr := single.Persist(); persistErr != nil {
			return "", persistErr
		}
	}
	if refs.NormalizeImageUpdateRef(target.DesiredImageRef) != refs.NormalizeImageUpdateRef(imageRef) {
		return "", errors.New("selected image changed after the update was accepted")
	}
	return immutableImageInternal(*target)
}

func prepareBatchPullInternal(ctx context.Context, imageRef string, plan *frozenUpdatePlanInternal) (string, error) {
	immutable := ""
	for _, target := range plan.Targets {
		if refs.NormalizeImageUpdateRef(target.DesiredImageRef) != refs.NormalizeImageUpdateRef(imageRef) {
			continue
		}
		selected, err := immutableImageInternal(target)
		if err != nil {
			return "", err
		}
		if immutable != "" && immutable != selected {
			return "", errors.New("conflicting frozen digests for one image reference")
		}
		immutable = selected
		if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: target.ContainerID, ResourceType: "container", Status: scheduler.Running}); progressErr != nil {
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
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return false, false, err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return false, false, err
	}
	immutable, imageErr := immutableImageInternal(target)
	desiredID := ""
	if imageErr == nil {
		image, imageInspectErr := dockerClient.ImageInspect(ctx, immutable)
		if imageInspectErr == nil {
			desiredID = image.ID
		}
	}
	for _, candidate := range listed.Items {
		named := false
		for _, name := range candidate.Names {
			if strings.TrimPrefix(name, "/") == target.ContainerName {
				named = true
				break
			}
		}
		if !named {
			continue
		}
		result, containerInspectWithCompatibilityErr := compat.ContainerInspectWithCompatibility(ctx, dockerClient, candidate.ID, client.ContainerInspectOptions{})
		if containerInspectWithCompatibilityErr != nil {
			return false, false, containerInspectWithCompatibilityErr
		}
		inspected := result.Container
		if inspected.Config == nil {
			return false, false, nil
		}
		labels := inspected.Config.Labels
		if labels["com.docker.compose.project"] != target.ComposeProject ||
			labels["com.docker.compose.service"] != target.ComposeService ||
			labels["com.docker.compose.container-number"] != target.ComposeNumber {
			return false, false, nil
		}
		if desiredID != "" && inspected.Image == desiredID {
			return true, false, nil
		}
		unchanged := inspected.ID == target.ContainerID &&
			inspected.Image == target.BaselineImageID &&
			inspected.RestartCount == target.BaselineRestartCount &&
			inspected.State != nil &&
			inspected.State.StartedAt == target.BaselineStartedAt
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
	if errors.Is(err, errNoFrozenPlanInternal) {
		return scheduler.Outcome{Status: scheduler.Failed, Message: "Interrupted update has no frozen target plan", Targets: run.Outcome.Targets}, nil
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
	outcome := scheduler.Outcome{Status: scheduler.Succeeded, Message: "Frozen update targets confirmed"}
	// Containers that could not be planned were never updated, so recovery reports them as finalize does.
	if outcome.Targets = s.Unplanned(run); len(outcome.Targets) > 0 {
		outcome.Status, outcome.Message = scheduler.Partial, "Some updates failed"
	}
	if unresolved {
		outcome.Status, outcome.Message = scheduler.Failed, "Some update effects could not be confirmed"
	}
	return outcome, nil
}

// ResumePlan confirms each unsettled frozen target before anything repeats and
// scopes ctx to the targets still to apply. Targets that changed without
// reaching the desired image are marked for review and reported as unresolved.
func (s *Service) ResumePlan(ctx context.Context, run scheduler.Run) (context.Context, int, bool, error) {
	plan, found, err := frozenPlanInternal(run)
	if err != nil {
		return ctx, 0, false, err
	}
	if !found {
		return ctx, 0, false, errNoFrozenPlanInternal
	}
	remaining := frozenUpdatePlanInternal{}
	unresolved := false
	for _, target := range plan.Targets {
		if frozenTargetSettledInternal(run, target.ContainerID) {
			continue
		}
		confirmed, unchanged, confirmErr := s.ConfirmTarget(ctx, target)
		if confirmErr != nil {
			return ctx, 0, false, confirmErr
		}
		progress := scheduler.TargetOutcome{ID: target.ContainerID, ResourceType: "container"}
		switch {
		case confirmed:
			progress.Status, progress.Message = scheduler.Succeeded, "Frozen desired image confirmed"
		case !unchanged:
			unresolved = true
			progress.Status, progress.Message = scheduler.NeedsAttention, "Update effect could not be confirmed"
		default:
			remaining.Targets = append(remaining.Targets, target)
			remaining.Records = appendFrozenRecordInternal(remaining.Records, plan.Records, target.ContainerID)
			continue
		}
		if progressErr := jobcontext.Progress(ctx, progress); progressErr != nil {
			return ctx, 0, false, progressErr
		}
	}
	return context.WithValue(ctx, frozenPendingKeyInternal{}, &remaining), len(remaining.Records), unresolved, nil
}

// Planned reports whether the run already holds a frozen plan, as a retry does.
func (s *Service) Planned(run scheduler.Run) bool {
	_, found, err := frozenPlanInternal(run)
	return found && err == nil
}

// Started reports whether applying the frozen plan began: a planned container moved past queued.
func (s *Service) Started(run scheduler.Run) bool {
	plan, found, err := frozenPlanInternal(run)
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
	plan, found, err := frozenPlanInternal(run)
	if err != nil || !found {
		return 0, cmp.Or(err, errNoFrozenPlanInternal)
	}
	unsettled := 0
	for _, target := range plan.Targets {
		if !frozenTargetSettledInternal(run, target.ContainerID) {
			unsettled++
		}
	}
	return unsettled, nil
}

// Unplanned lists the images and containers that never reached the frozen plan: failed checks and failed freezes.
func (s *Service) Unplanned(run scheduler.Run) []scheduler.TargetOutcome {
	plan, found, err := frozenPlanInternal(run)
	if err != nil || !found {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(run.Outcome.Targets), func(target scheduler.TargetOutcome) bool {
		return (target.ResourceType != "container" && target.ResourceType != "image") || target.Status != scheduler.Failed ||
			slices.ContainsFunc(plan.Targets, func(planned arcaneupdater.FrozenUpdateTarget) bool { return planned.ContainerID == target.ID })
	})
}

// frozenPlanInternal decodes the plan persisted on the run's batch target.
func frozenPlanInternal(run scheduler.Run) (frozenUpdatePlanInternal, bool, error) {
	var plan frozenUpdatePlanInternal
	for _, target := range run.Outcome.Targets {
		if target.ID == "auto-update" && len(target.RecoveryData) > 0 {
			err := json.Unmarshal(target.RecoveryData, &plan)
			return plan, err == nil, err
		}
	}
	return plan, false, nil
}

func appendFrozenRecordInternal(remaining, records []updater.ImageUpdateRecord, id string) []updater.ImageUpdateRecord {
	for _, record := range records {
		if record.ContainerID == id {
			return append(remaining, record)
		}
	}
	return remaining
}

func frozenTargetSettledInternal(run scheduler.Run, id string) bool {
	for _, target := range run.Outcome.Targets {
		if target.ID == id && (target.Status == scheduler.Succeeded || target.Status == scheduler.Skipped) {
			return true
		}
	}
	return false
}

func immutableImageInternal(target arcaneupdater.FrozenUpdateTarget) (string, error) {
	reference, err := refs.NormalizeReference(target.DesiredImageRef)
	if err != nil {
		return "", err
	}
	if target.DesiredDigest == "" {
		return "", errors.New("desired update digest is unavailable")
	}
	return reference.RegistryHost + "/" + reference.Repository + "@" + target.DesiredDigest, nil
}
