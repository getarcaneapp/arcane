package transfer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
)

const (
	chunkRetriesInternal        = 3
	chunkRetryDelayInternal     = 2 * time.Second
	progressEveryChunksInternal = 5
)

// errCanceledInternal marks a stop requested through the transfer API.
var errCanceledInternal = errors.New("transfer canceled")

// runInternal is the durable job body: one attempt of the phase machine
// followed by recovery when it does not complete.
func (s *Service) runInternal(ctx context.Context, transferID string) (schedulertypes.Outcome, error) {
	record, err := s.loadInternal(ctx, transferID)
	if errors.Is(err, common.ErrTransferNotFound) {
		return schedulertypes.Outcome{Status: schedulertypes.Canceled, Message: "transfer no longer exists"}, nil
	}
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.Retrying}, err
	}
	if record.Status.Terminal() || record.Status == transfertypes.StatusNeedsAttention {
		return schedulertypes.Outcome{Status: outcomeStatusInternal(record.Status), Message: record.Error}, nil
	}
	if record.CancelRequested {
		return s.finishInternal(ctx, record, errCanceledInternal)
	}
	lease, admitted, err := s.jobs.TryAcquire(ctx, transferID)
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.Retrying}, err
	}
	if !admitted {
		return schedulertypes.Outcome{Status: schedulertypes.Waiting, Message: "another run of this transfer is active"}, nil
	}
	defer lease.Release()
	pairLease, admitted, err := s.jobs.TryAcquire(ctx, record.SourceEnvironmentID+"->"+record.DestinationEnvironmentID)
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.Retrying}, err
	}
	if !admitted {
		return schedulertypes.Outcome{Status: schedulertypes.Waiting, Message: "another transfer between these environments is active"}, nil
	}
	defer pairLease.Release()

	record, err = s.updateInternal(ctx, transferID, func(t *ResourceTransfer) {
		t.Attempt++
		t.Status = transfertypes.StatusRunning
		t.Error = ""
		t.Recovery = nil
	})
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.Retrying}, err
	}
	user := common.SystemUser
	activityID, workCtx := activitylib.StartHandlerActivity(ctx, s.activity, record.SourceEnvironmentID, activitytypes.TypeResourceTransfer,
		string(record.Kind), resourceIDInternal(record), record.Plan.Resources[0].SourceName, &user,
		"Preparing", fmt.Sprintf("%s %s to %s", strings.ToUpper(string(record.Mode[:1]))+string(record.Mode[1:]), record.Kind, record.Plan.DestinationEnvironmentName),
		map[string]any{"transferId": record.ID, "mode": record.Mode, "destinationEnvironmentId": record.DestinationEnvironmentID}, false)
	if activityID != "" {
		record, _ = s.updateInternal(ctx, transferID, func(t *ResourceTransfer) { t.ActivityID = activityID })
	}
	runErr := s.executeInternal(workCtx, record)
	outcome, err := s.finishInternal(context.WithoutCancel(workCtx), record, runErr)
	activitylib.CompleteHandlerActivity(workCtx, s.activity, activityID, "Transfer completed", runErr)
	return outcome, err
}

// reconcileInternal handles a run interrupted by a restart: nothing about
// the endpoints is trusted, so recovery runs before anything continues.
func (s *Service) reconcileInternal(ctx context.Context, transferID string, _ schedulertypes.Run) (schedulertypes.Outcome, error) {
	record, err := s.loadInternal(ctx, transferID)
	if errors.Is(err, common.ErrTransferNotFound) {
		return schedulertypes.Outcome{Status: schedulertypes.Canceled, Message: "transfer no longer exists"}, nil
	}
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention}, err
	}
	if record.Status == transfertypes.StatusRunning {
		if err := s.markInterruptedInternal(ctx, record); err != nil {
			return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention}, err
		}
		record, _ = s.loadInternal(ctx, transferID)
	}
	return schedulertypes.Outcome{Status: outcomeStatusInternal(record.Status), Message: record.Error}, nil
}

// markInterruptedInternal recovers a transfer whose worker died mid-run.
func (s *Service) markInterruptedInternal(ctx context.Context, record *ResourceTransfer) error {
	_, err := s.finishInternal(ctx, record, errors.New("transfer interrupted by an Arcane restart"))
	return err
}

func outcomeStatusInternal(status transfertypes.Status) schedulertypes.RunStatus {
	switch status {
	case transfertypes.StatusSucceeded, transfertypes.StatusRolledBack:
		return schedulertypes.Succeeded
	case transfertypes.StatusCanceled:
		return schedulertypes.Canceled
	case transfertypes.StatusNeedsAttention:
		return schedulertypes.NeedsAttention
	case transfertypes.StatusFailed:
		return schedulertypes.Failed
	case transfertypes.StatusQueued, transfertypes.StatusRunning:
		return schedulertypes.Running
	default:
		return schedulertypes.Running
	}
}

func resourceIDInternal(record *ResourceTransfer) string {
	if record.Kind == transfertypes.KindProject {
		return record.Plan.Request.ProjectID
	}
	return record.Plan.Request.VolumeName
}

// finishInternal records the terminal state and runs recovery when needed.
func (s *Service) finishInternal(ctx context.Context, record *ResourceTransfer, runErr error) (schedulertypes.Outcome, error) {
	now := time.Now().UTC()
	if runErr == nil {
		_, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) {
			t.Status = transfertypes.StatusSucceeded
			t.Phase = transfertypes.PhaseFinished
			t.FinishedAt = &now
		})
		return schedulertypes.Outcome{Status: schedulertypes.Succeeded, Message: "Transfer completed"}, err
	}
	current, err := s.loadInternal(ctx, record.ID)
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention}, err
	}
	canceled := errors.Is(runErr, errCanceledInternal) || activitylib.CancelledByContext(ctx) || current.CancelRequested
	var report transfertypes.RecoveryReport
	status := transfertypes.StatusFailed
	switch {
	case current.DestinationStartupAttempted:
		// Destination writes may exist: stop it if reachable and hand over.
		report = s.stopDestinationInternal(ctx, current)
		status = transfertypes.StatusNeedsAttention
	case canceled:
		report = s.recoverInternal(ctx, current, true)
		status = transfertypes.StatusCanceled
	default:
		report = s.recoverInternal(ctx, current, false)
	}
	message := runErr.Error()
	if len(report.Incomplete) > 0 {
		message += "; recovery incomplete: " + strings.Join(report.Incomplete, "; ")
		if status == transfertypes.StatusFailed {
			status = transfertypes.StatusNeedsAttention
		}
	}
	_, err = s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) {
		t.Status = status
		t.Phase = transfertypes.PhaseRecover
		t.Error = message
		t.Recovery = &report
		if status.Terminal() {
			t.FinishedAt = &now
		}
	})
	if err != nil {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention}, err
	}
	if status == transfertypes.StatusCanceled {
		s.jobs.Unregister(ctx, record.ID)
	}
	return schedulertypes.Outcome{Status: outcomeStatusInternal(status), Message: message}, nil
}

// executeInternal runs the phases in order, persisting each boundary.
func (s *Service) executeInternal(ctx context.Context, record *ResourceTransfer) error {
	steps := []struct {
		phase transfertypes.Phase
		run   func(context.Context, *ResourceTransfer) error
	}{
		{transfertypes.PhaseRevalidate, s.revalidateInternal},
		{transfertypes.PhaseReserve, s.reserveInternal},
		{transfertypes.PhasePrepare, s.prepareInternal},
		{transfertypes.PhaseStop, s.stopSourceInternal},
		{transfertypes.PhaseCopy, s.copyInternal},
		{transfertypes.PhaseCutover, s.cutoverInternal},
	}
	for _, step := range steps {
		if err := s.checkCanceledInternal(ctx, record.ID); err != nil {
			return err
		}
		updated, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.Phase = step.phase })
		if err != nil {
			return err
		}
		s.stepInternal(ctx, updated, phaseLabelInternal(step.phase), nil)
		if err := step.run(ctx, updated); err != nil {
			return fmt.Errorf("%s: %w", step.phase, err)
		}
		if record, err = s.loadInternal(ctx, record.ID); err != nil {
			return err
		}
	}
	return nil
}

func phaseLabelInternal(phase transfertypes.Phase) string {
	switch phase {
	case transfertypes.PhaseRevalidate:
		return "Revalidating plan"
	case transfertypes.PhaseReserve:
		return "Reserving resources"
	case transfertypes.PhasePrepare:
		return "Preparing destination"
	case transfertypes.PhaseStop:
		return "Stopping source"
	case transfertypes.PhaseCopy:
		return "Copying data"
	case transfertypes.PhaseCutover:
		return "Finishing"
	case transfertypes.PhasePending, transfertypes.PhaseRecover, transfertypes.PhaseFinished:
		return string(phase)
	default:
		return string(phase)
	}
}

func (s *Service) checkCanceledInternal(ctx context.Context, transferID string) error {
	if err := ctx.Err(); err != nil {
		if activitylib.CancelledByContext(ctx) {
			return errCanceledInternal
		}
		return err
	}
	record, err := s.loadInternal(ctx, transferID)
	if err != nil {
		return err
	}
	if record.CancelRequested {
		return errCanceledInternal
	}
	return nil
}

func (s *Service) stepInternal(ctx context.Context, record *ResourceTransfer, step string, progress *int) {
	if record.ActivityID == "" {
		return
	}
	request := activity.UpdateActivityRequest{Step: &step, Progress: progress}
	if _, err := s.activity.UpdateActivity(utils.ActivityRuntimeContext(ctx, nil), record.ActivityID, request); err != nil {
		slog.DebugContext(ctx, "transfer: failed to update activity", "transfer", record.ID, "error", err)
	}
}

// revalidateInternal reruns preflight on the first attempt; later attempts
// trust the persisted plan because the source may already be stopped.
func (s *Service) revalidateInternal(ctx context.Context, record *ResourceTransfer) error {
	permissions, err := s.requesterPermissionsInternal(ctx, record.RequestedBy)
	if err != nil {
		return err
	}
	if record.Attempt > 1 {
		return authorizeActionInternal(permissions, record)
	}
	plan, err := s.buildPlanInternal(ctx, record.SourceEnvironmentID, record.Plan.Request, permissions)
	if err != nil {
		return err
	}
	if plan.PlanHash != record.Plan.PlanHash {
		return common.ErrTransferPlanChanged
	}
	if plan.Blocked() {
		return fmt.Errorf("%w: %s", common.ErrTransferBlocked, plan.Blockers[0].Message)
	}
	return nil
}

func (s *Service) sourceHoldsInternal(record *ResourceTransfer) []transfertypes.HoldRequest {
	holds := []transfertypes.HoldRequest{}
	if record.Kind == transfertypes.KindProject {
		holds = append(holds, transfertypes.HoldRequest{TransferID: record.ID, Kind: transfertypes.KindProject, Resource: record.Plan.Request.ProjectID})
	}
	for _, resource := range record.Plan.Resources {
		if resource.Kind == transfertypes.ResourceVolume {
			holds = append(holds, transfertypes.HoldRequest{TransferID: record.ID, Kind: transfertypes.KindVolume, Resource: resource.SourceName})
		}
	}
	return holds
}

func (s *Service) destinationHoldsInternal(record *ResourceTransfer) []transfertypes.HoldRequest {
	holds := []transfertypes.HoldRequest{}
	if record.Kind == transfertypes.KindProject {
		holds = append(holds, transfertypes.HoldRequest{TransferID: record.ID, Kind: transfertypes.KindProject, Resource: record.Plan.Request.DestinationName})
	}
	for _, resource := range record.Plan.Resources {
		if resource.Kind == transfertypes.ResourceVolume {
			holds = append(holds, transfertypes.HoldRequest{TransferID: record.ID, Kind: transfertypes.KindVolume, Resource: resource.DestinationName})
		}
	}
	return holds
}

func (s *Service) reserveInternal(ctx context.Context, record *ResourceTransfer) error {
	source := record.SourceEnvironmentID
	destination := record.DestinationEnvironmentID
	for _, hold := range s.sourceHoldsInternal(record) {
		if _, err := callInternal[transfertypes.Hold](ctx, s, source, http.MethodPost, "/api/environments/0/transfer/holds", hold, callTimeoutInternal, func() (transfertypes.Hold, error) { return s.Hold(ctx, hold) }); err != nil {
			return fmt.Errorf("reserve source %s %s: %w", hold.Kind, hold.Resource, err)
		}
	}
	if _, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.SourceHeld = true }); err != nil {
		return err
	}
	for _, hold := range s.destinationHoldsInternal(record) {
		if _, err := callInternal[transfertypes.Hold](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/holds", hold, callTimeoutInternal, func() (transfertypes.Hold, error) { return s.Hold(ctx, hold) }); err != nil {
			return fmt.Errorf("reserve destination %s %s: %w", hold.Kind, hold.Resource, err)
		}
	}
	return nil
}

// releaseHoldsInternal drops the transfer's reservations on each side.
func (s *Service) releaseHoldsInternal(ctx context.Context, record *ResourceTransfer, sourceSide, destinationSide bool) error {
	var combined error
	if sourceSide {
		source := record.SourceEnvironmentID
		for _, hold := range s.sourceHoldsInternal(record) {
			if err := discardInternal(callInternal[base.MessageResponse](ctx, s, source, http.MethodDelete, "/api/environments/0/transfer/holds/"+url.PathEscape(string(hold.Kind))+"/"+url.PathEscape(hold.Resource)+"?transferId="+url.QueryEscape(record.ID), nil, callTimeoutInternal, func() (base.MessageResponse, error) {
				return done, s.holds.Release(ctx, record.ID, hold.Kind, hold.Resource)
			})); err != nil {
				combined = errors.Join(combined, fmt.Errorf("release source %s %s: %w", hold.Kind, hold.Resource, err))
			}
		}
	}
	if destinationSide {
		destination := record.DestinationEnvironmentID
		for _, hold := range s.destinationHoldsInternal(record) {
			if err := discardInternal(callInternal[base.MessageResponse](ctx, s, destination, http.MethodDelete, "/api/environments/0/transfer/holds/"+url.PathEscape(string(hold.Kind))+"/"+url.PathEscape(hold.Resource)+"?transferId="+url.QueryEscape(record.ID), nil, callTimeoutInternal, func() (base.MessageResponse, error) {
				return done, s.holds.Release(ctx, record.ID, hold.Kind, hold.Resource)
			})); err != nil {
				combined = errors.Join(combined, fmt.Errorf("release destination %s %s: %w", hold.Kind, hold.Resource, err))
			}
		}
	}
	return combined
}

// prepareInternal copies the project directory, registers the destination
// project, pre-pulls its images, and creates stopped containers before any
// downtime starts. Volume transfers have nothing to prepare.
func (s *Service) prepareInternal(ctx context.Context, record *ResourceTransfer) error {
	if record.Kind != transfertypes.KindProject || record.DestinationProjectID != "" {
		return nil
	}
	return s.prepareProjectInternal(ctx, record)
}

// prepareProjectInternal is the project-directory pipeline: copy, register
// with the identity rewrite, pull images, create stopped containers.
func (s *Service) prepareProjectInternal(ctx context.Context, record *ResourceTransfer) error {
	resource, index := findResourceInternal(record, transfertypes.ResourceProjectDir)
	if index < 0 {
		return errors.New("plan has no project directory resource")
	}
	if resource.Status != transfertypes.ResourceVerified {
		if err := s.copyResourceInternal(ctx, record, index); err != nil {
			return err
		}
	}
	destination := record.DestinationEnvironmentID
	rewrite := transfertypes.ProjectRewrite{
		Name:           record.Plan.Request.DestinationName,
		VolumeMappings: map[string]string{},
	}
	for _, planned := range record.Plan.Resources {
		if planned.Kind == transfertypes.ResourceVolume && planned.SourceName != planned.DestinationName {
			rewrite.VolumeMappings[planned.SourceName] = planned.DestinationName
		}
	}
	registered, err := callInternal[transfertypes.RegisterProjectResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/projects/register", transfertypes.RegisterProjectRequest{TransferID: record.ID, ProjectDir: projectDirNameInternal(record), Rewrite: rewrite}, longTimeoutInternal, func() (transfertypes.RegisterProjectResponse, error) {
		return s.projects.RegisterTransferredProject(ctx, transfertypes.RegisterProjectRequest{TransferID: record.ID, ProjectDir: projectDirNameInternal(record), Rewrite: rewrite})
	})
	if err != nil {
		return fmt.Errorf("register destination project: %w", err)
	}
	if _, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.DestinationProjectID = registered.ProjectID }); err != nil {
		return err
	}
	s.stepInternal(ctx, record, "Pulling images on destination", nil)
	if err := discardInternal(callInternal[base.MessageResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/projects/"+url.PathEscape(registered.ProjectID)+"/prepare", transfertypes.ProjectActionRequest{TransferID: record.ID}, longTimeoutInternal, func() (base.MessageResponse, error) {
		return done, s.projects.PrepareTransferredProject(ctx, registered.ProjectID, transfertypes.ProjectActionRequest{TransferID: record.ID})
	})); err != nil {
		return fmt.Errorf("prepare destination project: %w", err)
	}
	return nil
}

func projectDirNameInternal(record *ResourceTransfer) string {
	return record.Plan.Request.DestinationName
}

func findResourceInternal(record *ResourceTransfer, kind transfertypes.ResourceKind) (transfertypes.ResourceProgress, int) {
	for index, resource := range record.Resources {
		if resource.Kind == kind {
			return resource, index
		}
	}
	return transfertypes.ResourceProgress{}, -1
}

// stopSourceInternal records the running state and stops writers gracefully,
// dependents before their dependencies for projects.
func (s *Service) stopSourceInternal(ctx context.Context, record *ResourceTransfer) error {
	consumers := record.RecordedConsumers
	if len(consumers) == 0 {
		consumers = orderConsumersInternal(record.Plan.Consumers, record.Plan.ServiceDependencies)
		if _, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.RecordedConsumers = consumers }); err != nil {
			return err
		}
	}
	if !record.Plan.RequiresDowntime {
		return nil
	}
	response, err := callInternal[transfertypes.StopConsumersResponse](ctx, s, record.SourceEnvironmentID, http.MethodPost, "/api/environments/0/transfer/consumers/stop", transfertypes.StopConsumersRequest{TransferID: record.ID, Consumers: consumers}, longTimeoutInternal, func() (transfertypes.StopConsumersResponse, error) {
		return s.volumes.StopTransferConsumers(ctx, transfertypes.StopConsumersRequest{TransferID: record.ID, Consumers: consumers})
	})
	if err != nil {
		return fmt.Errorf("stop source consumers: %w", err)
	}
	if len(response.Failed) > 0 {
		return fmt.Errorf("stop source consumers: %s: %s", response.Failed[0].Name, response.Failed[0].Error)
	}
	return nil
}

// orderConsumersInternal puts dependents before the services they depend on
// so a stop in list order never pulls a dependency from under a writer.
func orderConsumersInternal(consumers []transfertypes.Consumer, dependsOn map[string][]string) []transfertypes.Consumer {
	if len(dependsOn) == 0 {
		return consumers
	}
	depth := map[string]int{}
	var depthOf func(service string, seen map[string]bool) int
	depthOf = func(service string, seen map[string]bool) int {
		if value, ok := depth[service]; ok {
			return value
		}
		if seen[service] {
			return 0
		}
		seen[service] = true
		best := 0
		for _, dependency := range dependsOn[service] {
			best = max(best, depthOf(dependency, seen)+1)
		}
		depth[service] = best
		return best
	}
	ordered := append([]transfertypes.Consumer(nil), consumers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return depthOf(ordered[i].ComposeService, map[string]bool{}) > depthOf(ordered[j].ComposeService, map[string]bool{})
	})
	return ordered
}

// copyInternal relays every data resource that is not already done. The
// project directory was copied before downtime in the prepare phase.
func (s *Service) copyInternal(ctx context.Context, record *ResourceTransfer) error {
	for index, resource := range record.Resources {
		if resource.Kind == transfertypes.ResourceProjectDir || resource.Status == transfertypes.ResourceVerified {
			continue
		}
		if err := s.checkCanceledInternal(ctx, record.ID); err != nil {
			return err
		}
		if err := s.copyResourceInternal(ctx, record, index); err != nil {
			return err
		}
	}
	return nil
}

func sourceForInternal(record *ResourceTransfer, resource transfertypes.ResourceProgress) transfertypes.DataSource {
	switch resource.Kind { //nolint:exhaustive // volumes are the default
	case transfertypes.ResourceProjectDir:
		return transfertypes.DataSource{Kind: resource.Kind, ProjectID: record.Plan.Request.ProjectID}
	default:
		return transfertypes.DataSource{Kind: resource.Kind, Volume: resource.SourceName}
	}
}

func targetForInternal(record *ResourceTransfer, resource transfertypes.ResourceProgress) transfertypes.DataTarget {
	switch resource.Kind { //nolint:exhaustive // volumes are the default
	case transfertypes.ResourceProjectDir:
		return transfertypes.DataTarget{Kind: resource.Kind, ProjectDir: projectDirNameInternal(record)}
	default:
		return transfertypes.DataTarget{Kind: resource.Kind, Volume: resource.DestinationName}
	}
}

func (s *Service) updateResourceInternal(ctx context.Context, record *ResourceTransfer, key string, change func(*transfertypes.ResourceProgress)) error {
	_, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) {
		for index := range t.Resources {
			if t.Resources[index].Key == key {
				change(&t.Resources[index])
				return
			}
		}
	})
	return err
}

// copyResourceInternal stages the source archive, relays it into an upload
// session on the destination in bounded byte ranges, and imports it there
// against the source hash.
func (s *Service) copyResourceInternal(ctx context.Context, record *ResourceTransfer, index int) error {
	resource := record.Resources[index]
	source := record.SourceEnvironmentID
	destination := record.DestinationEnvironmentID
	if err := s.updateResourceInternal(ctx, record, resource.Key, func(r *transfertypes.ResourceProgress) {
		r.Status = transfertypes.ResourceCopying
		r.Attempt = record.Attempt
		r.BytesTransferred = 0
		r.SHA256 = ""
		r.Error = ""
	}); err != nil {
		return err
	}
	s.stepInternal(ctx, record, "Staging "+resource.SourceName, nil)
	export, err := callInternal[transfertypes.Export](ctx, s, source, http.MethodPost, "/api/environments/0/transfer/exports", transfertypes.ExportRequest{TransferID: record.ID, Source: sourceForInternal(record, resource)}, longTimeoutInternal, func() (transfertypes.Export, error) {
		return s.Export(ctx, transfertypes.ExportRequest{TransferID: record.ID, Source: sourceForInternal(record, resource)})
	})
	if err != nil {
		return s.failResourceInternal(ctx, record, resource, fmt.Errorf("stage %s: %w", resource.SourceName, err))
	}
	defer func() {
		_ = discardInternal(callInternal[base.MessageResponse](context.WithoutCancel(ctx), s, source, http.MethodDelete, "/api/environments/0/transfer/exports/"+url.PathEscape(export.ExportID), nil, callTimeoutInternal, func() (base.MessageResponse, error) { s.spool.Remove(export.ExportID); return done, nil }))
	}()
	upload, err := callInternal[uploadtypes.Session](ctx, s, destination, http.MethodPost, "/api/environments/0/uploads/"+uploadtypes.KindVolumeBackup, uploadtypes.CreateSessionRequest{Filename: "transfer.tar.gz", Size: export.Size, ChunkSize: transfertypes.ChunkSize}, callTimeoutInternal, func() (uploadtypes.Session, error) {
		session, err := s.uploads.CreateSession(ctx, uploadtypes.KindVolumeBackup, uploadtypes.CreateSessionRequest{Filename: "transfer.tar.gz", Size: export.Size, ChunkSize: transfertypes.ChunkSize})
		if err != nil {
			return uploadtypes.Session{}, err
		}
		return *session, nil
	})
	if err != nil {
		return s.failResourceInternal(ctx, record, resource, fmt.Errorf("open upload for %s: %w", resource.DestinationName, err))
	}
	if err := s.relayInternal(ctx, record, resource, source, destination, export, upload.ID); err != nil {
		_ = discardInternal(callInternal[base.MessageResponse](context.WithoutCancel(ctx), s, destination, http.MethodDelete, "/api/environments/0/uploads/"+uploadtypes.KindVolumeBackup+"/"+url.PathEscape(upload.ID), nil, callTimeoutInternal, func() (base.MessageResponse, error) {
			return done, s.uploads.DeleteSession(context.WithoutCancel(ctx), uploadtypes.KindVolumeBackup, upload.ID)
		}))
		return s.failResourceInternal(ctx, record, resource, err)
	}
	s.stepInternal(ctx, record, "Extracting "+resource.DestinationName, nil)
	err = discardInternal(callInternal[base.MessageResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/imports", transfertypes.ImportRequest{TransferID: record.ID, Target: targetForInternal(record, resource), UploadID: upload.ID, SHA256: export.SHA256}, longTimeoutInternal, func() (base.MessageResponse, error) {
		return done, s.Import(ctx, transfertypes.ImportRequest{TransferID: record.ID, Target: targetForInternal(record, resource), UploadID: upload.ID, SHA256: export.SHA256})
	}))
	if err != nil {
		return s.failResourceInternal(ctx, record, resource, fmt.Errorf("import %s: %w", resource.DestinationName, err))
	}
	return s.updateResourceInternal(ctx, record, resource.Key, func(r *transfertypes.ResourceProgress) {
		r.Status = transfertypes.ResourceVerified
		r.SHA256 = export.SHA256
		r.BytesTransferred = export.Size
		r.BytesTotal = export.Size
	})
}

func (s *Service) failResourceInternal(ctx context.Context, record *ResourceTransfer, resource transfertypes.ResourceProgress, cause error) error {
	_ = s.updateResourceInternal(context.WithoutCancel(ctx), record, resource.Key, func(r *transfertypes.ResourceProgress) {
		r.Status = transfertypes.ResourceFailed
		r.Error = cause.Error()
	})
	return cause
}

// relayInternal moves the staged archive one byte range at a time with
// bounded retries per hop; both hops are idempotent per chunk index.
func (s *Service) relayInternal(ctx context.Context, record *ResourceTransfer, resource transfertypes.ResourceProgress, source, destination string, export transfertypes.Export, uploadID string) error {
	for index, offset := 0, int64(0); offset < export.Size; index, offset = index+1, offset+transfertypes.ChunkSize {
		if err := s.checkCanceledInternal(ctx, record.ID); err != nil {
			return err
		}
		var chunk transfertypes.ExportRange
		if err := retryInternal(ctx, func() error {
			var err error
			chunk, err = callInternal[transfertypes.ExportRange](ctx, s, source, http.MethodGet, "/api/environments/0/transfer/exports/"+url.PathEscape(export.ExportID)+"?offset="+strconv.FormatInt(offset, 10)+"&length="+strconv.FormatInt(transfertypes.ChunkSize, 10), nil, chunkTimeoutInternal, func() (transfertypes.ExportRange, error) {
				data, err := s.ExportRead(export.ExportID, offset, transfertypes.ChunkSize)
				return transfertypes.ExportRange{Data: data}, err
			})
			return err
		}); err != nil {
			return fmt.Errorf("read chunk %d of %s: %w", index, resource.SourceName, err)
		}
		if err := retryInternal(ctx, func() error {
			if destination == environment.LocalEnvironmentID {
				_, err := s.uploads.WriteChunk(ctx, uploadtypes.KindVolumeBackup, uploadID, index, chunk.Data)
				return err
			}
			callCtx, cancel := context.WithTimeout(ctx, chunkTimeoutInternal)
			defer cancel()
			response, err := s.environments.ExecuteRemoteRequest(callCtx, destination, http.MethodPut, "/api/environments/0/uploads/"+uploadtypes.KindVolumeBackup+"/"+url.PathEscape(uploadID)+"/chunks/"+strconv.Itoa(index), chunk.Data, map[string]string{"Content-Type": "application/octet-stream"})
			if err != nil {
				return err
			}
			if err := response.RequireSuccess(); err != nil {
				return remoteErrorInternal(err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("write chunk %d of %s: %w", index, resource.DestinationName, err)
		}
		done := min(offset+int64(len(chunk.Data)), export.Size)
		if index%progressEveryChunksInternal == 0 || done == export.Size {
			_ = s.updateResourceInternal(ctx, record, resource.Key, func(r *transfertypes.ResourceProgress) { r.BytesTransferred, r.BytesTotal = done, export.Size })
			s.stepInternal(ctx, record, "Copying "+resource.SourceName, progressPercentInternal(done, export.Size))
		}
	}
	return nil
}

func progressPercentInternal(done, total int64) *int {
	if total <= 0 {
		return nil
	}
	percent := int(min(done*100/total, 100))
	return &percent
}

// retryInternal repeats a transport call on transient failures; semantic
// rejections (4xx) surface immediately.
func retryInternal(ctx context.Context, call func() error) error {
	var err error
	for attempt := 1; attempt <= chunkRetriesInternal; attempt++ {
		err = call()
		if err == nil {
			return nil
		}
		if errors.Is(err, common.ErrConflict) || errors.Is(err, common.ErrNotFound) || errors.Is(err, common.ErrBadRequest) || errors.Is(err, common.ErrForbidden) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(chunkRetryDelayInternal * time.Duration(attempt)):
		}
	}
	return err
}

// cutoverInternal starts the destination for moves, restores the source for
// copies, and releases the holds that are no longer needed.
func (s *Service) cutoverInternal(ctx context.Context, record *ResourceTransfer) error {
	source := record.SourceEnvironmentID
	destination := record.DestinationEnvironmentID
	if record.Mode == transfertypes.ModeMove {
		if record.Kind == transfertypes.KindProject {
			now := time.Now().UTC()
			if _, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) {
				t.DestinationStartupAttempted = true
				t.DestinationStartupAt = &now
			}); err != nil {
				return err
			}
			s.stepInternal(ctx, record, "Starting destination project", nil)
			if err := discardInternal(callInternal[base.MessageResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/projects/"+url.PathEscape(record.DestinationProjectID)+"/deploy", transfertypes.ProjectActionRequest{TransferID: record.ID}, longTimeoutInternal, func() (base.MessageResponse, error) {
				return done, s.projects.DeployTransferredProject(ctx, record.DestinationProjectID, transfertypes.ProjectActionRequest{TransferID: record.ID})
			})); err != nil {
				return fmt.Errorf("start destination project: %w", err)
			}
		}
		if err := s.releaseHoldsInternal(ctx, record, false, true); err != nil {
			slog.WarnContext(ctx, "transfer: failed to release destination holds", "transfer", record.ID, "error", err)
		}
		return nil
	}
	s.stepInternal(ctx, record, "Restoring source", nil)
	if err := s.restoreSourceInternal(ctx, record, source); err != nil {
		return err
	}
	if err := s.releaseHoldsInternal(ctx, record, true, true); err != nil {
		return err
	}
	_, err := s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.SourceHeld = false })
	return err
}

// restoreSourceInternal restarts whatever was running before the stop phase.
func (s *Service) restoreSourceInternal(ctx context.Context, record *ResourceTransfer, source string) error {
	if len(record.RecordedConsumers) == 0 {
		return nil
	}
	response, err := callInternal[transfertypes.RestoreConsumersResponse](ctx, s, source, http.MethodPost, "/api/environments/0/transfer/consumers/restore", transfertypes.RestoreConsumersRequest{TransferID: record.ID, Consumers: record.RecordedConsumers}, longTimeoutInternal, func() (transfertypes.RestoreConsumersResponse, error) {
		return s.volumes.RestoreTransferConsumers(ctx, transfertypes.RestoreConsumersRequest{TransferID: record.ID, Consumers: record.RecordedConsumers})
	})
	if err != nil {
		return fmt.Errorf("restore source consumers: %w", err)
	}
	if len(response.Failed) > 0 {
		return fmt.Errorf("restore source consumers: %s: %s", response.Failed[0].Name, response.Failed[0].Error)
	}
	return nil
}

// removeDestinationResourceInternal deletes one destination resource this
// transfer created.
func (s *Service) removeDestinationResourceInternal(ctx context.Context, record *ResourceTransfer, resource transfertypes.ResourceProgress) error {
	destination := record.DestinationEnvironmentID
	owned := transfertypes.RemoveRequest{TransferID: record.ID, RemoveFiles: true}
	var err error
	switch resource.Kind {
	case transfertypes.ResourceVolume:
		err = discardInternal(callInternal[base.MessageResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/volumes/"+url.PathEscape(resource.DestinationName)+"/remove", owned, longTimeoutInternal, func() (base.MessageResponse, error) {
			return done, s.volumes.RemoveTransferVolume(ctx, resource.DestinationName, owned)
		}))
	case transfertypes.ResourceProjectDir:
		if record.DestinationProjectID != "" {
			err = discardInternal(callInternal[base.MessageResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/projects/"+url.PathEscape(record.DestinationProjectID)+"/remove", owned, longTimeoutInternal, func() (base.MessageResponse, error) {
				return done, s.projects.RemoveTransferProject(ctx, record.DestinationProjectID, owned)
			}))
			if err == nil {
				_, err = s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.DestinationProjectID = "" })
			}
		}
	}
	if err != nil && !errors.Is(err, common.ErrNotFound) {
		return fmt.Errorf("remove destination %s: %w", resource.DestinationName, err)
	}
	return s.updateResourceInternal(ctx, record, resource.Key, func(r *transfertypes.ResourceProgress) {
		r.Status = transfertypes.ResourcePending
		r.SHA256 = ""
		r.BytesTransferred = 0
	})
}

// recoverInternal restores the source before any destination startup and
// removes incomplete (or, when the transfer is over, all) owned destination
// resources. Everything it could not do is reported, never hidden.
func (s *Service) recoverInternal(ctx context.Context, record *ResourceTransfer, terminal bool) transfertypes.RecoveryReport {
	report := transfertypes.RecoveryReport{}
	source := record.SourceEnvironmentID
	if err := s.restoreSourceInternal(ctx, record, source); err != nil {
		report.Incomplete = append(report.Incomplete, err.Error())
	} else {
		report.SourceRestored = true
	}
	removedAll := true
	for _, resource := range record.Resources {
		keep := !terminal && resource.Status == transfertypes.ResourceVerified
		if keep || resource.Status == transfertypes.ResourcePending {
			continue
		}
		if err := s.removeDestinationResourceInternal(ctx, record, resource); err != nil {
			removedAll = false
			report.Incomplete = append(report.Incomplete, err.Error())
		}
	}
	report.DestinationRemoved = removedAll
	if terminal {
		if err := s.releaseHoldsInternal(ctx, record, true, true); err != nil {
			report.Incomplete = append(report.Incomplete, err.Error())
		} else {
			_, _ = s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) { t.SourceHeld = false })
		}
	}
	return report
}

// stopDestinationInternal is the only automatic action after the startup
// boundary: stop destination workloads when reachable, never touch the source.
func (s *Service) stopDestinationInternal(ctx context.Context, record *ResourceTransfer) transfertypes.RecoveryReport {
	report := transfertypes.RecoveryReport{}
	if record.DestinationProjectID == "" {
		return report
	}
	destination := record.DestinationEnvironmentID
	inspection, err := callInternal[transfertypes.ProjectInspection](ctx, s, destination, http.MethodGet, "/api/environments/0/transfer/projects/"+url.PathEscape(record.DestinationProjectID), nil, longTimeoutInternal, func() (transfertypes.ProjectInspection, error) {
		return s.projects.InspectForTransfer(ctx, record.DestinationProjectID)
	})
	if err != nil {
		report.DestinationUnknown = true
		report.Incomplete = append(report.Incomplete, "destination state unknown: "+err.Error())
		return report
	}
	if _, err := callInternal[transfertypes.StopConsumersResponse](ctx, s, destination, http.MethodPost, "/api/environments/0/transfer/consumers/stop", transfertypes.StopConsumersRequest{TransferID: record.ID, Consumers: orderConsumersInternal(inspection.Containers, record.Plan.ServiceDependencies)}, longTimeoutInternal, func() (transfertypes.StopConsumersResponse, error) {
		return s.volumes.StopTransferConsumers(ctx, transfertypes.StopConsumersRequest{TransferID: record.ID, Consumers: orderConsumersInternal(inspection.Containers, record.Plan.ServiceDependencies)})
	}); err != nil {
		report.Incomplete = append(report.Incomplete, "stop destination project: "+err.Error())
	}
	return report
}

// Rollback restores the recorded source state after a Move once the
// destination is confirmed stopped.
func (s *Service) Rollback(ctx context.Context, sourceEnvID string, kind transfertypes.Kind, transferID string, request transfertypes.RollbackRequest, permissions *authz.PermissionSet) (*ResourceTransfer, error) {
	record, err := s.Get(ctx, sourceEnvID, kind, transferID)
	if err != nil {
		return nil, err
	}
	if err := authorizeActionInternal(permissions, record); err != nil {
		return nil, err
	}
	if !request.AcknowledgeDestinationWritesDiscarded {
		return nil, common.Classify(common.ErrValidation, errors.New("acknowledge that newer destination writes will not be merged"))
	}
	if record.Status != transfertypes.StatusSucceeded && record.Status != transfertypes.StatusNeedsAttention && record.Status != transfertypes.StatusFailed {
		return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("transfer is %s", record.Status))
	}
	if !record.SourceHeld {
		return nil, common.Classify(common.ErrTransferStateInvalid, errors.New("the source hold was already released; nothing to roll back"))
	}
	destination := record.DestinationEnvironmentID
	if record.Kind == transfertypes.KindProject && record.DestinationProjectID != "" {
		inspection, err := callInternal[transfertypes.ProjectInspection](ctx, s, destination, http.MethodGet, "/api/environments/0/transfer/projects/"+url.PathEscape(record.DestinationProjectID), nil, longTimeoutInternal, func() (transfertypes.ProjectInspection, error) {
			return s.projects.InspectForTransfer(ctx, record.DestinationProjectID)
		})
		if err != nil {
			return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("destination state is unknown; the source stays stopped: %w", err))
		}
		for _, container := range inspection.Containers {
			if container.Running {
				return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("destination container %s is still running; stop the destination project first", container.Name))
			}
		}
	}
	if record.Kind == transfertypes.KindVolume {
		inspection, err := callInternal[transfertypes.VolumeInspection](ctx, s, destination, http.MethodGet, "/api/environments/0/transfer/volumes/"+url.PathEscape(record.Plan.Request.DestinationName), nil, longTimeoutInternal, func() (transfertypes.VolumeInspection, error) {
			return s.volumes.InspectForTransfer(ctx, record.Plan.Request.DestinationName)
		})
		if err != nil {
			return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("destination state is unknown; the source stays stopped: %w", err))
		}
		for _, container := range inspection.Consumers {
			if container.Running {
				return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("destination container %s still uses the volume; stop it first", container.Name))
			}
		}
	}
	source := record.SourceEnvironmentID
	if err := s.restoreSourceInternal(ctx, record, source); err != nil {
		return nil, err
	}
	if err := s.releaseHoldsInternal(ctx, record, true, false); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return s.updateInternal(ctx, transferID, func(t *ResourceTransfer) {
		t.Status = transfertypes.StatusRolledBack
		t.SourceHeld = false
		t.FinishedAt = &now
	})
}

// Cleanup deletes the selected source resources after a successful Move.
// Each item is rechecked and refused individually; nothing is implied.
func (s *Service) Cleanup(ctx context.Context, sourceEnvID string, kind transfertypes.Kind, transferID string, request transfertypes.CleanupRequest, permissions *authz.PermissionSet) (transfertypes.CleanupResponse, error) {
	record, err := s.Get(ctx, sourceEnvID, kind, transferID)
	if err != nil {
		return transfertypes.CleanupResponse{}, err
	}
	if err := authorizeActionInternal(permissions, record); err != nil {
		return transfertypes.CleanupResponse{}, err
	}
	if record.Mode != transfertypes.ModeMove || record.Status != transfertypes.StatusSucceeded {
		return transfertypes.CleanupResponse{}, common.Classify(common.ErrTransferStateInvalid, errors.New("cleanup is only offered after a successful move"))
	}
	cleanup := &cleanupRunInternal{
		service:     s,
		record:      record,
		source:      record.SourceEnvironmentID,
		permissions: permissions,
		response:    transfertypes.CleanupResponse{Removed: []string{}, Skipped: []transfertypes.CleanupSkip{}},
	}
	if record.Kind == transfertypes.KindProject && (request.RemoveSourceProject || request.RemoveSourceFiles) {
		cleanup.projectInternal(ctx, request.RemoveSourceFiles)
	}
	for _, name := range request.RemoveSourceVolumes {
		cleanup.volumeInternal(ctx, name)
	}
	if len(cleanup.response.Removed) > 0 {
		if err := s.releaseHoldsInternal(ctx, record, true, false); err != nil {
			cleanup.skip("holds", err.Error())
		} else {
			_, _ = s.updateInternal(ctx, transferID, func(t *ResourceTransfer) { t.SourceHeld = false })
		}
	}
	return cleanup.response, nil
}

// cleanupRunInternal accumulates one cleanup request's removals and refusals.
type cleanupRunInternal struct {
	service     *Service
	record      *ResourceTransfer
	source      string
	permissions *authz.PermissionSet
	response    transfertypes.CleanupResponse
}

func (c *cleanupRunInternal) skip(resource, reason string) {
	c.response.Skipped = append(c.response.Skipped, transfertypes.CleanupSkip{Resource: resource, Reason: reason})
}

func (c *cleanupRunInternal) removed(resource string) {
	c.response.Removed = append(c.response.Removed, resource)
}

func (c *cleanupRunInternal) projectInternal(ctx context.Context, removeFiles bool) {
	projectID := c.record.Plan.Request.ProjectID
	if !c.permissions.Allows(authz.PermProjectsDelete, c.record.SourceEnvironmentID) {
		c.skip(projectID, "missing projects:delete on the source environment")
		return
	}
	if err := discardInternal(callInternal[base.MessageResponse](ctx, c.service, c.source, http.MethodPost, "/api/environments/0/transfer/projects/"+url.PathEscape(projectID)+"/remove", transfertypes.RemoveRequest{TransferID: c.record.ID, RemoveFiles: removeFiles}, longTimeoutInternal, func() (base.MessageResponse, error) {
		return done, c.service.projects.RemoveTransferProject(ctx, projectID, transfertypes.RemoveRequest{TransferID: c.record.ID, RemoveFiles: removeFiles})
	})); err != nil {
		c.skip(projectID, err.Error())
		return
	}
	c.removed(projectID)
}

func (c *cleanupRunInternal) volumeInternal(ctx context.Context, name string) {
	resource, ok := plannedResourceInternal(c.record, transfertypes.ResourceVolume, name)
	if !ok {
		c.skip(name, "volume is not part of this transfer")
		return
	}
	if !c.permissions.Allows(authz.PermVolumesDelete, c.record.SourceEnvironmentID) {
		c.skip(name, "missing volumes:delete on the source environment")
		return
	}
	if resource.Status != transfertypes.ResourceVerified {
		c.skip(name, "volume was not transferred by this transfer")
		return
	}
	if err := discardInternal(callInternal[base.MessageResponse](ctx, c.service, c.source, http.MethodPost, "/api/environments/0/transfer/volumes/"+url.PathEscape(name)+"/remove", transfertypes.RemoveRequest{TransferID: c.record.ID}, longTimeoutInternal, func() (base.MessageResponse, error) {
		return done, c.service.volumes.RemoveTransferVolume(ctx, name, transfertypes.RemoveRequest{TransferID: c.record.ID})
	})); err != nil {
		c.skip(name, err.Error())
		return
	}
	c.removed(name)
}

func plannedResourceInternal(record *ResourceTransfer, kind transfertypes.ResourceKind, name string) (transfertypes.ResourceProgress, bool) {
	for _, resource := range record.Resources {
		if resource.Kind == kind && resource.SourceName == name {
			return resource, true
		}
	}
	return transfertypes.ResourceProgress{}, false
}
