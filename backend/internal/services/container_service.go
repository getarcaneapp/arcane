package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	containertypes "go.getarcane.app/types/container"
	"go.getarcane.app/types/containerregistry"
)

type ContainerService struct {
	db            *database.DB
	dockerService *DockerClientService
	eventService  *EventService
	imageService  *ImageService
}

func NewContainerService(db *database.DB, eventService *EventService, dockerService *DockerClientService, imageService *ImageService) *ContainerService {
	return &ContainerService{db: db, eventService: eventService, dockerService: dockerService, imageService: imageService}
}

func (s *ContainerService) StartContainer(ctx context.Context, containerID string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "start"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      "start",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerStart, containerID, "name", user.ID, user.Username, "0", metadata)

	if err != nil {
		fmt.Printf("Could not log container start action: %s\n", err)
	}

	err = dockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "start"})
	}
	return err
}

func (s *ContainerService) StopContainer(ctx context.Context, containerID string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "stop"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      "stop",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerStop, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	timeout := 30
	err = dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "stop"})
	}
	return err
}

func (s *ContainerService) RestartContainer(ctx context.Context, containerID string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "restart"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      "restart",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerRestart, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	err = dockerClient.ContainerRestart(ctx, containerID, container.StopOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "restart"})
	}
	return err
}

func (s *ContainerService) GetContainerByID(ctx context.Context, id string) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	container, err := dockerClient.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("container not found: %w", err)
	}

	return &container, nil
}

func (s *ContainerService) DeleteContainer(ctx context.Context, containerID string, force bool, removeVolumes bool, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Get container mounts before deletion if we need to remove volumes
	var volumesToRemove []string
	if removeVolumes {
		containerJSON, inspectErr := dockerClient.ContainerInspect(ctx, containerID)
		if inspectErr == nil {
			for _, mount := range containerJSON.Mounts {
				// Only collect named volumes (not bind mounts or tmpfs)
				if mount.Type == "volume" && mount.Name != "" {
					volumesToRemove = append(volumesToRemove, mount.Name)
				}
			}
		}
	}

	err = dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
		RemoveLinks:   false,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return fmt.Errorf("failed to delete container: %w", err)
	}

	// Remove named volumes if requested
	if removeVolumes && len(volumesToRemove) > 0 {
		for _, volumeName := range volumesToRemove {
			if removeErr := dockerClient.VolumeRemove(ctx, volumeName, false); removeErr != nil {
				// Log but don't fail if volume removal fails (might be in use by another container)
				s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", volumeName, "", user.ID, user.Username, "0", removeErr, models.JSON{"action": "delete", "container": containerID})
			}
		}
	}

	metadata := models.JSON{
		"action":      "delete",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDelete, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	return nil
}

func (s *ContainerService) CreateContainer(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string, user models.User, credentials []containerregistry.Credential) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.ImageInspect(ctx, config.Image)
	if err != nil {
		// Image not found locally, need to pull it
		pullOptions, authErr := s.imageService.getPullOptionsWithAuth(ctx, config.Image, credentials)
		if authErr != nil {
			slog.WarnContext(ctx, "Failed to get registry authentication for container image; proceeding without auth",
				"image", config.Image,
				"error", authErr.Error())
			pullOptions = image.PullOptions{}
		}

		reader, pullErr := dockerClient.ImagePull(ctx, config.Image, pullOptions)
		if pullErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, models.JSON{"action": "create", "image": config.Image, "step": "pull_image"})
			return nil, fmt.Errorf("failed to pull image %s: %w", config.Image, pullErr)
		}
		defer reader.Close()

		_, copyErr := io.Copy(io.Discard, reader)
		if copyErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", copyErr, models.JSON{"action": "create", "image": config.Image, "step": "complete_pull"})
			return nil, fmt.Errorf("failed to complete image pull: %w", copyErr)
		}
	}

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, containerName)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "create"})
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	metadata := models.JSON{
		"action":      "create",
		"containerId": resp.ID,
	}

	if logErr := s.eventService.LogContainerEvent(ctx, models.EventTypeContainerCreate, resp.ID, "name", user.ID, user.Username, "0", metadata); logErr != nil {
		fmt.Printf("Could not log container stop action: %s\n", logErr)
	}

	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = dockerClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "start"})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	containerJSON, err := dockerClient.ContainerInspect(ctx, resp.ID)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "inspect"})
		return nil, fmt.Errorf("failed to inspect created container: %w", err)
	}

	return &containerJSON, nil
}

func (s *ContainerService) StreamStats(ctx context.Context, containerID string, statsChan chan<- interface{}) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	stats, err := dockerClient.ContainerStats(ctx, containerID, true)
	if err != nil {
		return fmt.Errorf("failed to start stats stream: %w", err)
	}
	defer stats.Body.Close()

	decoder := json.NewDecoder(stats.Body)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var statsData interface{}
			if err := decoder.Decode(&statsData); err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("failed to decode stats: %w", err)
			}

			select {
			case statsChan <- statsData:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (s *ContainerService) StreamLogs(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
	}

	logs, err := dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logs.Close()

	if follow {
		return s.streamMultiplexedLogs(ctx, logs, logsChan)
	}

	return s.readAllLogs(logs, logsChan)
}

func (s *ContainerService) streamMultiplexedLogs(ctx context.Context, logs io.ReadCloser, logsChan chan<- string) error {
	// Use stdcopy to demultiplex Docker's stream format
	// Docker multiplexes stdout and stderr in a special format
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	// Start demultiplexing in a goroutine
	go func() {
		defer stdoutWriter.Close()
		defer stderrWriter.Close()
		_, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, logs)
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Printf("Error demultiplexing logs: %v\n", err)
		}
	}()

	// Read from both stdout and stderr concurrently
	done := make(chan error, 2)

	// Read stdout
	go func() {
		done <- s.readLogsFromReader(ctx, stdoutReader, logsChan, "stdout")
	}()

	// Read stderr
	go func() {
		done <- s.readLogsFromReader(ctx, stderrReader, logsChan, "stderr")
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		// Wait for the other goroutine or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}
}

// readLogsFromReader reads logs line by line from a reader
func (s *ContainerService) readLogsFromReader(ctx context.Context, reader io.Reader, logsChan chan<- string, source string) error {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			line := scanner.Text()
			if line != "" {
				// Add source prefix for stderr logs
				if source == "stderr" {
					line = "[STDERR] " + line
				}

				select {
				case logsChan <- line:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return scanner.Err()
}

func (s *ContainerService) readAllLogs(logs io.ReadCloser, logsChan chan<- string) error {
	stdoutBuf := &strings.Builder{}
	stderrBuf := &strings.Builder{}

	_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, logs)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to demultiplex logs: %w", err)
	}

	// Send stdout lines
	if stdoutBuf.Len() > 0 {
		lines := strings.Split(strings.TrimRight(stdoutBuf.String(), "\n"), "\n")
		for _, line := range lines {
			if line != "" {
				logsChan <- line
			}
		}
	}

	// Send stderr lines with prefix
	if stderrBuf.Len() > 0 {
		lines := strings.Split(strings.TrimRight(stderrBuf.String(), "\n"), "\n")
		for _, line := range lines {
			if line != "" {
				logsChan <- "[STDERR] " + line
			}
		}
	}

	return nil
}

func (s *ContainerService) ListContainersPaginated(ctx context.Context, params pagination.QueryParams, includeAll bool) ([]containertypes.Summary, pagination.Response, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	dockerContainers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: includeAll})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list Docker containers: %w", err)
	}

	items := make([]containertypes.Summary, 0, len(dockerContainers))
	for _, dc := range dockerContainers {
		items = append(items, containertypes.NewSummary(dc))
	}

	config := pagination.Config[containertypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[containertypes.Summary]{
			func(c containertypes.Summary) (string, error) {
				if len(c.Names) > 0 {
					return c.Names[0], nil
				}
				return "", nil
			},
			func(c containertypes.Summary) (string, error) { return c.Image, nil },
			func(c containertypes.Summary) (string, error) { return c.State, nil },
			func(c containertypes.Summary) (string, error) { return c.Status, nil },
		},
		SortBindings: []pagination.SortBinding[containertypes.Summary]{
			{
				Key: "name",
				Fn: func(a, b containertypes.Summary) int {
					nameA := ""
					if len(a.Names) > 0 {
						nameA = a.Names[0]
					}
					nameB := ""
					if len(b.Names) > 0 {
						nameB = b.Names[0]
					}
					return strings.Compare(nameA, nameB)
				},
			},
			{
				Key: "image",
				Fn: func(a, b containertypes.Summary) int {
					return strings.Compare(a.Image, b.Image)
				},
			},
			{
				Key: "state",
				Fn: func(a, b containertypes.Summary) int {
					return strings.Compare(a.State, b.State)
				},
			},
			{
				Key: "status",
				Fn: func(a, b containertypes.Summary) int {
					return strings.Compare(a.Status, b.Status)
				},
			},
			{
				Key: "created",
				Fn: func(a, b containertypes.Summary) int {
					if a.Created < b.Created {
						return -1
					}
					if a.Created > b.Created {
						return 1
					}
					return 0
				},
			},
		},
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)

	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (int64(result.TotalCount) + int64(params.Limit) - 1) / int64(params.Limit)
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Start / params.Limit) + 1
	}

	paginationResp := pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      int64(result.TotalCount),
		CurrentPage:     page,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: int64(result.TotalAvailable),
	}

	return result.Items, paginationResp, nil
}

// CreateExec creates an exec instance in the container
func (s *ContainerService) CreateExec(ctx context.Context, containerID string, cmd []string) (string, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
	}

	execResp, err := dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	return execResp.ID, nil
}

// AttachExec attaches to an exec instance and returns stdin, stdout/stderr streams
func (s *ContainerService) AttachExec(ctx context.Context, execID string) (io.WriteCloser, io.Reader, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	execAttach, err := dockerClient.ContainerExecAttach(ctx, execID, container.ExecAttachOptions{
		Tty: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to attach to exec: %w", err)
	}

	return execAttach.Conn, execAttach.Reader, nil
}
