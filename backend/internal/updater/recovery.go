package updater

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"sync"

	"emperror.dev/errors"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/refs"
)

type (
	updateProgressKeyInternal struct{}
	updateProgressInternal    struct {
		mu            sync.Mutex
		err           error
		selfTriggered bool
		selfID        string
	}
)

// RecordUpdateRun persists one updater resource result into Arcane history.
func (s *UpdaterService) RecordUpdateRun(ctx context.Context, result updater.ResourceResult) error {
	var recordErr error
	if s != nil && s.deps.DB != nil {
		recordErr = s.recordRunInternal(ctx, resourceResultFromModuleInternal(result))
	}
	name := cmp.Or(strings.TrimSpace(result.ResourceName), result.ResourceID)
	message := name + ": " + string(result.Status)
	level := activitytypes.MessageLevelInfo
	status := schedulertypes.NeedsAttention
	switch result.Status {
	case updater.StatusUpdated, updater.StatusRestarted, updater.StatusUpToDate:
		status = schedulertypes.Succeeded
		if result.Status != updater.StatusUpToDate && result.OldImage != "" && result.NewImage != "" && result.OldImage != result.NewImage {
			message += " (" + result.OldImage + " -> " + result.NewImage + ")"
		}
	case updater.StatusSkipped:
		status = schedulertypes.Skipped
	case updater.StatusChecked, updater.StatusUpdateAvailable:
		status = schedulertypes.NeedsAttention
	case updater.StatusFailed:
		status = schedulertypes.Failed
		level = activitytypes.MessageLevelError
	}
	if reason := strings.TrimSpace(result.Error); reason != "" {
		if level == activitytypes.MessageLevelError {
			message = name + ": " + reason
		} else {
			message += " (" + reason + ")"
		}
	}
	if activityID := activityIDFromContextInternal(ctx); activityID != "" && s != nil && s.deps.Activity != nil {
		if _, appendErr := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{Level: level, Message: message, Step: "Applying updates"}); appendErr != nil {
			slog.DebugContext(ctx, "failed to append update result activity message", "activityId", activityID, "resource", name, "error", appendErr)
		}
	}
	var evidenceErr error
	if status == schedulertypes.Succeeded {
		evidenceErr = s.verifyFrozenResultInternal(ctx, result.ResourceID)
	}
	if evidenceErr != nil {
		status = schedulertypes.NeedsAttention
	}
	progressMessage := result.Error
	if evidenceErr != nil {
		progressMessage = evidenceErr.Error()
	}
	progressErr := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: result.ResourceID, ResourceType: string(result.ResourceType), Status: status, Message: progressMessage, ActivityID: activityIDFromContextInternal(ctx)})
	err := errors.Combine(recordErr, progressErr, evidenceErr)
	if progress, ok := ctx.Value(updateProgressKeyInternal{}).(*updateProgressInternal); ok && err != nil {
		progress.mu.Lock()
		progress.err = errors.Combine(progress.err, err)
		progress.mu.Unlock()
	}
	return err
}

func (s *UpdaterService) verifyFrozenResultInternal(ctx context.Context, resourceID string) error {
	plan, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal)
	if !ok {
		return nil
	}
	for _, target := range plan.Targets {
		if target.ContainerID != resourceID {
			continue
		}
		confirmed, _, err := s.confirmFrozenTargetInternal(ctx, target)
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

func (p *updateProgressInternal) completeBatchInternal(ctx context.Context, options arcaneupdater.Options, out *arcaneupdater.Result, batchCompleted bool, err error) error {
	activityID := activityIDFromContextInternal(ctx)
	p.mu.Lock()
	err = errors.Combine(err, p.err)
	recordingFailed := p.err != nil
	_, durableRun := jobcontext.Run(ctx)
	selfTriggered := p.selfTriggered && durableRun
	selfID := p.selfID
	p.mu.Unlock()
	status := schedulertypes.Succeeded
	if !batchCompleted && err == nil {
		err = errors.New("update batch ended without a confirmed result")
		recordingFailed = true
	}
	if err != nil || out.Failed > 0 {
		status = schedulertypes.Partial
	}
	if recordingFailed {
		status = schedulertypes.NeedsAttention
	}
	if selfTriggered {
		err = errors.Combine(err, jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: selfID, ResourceType: "container", Status: schedulertypes.NeedsAttention, ActivityID: activityID, Message: "Self-update completion requires review"}))
		status = schedulertypes.NeedsAttention
		err = errors.Combine(err, errors.New("self-update was triggered but completion requires review"))
	}
	batchType := updateBatchTypeInternal(ctx, options, batchCompleted)
	checkpoint := schedulertypes.TargetOutcome{ID: "auto-update", ResourceType: batchType, Status: status, ActivityID: activityID}
	if err != nil {
		checkpoint.Message = err.Error()
	}
	checkpointErr := jobcontext.Progress(ctx, checkpoint)
	err = errors.Combine(err, checkpointErr)
	if err != nil {
		out.Success = false
	}
	if recordingFailed || selfTriggered || checkpointErr != nil {
		err = &schedulertypes.OutcomeError{Outcome: schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: err.Error(), ActivityID: activityID}, Cause: err}
	}
	return err
}

func updateBatchTypeInternal(ctx context.Context, options arcaneupdater.Options, batchCompleted bool) string {
	batchType := "update-batch"
	if previous, ok := jobcontext.Run(ctx); ok && len(options.ResourceIds) > 0 {
		batchType = "update-retry"
		for _, target := range previous.Outcome.Targets {
			if target.ID == "auto-update" && target.ResourceType == "update-batch" && (target.Status == schedulertypes.Succeeded || target.Status == schedulertypes.Partial) {
				batchType = "update-batch"
			}
		}
	}
	if !batchCompleted {
		batchType = "update-interrupted"
	}
	return batchType
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

// freezePendingInternal persists identities and desired images before any pull.
func (s *UpdaterService) freezePendingInternal(ctx context.Context) (context.Context, error) {
	if _, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal); ok {
		return ctx, nil
	}
	run, ok := jobcontext.Run(ctx)
	if !ok {
		return ctx, nil
	}
	for _, target := range run.Outcome.Targets {
		if target.ID == "auto-update" && len(target.RecoveryData) > 0 {
			var plan frozenUpdatePlanInternal
			if err := json.Unmarshal(target.RecoveryData, &plan); err != nil {
				return ctx, err
			}
			return context.WithValue(ctx, frozenPendingKeyInternal{}, &plan), nil
		}
	}
	records, err := s.PendingImageUpdates(ctx)
	if err != nil {
		return ctx, err
	}
	plan, err := s.buildFrozenPlanInternal(ctx, records)
	if err != nil {
		return ctx, err
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return ctx, err
	}
	if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: "auto-update", ResourceType: "update-batch", Status: schedulertypes.Running, RecoveryData: raw, ActivityID: activityIDFromContextInternal(ctx)}); err != nil {
		return ctx, err
	}
	for _, target := range plan.Targets {
		if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: target.ContainerID, ResourceType: "container", Status: schedulertypes.Queued}); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, frozenPendingKeyInternal{}, &plan), nil
}

func (s *UpdaterService) buildFrozenPlanInternal(ctx context.Context, records []updater.ImageUpdateRecord) (frozenUpdatePlanInternal, error) {
	plan := frozenUpdatePlanInternal{Records: make([]updater.ImageUpdateRecord, 0, len(records))}
	if len(records) == 0 {
		return plan, nil
	}
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return plan, err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return plan, err
	}
	for _, record := range records {
		if !record.HasUpdate {
			continue
		}
		for _, candidate := range listed.Items {
			if record.ContainerID != "" && record.ContainerID != candidate.ID {
				continue
			}
			if record.ContainerID == "" && refs.NormalizeImageUpdateRef(candidate.Image) != refs.NormalizeImageUpdateRef(record.ImageRef()) {
				continue
			}
			target, selected, err := s.freezeRecordTargetInternal(ctx, record, candidate.ID)
			if err != nil {
				return plan, err
			}
			plan.Records = append(plan.Records, selected)
			plan.Targets = append(plan.Targets, *target)
		}
	}
	return plan, nil
}

func (s *UpdaterService) freezeRecordTargetInternal(ctx context.Context, record updater.ImageUpdateRecord, containerID string) (*arcaneupdater.FrozenUpdateTarget, updater.ImageUpdateRecord, error) {
	target, err := s.freezeContainerInternal(ctx, containerID)
	if err != nil {
		return nil, record, err
	}
	target.DesiredImageRef = record.NewImageRef()
	if record.LatestDigest != nil && !record.IsTagUpdate() {
		target.DesiredDigest = *record.LatestDigest
	}
	if target.DesiredDigest == "" {
		resolver := s.registryDigestResolverInternal()
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

func (s *UpdaterService) freezeContainerInternal(ctx context.Context, id string) (*arcaneupdater.FrozenUpdateTarget, error) {
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return nil, err
	}
	result, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	inspected := result.Container
	target := &arcaneupdater.FrozenUpdateTarget{ContainerID: inspected.ID, ContainerName: strings.TrimPrefix(inspected.Name, "/"), BaselineImageID: inspected.Image, BaselineRestartCount: inspected.RestartCount}
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

// prepareFrozenPullInternal commits the exact image before the engine changes Docker.
func (s *UpdaterService) prepareFrozenPullInternal(ctx context.Context, imageRef string) (string, error) {
	if single, ok := ctx.Value(frozenSingleKeyInternal{}).(*frozenSingleInternal); ok {
		return s.prepareSinglePullInternal(ctx, imageRef, single)
	}
	if plan, ok := ctx.Value(frozenPendingKeyInternal{}).(*frozenUpdatePlanInternal); ok {
		return prepareBatchPullInternal(ctx, imageRef, plan)
	}
	return imageRef, nil
}

func (s *UpdaterService) prepareSinglePullInternal(ctx context.Context, imageRef string, single *frozenSingleInternal) (string, error) {
	target := single.Target
	if target.DesiredImageRef == "" {
		target.DesiredImageRef = imageRef
		resolver := s.registryDigestResolverInternal()
		if resolver == nil {
			return "", errors.New("cannot freeze update without registry digest resolver")
		}
		digest, err := resolver.ImageDigest(ctx, imageRef)
		if err != nil {
			return "", err
		}
		target.DesiredDigest = digest
		if err := single.Persist(); err != nil {
			return "", err
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
		if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: target.ContainerID, ResourceType: "container", Status: schedulertypes.Running}); err != nil {
			return "", err
		}
	}
	if immutable == "" {
		return "", errors.New("updater attempted to pull an image outside its frozen plan")
	}
	return immutable, nil
}

// confirmFrozenTargetInternal returns whether the desired effect is confirmed,
// or whether the exact original container is unchanged and safe to resume.
func (s *UpdaterService) confirmFrozenTargetInternal(ctx context.Context, target arcaneupdater.FrozenUpdateTarget) (bool, bool, error) {
	dockerClient, err := s.DockerClient(ctx)
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
		image, err := dockerClient.ImageInspect(ctx, immutable)
		if err == nil {
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
		result, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, candidate.ID, client.ContainerInspectOptions{})
		if err != nil {
			return false, false, err
		}
		inspected := result.Container
		if inspected.Config == nil {
			return false, false, nil
		}
		labels := inspected.Config.Labels
		if labels["com.docker.compose.project"] != target.ComposeProject || labels["com.docker.compose.service"] != target.ComposeService || labels["com.docker.compose.container-number"] != target.ComposeNumber {
			return false, false, nil
		}
		if desiredID != "" && inspected.Image == desiredID {
			return true, false, nil
		}
		unchanged := inspected.ID == target.ContainerID && inspected.Image == target.BaselineImageID && inspected.RestartCount == target.BaselineRestartCount && inspected.State != nil && inspected.State.StartedAt == target.BaselineStartedAt
		return false, unchanged, nil
	}
	return false, false, nil
}

// ReconcilePending resumes only frozen targets whose original container is unchanged.
func (s *UpdaterService) ReconcilePending(ctx context.Context, run schedulertypes.Run) (schedulertypes.Outcome, error) {
	ctx, release, err := s.acquireUpdateInternal(ctx)
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.Waiting}, err
	}
	defer release()
	var plan frozenUpdatePlanInternal
	found := false
	for _, target := range run.Outcome.Targets {
		if target.ID == "auto-update" && len(target.RecoveryData) > 0 {
			if err := json.Unmarshal(target.RecoveryData, &plan); err != nil {
				return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention}, err
			}
			found = true
		}
	}
	if !found {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: "Interrupted update has no frozen target plan", Targets: run.Outcome.Targets}, nil
	}
	remaining := frozenUpdatePlanInternal{}
	unresolved := false
	for _, target := range plan.Targets {
		if frozenTargetSettledInternal(run, target.ContainerID) {
			continue
		}
		confirmed, unchanged, err := s.confirmFrozenTargetInternal(ctx, target)
		if err != nil {
			return schedulertypes.Outcome{Status: schedulertypes.Waiting}, err
		}
		if confirmed {
			if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: target.ContainerID, ResourceType: "container", Status: schedulertypes.Succeeded, Message: "Frozen desired image confirmed after restart"}); err != nil {
				return schedulertypes.Outcome{}, err
			}
			continue
		}
		if !unchanged {
			unresolved = true
			continue
		}
		remaining.Targets = append(remaining.Targets, target)
		remaining.Records = appendFrozenRecordInternal(remaining.Records, plan.Records, target.ContainerID)
	}
	if len(remaining.Records) > 0 {
		result, err := s.ApplyPending(context.WithValue(ctx, frozenPendingKeyInternal{}, &remaining), arcaneupdater.Options{})
		if err != nil || result == nil || result.Failed > 0 {
			return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: "Frozen update recovery requires review"}, err
		}
	}
	status := schedulertypes.Succeeded
	message := "Frozen update targets confirmed"
	if unresolved {
		status = schedulertypes.NeedsAttention
		message = "Unconfirmed update effects require review"
	}
	return schedulertypes.Outcome{Status: status, Message: message}, nil
}

func frozenTargetSettledInternal(run schedulertypes.Run, id string) bool {
	for _, target := range run.Outcome.Targets {
		if target.ID == id && (target.Status == schedulertypes.Succeeded || target.Status == schedulertypes.Skipped) {
			return true
		}
	}
	return false
}

func appendFrozenRecordInternal(remaining, records []updater.ImageUpdateRecord, id string) []updater.ImageUpdateRecord {
	for _, record := range records {
		if record.ContainerID == id {
			return append(remaining, record)
		}
	}
	return remaining
}
