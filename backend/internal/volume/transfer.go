package volume

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"emperror.dev/errors"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	transferlib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/transfer"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
)

const transferMountPath = "/volume"

// Holds exposes the node's hold registry for the transfer endpoint.
func (s *VolumeService) Holds() *transferlib.Holds { return s.holds }

// transferMountInternal validates a volume name and maps it onto /volume.
func transferMountInternal(volumeName string, readOnly bool) (mount.Mount, error) {
	name := strings.TrimSpace(volumeName)
	if name == "" {
		return mount.Mount{}, errors.New("transfer volume name is required")
	}
	return mount.Mount{Type: mount.TypeVolume, Source: name, Target: transferMountPath, ReadOnly: readOnly}, nil
}

// transferHelperInternal starts a throwaway helper with the source or target on /volume.
func (s *VolumeService) transferHelperInternal(ctx context.Context, volumeName string, readOnly bool) (*client.Client, string, func(), error) {
	helperMount, err := transferMountInternal(volumeName, readOnly)
	if err != nil {
		return nil, "", nil, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, "", nil, errors.WrapIf(err, "failed to connect to Docker")
	}
	containerID, cleanup, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, "", helperMount)
	return dockerClient, containerID, cleanup, err
}

// InspectForTransfer describes a volume, its consumers, and its capacity for
// transfer preflight. A missing volume reports Exists=false without an error.
func (s *VolumeService) InspectForTransfer(ctx context.Context, volumeName string) (transfertypes.VolumeInspection, error) {
	name := strings.TrimSpace(volumeName)
	inspection := transfertypes.VolumeInspection{Name: name, Consumers: []transfertypes.Consumer{}, SizeBytes: -1, FileCount: -1}
	if name == "" {
		return inspection, errors.New("transfer volume name is required")
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return inspection, errors.WrapIf(err, "failed to connect to Docker")
	}
	inspect, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return inspection, nil
	}
	if err != nil {
		return inspection, errors.WrapIf(err, "inspect volume")
	}
	inspection.Exists = true
	inspection.Driver = inspect.Volume.Driver
	inspection.Options = maps.Clone(inspect.Volume.Options)
	inspection.Labels = maps.Clone(inspect.Volume.Labels)
	inspection.Anonymous = isAnonymousVolumeInternal(inspect.Volume)
	inspection.Internal = s.isInternalVolumeInternal(volumetypes.NewSummary(inspect.Volume))
	containerIDs, err := dockerutil.GetContainersUsingVolume(ctx, dockerClient, name)
	if err != nil {
		return inspection, err
	}
	for _, containerID := range containerIDs {
		consumer, found, err := s.inspectTransferConsumerInternal(ctx, dockerClient, transfertypes.Consumer{ContainerID: containerID})
		if err != nil {
			return inspection, err
		}
		if found {
			inspection.Consumers = append(inspection.Consumers, consumer)
		}
	}
	slices.SortFunc(inspection.Consumers, func(a, b transfertypes.Consumer) int { return strings.Compare(a.Name, b.Name) })
	if sizes, err := s.GetVolumeSizes(ctx); err == nil {
		if size, ok := sizes[name]; ok {
			inspection.SizeBytes = size.Size
		}
	}
	hold, err := s.holds.Get(ctx, transfertypes.KindVolume, name)
	if err != nil {
		return inspection, errors.WrapIf(err, "read transfer hold")
	}
	inspection.Hold = hold
	return inspection, nil
}

// inspectTransferConsumerInternal resolves a consumer by ID, then name; Arcane's own containers report found=false.
func (s *VolumeService) inspectTransferConsumerInternal(ctx context.Context, dockerClient *client.Client, consumer transfertypes.Consumer) (transfertypes.Consumer, bool, error) {
	for _, ref := range kit.TrimNonEmpty([]string{consumer.ContainerID, consumer.Name}) {
		inspect, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, ref, client.ContainerInspectOptions{})
		if cerrdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return consumer, false, errors.WrapIff(err, "inspect container %s", ref)
		}
		if config := inspect.Container.Config; config != nil {
			if internal, _ := kit.ParseBool(config.Labels[libarcane.InternalResourceLabel]); internal || isArcaneOwnedContainerInternal(config.Labels) {
				return consumer, false, nil
			}
		}
		return dockerutil.TransferConsumerFromInspect(inspect.Container), true, nil
	}
	return consumer, false, nil
}

// OpenTransferArchive streams the source as tar; cleanup removes the helper once the stream is consumed.
func (s *VolumeService) OpenTransferArchive(ctx context.Context, source transfertypes.DataSource) (io.ReadCloser, func(), error) {
	dockerClient, containerID, cleanup, err := s.transferHelperInternal(ctx, source.Volume, true)
	if err != nil {
		return nil, nil, err
	}
	archive, err := dockerClient.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{SourcePath: transferMountPath + "/."})
	if err != nil {
		cleanup()
		return nil, nil, errors.WrapIf(err, "read transfer archive")
	}
	return archive.Content, cleanup, nil
}

// ImportTransferArchive creates the labelled volume and extracts the archive
// into it, removing the volume again on failure.
func (s *VolumeService) ImportTransferArchive(ctx context.Context, target transfertypes.DataTarget, source io.Reader) error {
	targetMount, err := transferMountInternal(target.Volume, false)
	if err != nil {
		return err
	}
	if err := s.ensureVolumeMutableInternal(ctx, targetMount.Source); err != nil {
		return err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to connect to Docker")
	}
	if _, err := dockerClient.VolumeInspect(ctx, targetMount.Source, client.VolumeInspectOptions{}); err == nil {
		return common.Classify(common.ErrConflict, errors.Errorf("volume %s already exists", targetMount.Source))
	} else if !cerrdefs.IsNotFound(err) {
		return errors.WrapIf(err, "inspect target volume")
	}
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{Name: targetMount.Source}); err != nil {
		return errors.WrapIf(err, "create target volume")
	}
	// The helper is removed before the volume is dropped so nothing holds it.
	containerID, cleanup, err := s.createBackupTempContainerWithMountInternal(ctx, dockerClient, "", targetMount)
	if err == nil {
		_, err = dockerClient.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{DestinationPath: transferMountPath, Content: source, CopyUIDGID: true})
		cleanup()
	}
	if err == nil {
		dockerutil.InvalidateVolumeUsageCache(dockerClient)
		return nil
	}
	if _, removeErr := dockerClient.VolumeRemove(context.WithoutCancel(ctx), targetMount.Source, client.VolumeRemoveOptions{Force: true}); removeErr != nil {
		slog.WarnContext(ctx, "volume service: could not remove partial transfer volume", "volume", targetMount.Source, "error", removeErr.Error())
	}
	return errors.WrapIf(err, "extract transfer archive")
}

// transferConsumerSummaryInternal carries the identity the restart pass matches on.
func transferConsumerSummaryInternal(consumer transfertypes.Consumer) container.Summary {
	return container.Summary{ID: consumer.ContainerID, Names: []string{"/" + consumer.Name}, Labels: map[string]string{
		dockerutil.ComposeProjectLabelKey: consumer.ComposeProject,
		dockerutil.ComposeServiceLabelKey: consumer.ComposeService,
	}}
}

// StopTransferConsumers stops the consumers gracefully in the order given. On
// the first failure it restores what it already stopped and reports the rest as failed.
func (s *VolumeService) StopTransferConsumers(ctx context.Context, request transfertypes.StopConsumersRequest) (transfertypes.StopConsumersResponse, error) {
	ctx, user := transferlib.WithHoldOwner(ctx, request.TransferID), common.SystemUser
	response := transfertypes.StopConsumersResponse{Stopped: []transfertypes.Consumer{}}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return response, errors.WrapIf(err, "failed to connect to Docker")
	}
	for _, requested := range request.Consumers {
		consumer, found, err := s.inspectTransferConsumerInternal(ctx, dockerClient, requested)
		if err != nil {
			return response, err
		}
		if !found || !consumer.Running {
			consumer.Running = false
			response.Stopped = append(response.Stopped, consumer)
			continue
		}
		seconds := max(0, cmp.Or(request.GracePeriodSeconds, consumer.StopTimeout))
		grace := cmp.Or(time.Duration(seconds)*time.Second, transfertypes.DefaultStopGracePeriod)
		stopErr := s.containerService.StopContainerGracefully(ctx, consumer.ContainerID, grace, user)
		if stopErr == nil {
			response.Stopped = append(response.Stopped, consumer)
			continue
		}
		response.Failed = append(response.Failed, transfertypes.ConsumerFailure{ContainerID: consumer.ContainerID, Name: consumer.Name, Error: stopErr.Error()})
		restore, restoreErr := s.RestoreTransferConsumers(context.WithoutCancel(ctx), transfertypes.RestoreConsumersRequest{TransferID: request.TransferID, Consumers: response.Stopped})
		response.Failed = append(response.Failed, restore.Failed...)
		return response, errors.Combine(errors.WrapIff(stopErr, "stop container %s for transfer", consumer.Name), restoreErr)
	}
	return response, nil
}

// RestoreTransferConsumers restarts the consumers recorded as running, matching replacements like backup recovery.
func (s *VolumeService) RestoreTransferConsumers(ctx context.Context, request transfertypes.RestoreConsumersRequest) (transfertypes.RestoreConsumersResponse, error) {
	ctx, user := transferlib.WithHoldOwner(ctx, request.TransferID), common.SystemUser
	response := transfertypes.RestoreConsumersResponse{Restored: []string{}}
	toStart := make([]container.Summary, 0, len(request.Consumers))
	for _, consumer := range request.Consumers {
		if consumer.Running {
			toStart = append(toStart, transferConsumerSummaryInternal(consumer))
		}
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return response, errors.WrapIf(err, "failed to connect to Docker")
	}
	stillDown, restartErr := s.startContainersAfterBackupInternal(ctx, dockerClient, toStart, user)
	for _, summary := range toStart {
		if slices.ContainsFunc(stillDown, func(down container.Summary) bool { return down.ID == summary.ID }) {
			response.Failed = append(response.Failed, transfertypes.ConsumerFailure{ContainerID: summary.ID, Name: dockerutil.ContainerNameFromNames(summary.Names), Error: restartErr.Error()})
			continue
		}
		response.Restored = append(response.Restored, summary.ID)
	}
	return response, restartErr
}

// RemoveTransferVolume deletes a volume without force under the transfer's hold.
func (s *VolumeService) RemoveTransferVolume(ctx context.Context, volumeName string, request transfertypes.RemoveRequest) error {
	name := strings.TrimSpace(volumeName)
	transferID := strings.TrimSpace(request.TransferID)
	if name == "" || transferID == "" {
		return errors.New("volume name and transfer ID are required")
	}
	return s.DeleteVolume(transferlib.WithHoldOwner(ctx, transferID), name, false, common.SystemUser)
}
