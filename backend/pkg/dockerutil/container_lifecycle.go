package docker

import (
	"context"
	"strings"
	"time"

	"emperror.dev/errors"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// ErrGracefulStopTimeout reports a container that ignored its stop signal for
// the whole grace period; the caller decides whether to escalate.
var ErrGracefulStopTimeout = errors.New("container did not stop within the grace period")

// StopContainerGracefully sends the container's configured stop signal
// (SIGTERM by default) and waits up to gracePeriod for it to exit. It never
// escalates to SIGKILL: a timeout is ErrGracefulStopTimeout.
func StopContainerGracefully(ctx context.Context, dockerClient *client.Client, containerID string, gracePeriod time.Duration) error {
	inspect, err := dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	if inspect.Container.State == nil || !inspect.Container.State.Running {
		return nil
	}
	signal := "SIGTERM"
	if inspect.Container.Config != nil && strings.TrimSpace(inspect.Container.Config.StopSignal) != "" {
		signal = inspect.Container.Config.StopSignal
	}
	waitCtx, cancel := context.WithTimeout(ctx, gracePeriod)
	defer cancel()
	wait := dockerClient.ContainerWait(waitCtx, containerID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	if _, err := dockerClient.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: signal}); err != nil {
		return err
	}
	select {
	case <-wait.Result:
		return nil
	case err := <-wait.Error:
		if errors.Is(err, context.DeadlineExceeded) || waitCtx.Err() != nil {
			return ErrGracefulStopTimeout
		}
		return err
	}
}

// TransferConsumerFromInspect records what a transfer needs to stop a
// container gracefully and start it again afterwards.
func TransferConsumerFromInspect(inspect container.InspectResponse) transfertypes.Consumer {
	var labels map[string]string
	if inspect.Config != nil {
		labels = inspect.Config.Labels
	}
	consumer := transfertypes.Consumer{
		ContainerID:    inspect.ID,
		Name:           strings.TrimPrefix(inspect.Name, "/"),
		ComposeProject: ComposeProjectLabel(labels),
		ComposeService: ComposeServiceLabel(labels),
	}
	if inspect.State != nil {
		consumer.Running = inspect.State.Running
	}
	if inspect.HostConfig != nil {
		consumer.RestartPolicy = string(inspect.HostConfig.RestartPolicy.Name)
	}
	if inspect.Config != nil && inspect.Config.StopTimeout != nil {
		consumer.StopTimeout = *inspect.Config.StopTimeout
	}
	return consumer
}
