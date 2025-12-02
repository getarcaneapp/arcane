package container

import (
	"context"
	"errors"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/versions"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

func waitExitOrRemoved(ctx context.Context, apiClient client.APIClient, containerID string, waitRemove bool) <-chan int {
	if len(containerID) == 0 {
		// containerID can never be empty
		panic("Internal Error: waitExitOrRemoved needs a containerID as parameter")
	}

	// Older versions used the Events API, and even older versions did not
	// support server-side removal. This legacyWaitExitOrRemoved method
	// preserves that old behavior and any issues it may have.
	if versions.LessThan(apiClient.ClientVersion(), "1.30") {
		return legacyWaitExitOrRemoved(ctx, apiClient, containerID, waitRemove)
	}

	condition := container.WaitConditionNextExit
	if waitRemove {
		condition = container.WaitConditionRemoved
	}

	resultC, errC := apiClient.ContainerWait(ctx, containerID, condition)

	statusC := make(chan int)
	go func() {
		defer close(statusC)
		select {
		case <-ctx.Done():
			return
		case result := <-resultC:
			if result.Error != nil {
				logrus.Errorf("Error waiting for container: %v", result.Error.Message)
				statusC <- 125
			} else {
				statusC <- int(result.StatusCode)
			}
		case err := <-errC:
			if errors.Is(err, context.Canceled) {
				return
			}
			logrus.Errorf("error waiting for container: %v", err)
			statusC <- 125
		}
	}()

	return statusC
}

func legacyWaitExitOrRemoved(ctx context.Context, apiClient client.APIClient, containerID string, waitRemove bool) <-chan int {
	var removeErr error
	statusChan := make(chan int)
	exitCode := 125

	// Get events via Events API
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("container", containerID)

	eventCtx, cancel := context.WithCancel(ctx)
	eventq, errq := apiClient.Events(eventCtx, events.ListOptions{
		Filters: f,
	})

	eventProcessor := func(e events.Message) bool {
		stopProcessing := false
		switch e.Action { //nolint:exhaustive // TODO(thaJeztah): make exhaustive
		case events.ActionDie:
			if v, ok := e.Actor.Attributes["exitCode"]; ok {
				code, cerr := strconv.Atoi(v)
				if cerr != nil {
					logrus.Errorf("failed to convert exitcode '%q' to int: %v", v, cerr)
				} else {
					exitCode = code
				}
			}
			if !waitRemove {
				stopProcessing = true
			} else if versions.LessThan(apiClient.ClientVersion(), "1.25") {
				// If we are talking to an older daemon, `AutoRemove` is not supported.
				// We need to fall back to the old behavior, which is client-side removal
				go func() {
					removeErr = apiClient.ContainerRemove(ctx, containerID, container.RemoveOptions{RemoveVolumes: true})
					if removeErr != nil {
						logrus.Errorf("error removing container: %v", removeErr)
						cancel() // cancel the event Q
					}
				}()
			}
		case events.ActionDetach:
			exitCode = 0
			stopProcessing = true
		case events.ActionDestroy:
			stopProcessing = true
		}
		return stopProcessing
	}

	go func() {
		defer func() {
			statusChan <- exitCode // must always send an exit code or the caller will block
			cancel()
		}()

		for {
			select {
			case <-eventCtx.Done():
				if removeErr != nil {
					return
				}
			case evt := <-eventq:
				if eventProcessor(evt) {
					return
				}
			case err := <-errq:
				logrus.Errorf("error getting events from daemon: %v", err)
				return
			}
		}
	}()

	return statusChan
}

func parallelOperation(ctx context.Context, containers []string, op func(ctx context.Context, containerID string) error) chan error {
	if len(containers) == 0 {
		return nil
	}
	const defaultParallel int = 50
	sem := make(chan struct{}, defaultParallel)
	errChan := make(chan error)

	// make sure result is printed in correct order
	output := map[string]chan error{}
	for _, c := range containers {
		output[c] = make(chan error, 1)
	}
	go func() {
		for _, c := range containers {
			err := <-output[c]
			errChan <- err
		}
	}()

	go func() {
		for _, c := range containers {
			sem <- struct{}{} // Wait for active queue sem to drain.
			go func(container string) {
				output[container] <- op(ctx, container)
				<-sem
			}(c)
		}
	}()
	return errChan
}
