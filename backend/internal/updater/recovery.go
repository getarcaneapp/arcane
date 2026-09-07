package updater

import (
	"context"
	"sync"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"go.getarcane.app/updater"
)

type updateProgressKeyInternal struct{}
type updateProgressInternal struct {
	mu            sync.Mutex
	err           error
	selfTriggered bool
	selfID        string
}

// RecordUpdateRun persists one updater resource result into Arcane history.
func (s *UpdaterService) RecordUpdateRun(ctx context.Context, result updater.ResourceResult) error {
	var recordErr error
	if s != nil && s.deps.DB != nil {
		recordErr = s.recordRunInternal(ctx, resourceResultFromModuleInternal(result))
	}
	status := schedulertypes.NeedsAttention
	switch result.Status {
	case updater.StatusUpdated, updater.StatusRestarted, updater.StatusUpToDate:
		status = schedulertypes.Succeeded
	case updater.StatusSkipped:
		status = schedulertypes.Skipped
	case updater.StatusChecked, updater.StatusUpdateAvailable:
		status = schedulertypes.NeedsAttention
	case updater.StatusFailed:
		status = schedulertypes.Failed
	}
	progressErr := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ID: result.ResourceID, ResourceType: string(result.ResourceType), Status: status, Message: result.Error, ActivityID: activityIDFromContextInternal(ctx)})
	err := errors.Combine(recordErr, progressErr)
	if progress, ok := ctx.Value(updateProgressKeyInternal{}).(*updateProgressInternal); ok && err != nil {
		progress.mu.Lock()
		progress.err = errors.Combine(progress.err, err)
		progress.mu.Unlock()
	}
	return err
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
