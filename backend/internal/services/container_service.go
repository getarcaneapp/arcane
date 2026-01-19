package services

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/backend/internal/utils/timeouts"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
)

type ContainerService struct {
	db              *database.DB
	dockerService   *DockerClientService
	eventService    *EventService
	imageService    *ImageService
	settingsService *SettingsService
}

func NewContainerService(db *database.DB, eventService *EventService, dockerService *DockerClientService, imageService *ImageService, settingsService *SettingsService) *ContainerService {
	return &ContainerService{
		db:              db,
		eventService:    eventService,
		dockerService:   dockerService,
		imageService:    imageService,
		settingsService: settingsService,
	}
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

		settings := s.settingsService.GetSettingsConfig()
		pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
		defer pullCancel()

		reader, pullErr := dockerClient.ImagePull(pullCtx, config.Image, pullOptions)
		if pullErr != nil {
			if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
				s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, models.JSON{"action": "create", "image": config.Image, "step": "pull_image_timeout"})
				return nil, fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", config.Image)
			}
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

func (s *ContainerService) ListContainersPaginated(ctx context.Context, params pagination.QueryParams, includeAll bool) ([]containertypes.Summary, pagination.Response, containertypes.StatusCounts, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, containertypes.StatusCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	dockerContainers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: includeAll})
	if err != nil {
		return nil, pagination.Response{}, containertypes.StatusCounts{}, fmt.Errorf("failed to list Docker containers: %w", err)
	}

	// Collect unique image IDs for update info lookup
	imageIDSet := make(map[string]struct{}, len(dockerContainers))
	for _, dc := range dockerContainers {
		if dc.ImageID != "" {
			imageIDSet[dc.ImageID] = struct{}{}
		}
	}
	imageIDs := make([]string, 0, len(imageIDSet))
	for id := range imageIDSet {
		imageIDs = append(imageIDs, id)
	}

	// Fetch update info for all images used by containers
	var updateInfoMap map[string]*imagetypes.UpdateInfo
	if s.imageService != nil && len(imageIDs) > 0 {
		updateInfoMap, err = s.imageService.GetUpdateInfoByImageIDs(ctx, imageIDs)
		if err != nil {
			// Log error but continue - update info is optional
			slog.WarnContext(ctx, "Failed to fetch image update info for containers", "error", err)
			updateInfoMap = make(map[string]*imagetypes.UpdateInfo)
		}
	} else {
		updateInfoMap = make(map[string]*imagetypes.UpdateInfo)
	}

	items := make([]containertypes.Summary, 0, len(dockerContainers))
	for _, dc := range dockerContainers {
		summary := containertypes.NewSummary(dc)
		// Attach update info if available
		if info, exists := updateInfoMap[dc.ImageID]; exists {
			summary.UpdateInfo = info
		}
		items = append(items, summary)
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

	// Calculate status counts from items (before pagination)
	counts := containertypes.StatusCounts{
		TotalContainers: len(items),
	}
	for _, c := range items {
		if c.State == "running" {
			counts.RunningContainers++
		} else {
			counts.StoppedContainers++
		}
	}

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

	return result.Items, paginationResp, counts, nil
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

// BrowseContainerFiles lists files and directories at the given path inside a container.
func (s *ContainerService) BrowseContainerFiles(ctx context.Context, containerID, dirPath string) (*containertypes.BrowseFilesResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Validate and sanitize path
	if dirPath == "" {
		dirPath = "/"
	}

	// Check if container is running
	inspect, err := dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if inspect.State != nil && inspect.State.Running {
		// Container is running - use fast exec-based listing
		return s.browseContainerFilesExec(ctx, dockerClient, containerID, dirPath)
	}

	// Container is stopped - use CopyFromContainer (slower but works)
	return s.browseContainerFilesTar(ctx, dockerClient, containerID, dirPath)
}

// browseContainerFilesExec lists files using exec (fast, requires running container).
func (s *ContainerService) browseContainerFilesExec(ctx context.Context, dockerClient *dockerclient.Client, containerID, dirPath string) (*containertypes.BrowseFilesResponse, error) {
	// Use ls command with specific format to get file information
	// -l for long format, -a for all files, --time-style for consistent date format
	cmd := []string{"ls", "-la", "--time-style=+%Y-%m-%dT%H:%M:%S", dirPath}

	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}

	execResp, err := dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	execAttach, err := dockerClient.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer execAttach.Close()

	// Read output
	var stdout, stderr strings.Builder
	_, err = stdcopy.StdCopy(&stdout, &stderr, execAttach.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check for errors
	if stderr.Len() > 0 {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" && !strings.Contains(errMsg, "cannot access") {
			return nil, fmt.Errorf("failed to list directory: %s", errMsg)
		}
	}

	// Parse ls output
	files := parseLsOutput(stdout.String(), dirPath)

	return &containertypes.BrowseFilesResponse{
		Path:  dirPath,
		Files: files,
	}, nil
}

// browseContainerFilesTar lists files using CopyFromContainer (works on stopped containers).
func (s *ContainerService) browseContainerFilesTar(ctx context.Context, dockerClient *dockerclient.Client, containerID, dirPath string) (*containertypes.BrowseFilesResponse, error) {
	// Ensure path ends with / for directory listing
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}

	// Use CopyFromContainer to get directory contents as a TAR stream
	tarStream, _, err := dockerClient.CopyFromContainer(ctx, containerID, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read container path: %w", err)
	}
	defer tarStream.Close()

	files, err := parseTarDirectory(tarStream, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse directory contents: %w", err)
	}

	return &containertypes.BrowseFilesResponse{
		Path:  strings.TrimSuffix(dirPath, "/"),
		Files: files,
	}, nil
}

// parseLsOutput parses the output of ls -la command.
func parseLsOutput(output, basePath string) []containertypes.FileEntry {
	var files []containertypes.FileEntry
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total") {
			continue
		}

		entry := parseLsLine(line, basePath)
		if entry != nil && entry.Name != "." && entry.Name != ".." {
			files = append(files, *entry)
		}
	}

	return files
}

// parseLsLine parses a single line from ls -la output.
func parseLsLine(line, basePath string) *containertypes.FileEntry {
	// Format: -rw-r--r-- 1 root root 1234 2024-01-15T10:30:00 filename
	// Or with symlink: lrwxrwxrwx 1 root root 12 2024-01-15T10:30:00 link -> target

	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil
	}

	mode := fields[0]
	// Size is at index 4 (after permissions, links, user, group)
	sizeStr := fields[4]
	// Date is at index 5
	modTime := fields[5]

	// Name starts at index 6, but may contain spaces
	nameIdx := 6
	if len(fields) <= nameIdx {
		return nil
	}

	name := strings.Join(fields[nameIdx:], " ")
	linkTarget := ""

	// Check for symlink arrow
	if arrowIdx := strings.Index(name, " -> "); arrowIdx != -1 {
		linkTarget = name[arrowIdx+4:]
		name = name[:arrowIdx]
	}

	// Determine file type from mode
	var fileType containertypes.FileEntryType
	switch mode[0] {
	case 'd':
		fileType = containertypes.FileEntryTypeDirectory
	case 'l':
		fileType = containertypes.FileEntryTypeSymlink
	default:
		fileType = containertypes.FileEntryTypeFile
	}

	// Parse size
	size, _ := parseInt64(sizeStr)

	// Build full path
	fullPath := basePath
	if !strings.HasSuffix(basePath, "/") {
		fullPath += "/"
	}
	fullPath += name

	return &containertypes.FileEntry{
		Name:       name,
		Path:       fullPath,
		Type:       fileType,
		Size:       size,
		Mode:       mode,
		ModTime:    modTime,
		LinkTarget: linkTarget,
	}
}

// parseInt64 parses a string to int64, returning 0 on error.
func parseInt64(s string) (int64, error) {
	var result int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid number: %s", s)
		}
		result = result*10 + int64(c-'0')
	}
	return result, nil
}

// parseTarDirectory reads a TAR stream and extracts only the immediate children of the directory.
func parseTarDirectory(tarStream io.Reader, basePath string) ([]containertypes.FileEntry, error) {
	tr := tar.NewReader(tarStream)
	filesMap := make(map[string]containertypes.FileEntry)

	// Normalize base path
	basePath = path.Clean(basePath)
	if basePath == "." {
		basePath = ""
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// The TAR from Docker includes the directory name as the root
		// e.g., for /etc/, entries are like "etc/passwd", "etc/hosts"
		entryPath := header.Name

		// Remove trailing slash for directories
		entryPath = strings.TrimSuffix(entryPath, "/")

		// Skip the root directory itself
		if entryPath == "" || entryPath == "." {
			continue
		}

		// Count path depth - we only want immediate children (depth 1)
		parts := strings.Split(entryPath, "/")

		// Skip if this is a nested file (depth > 1)
		// The first part is the directory name itself
		if len(parts) > 2 {
			continue
		}

		// Get the actual file/directory name
		var name string
		if len(parts) == 1 {
			name = parts[0]
		} else {
			name = parts[1]
		}

		// Skip . and ..
		if name == "." || name == ".." || name == "" {
			continue
		}

		// Skip if we've already seen this entry (can happen with directory entries)
		if _, exists := filesMap[name]; exists {
			continue
		}

		// Determine file type
		var fileType containertypes.FileEntryType
		switch header.Typeflag {
		case tar.TypeDir:
			fileType = containertypes.FileEntryTypeDirectory
		case tar.TypeSymlink:
			fileType = containertypes.FileEntryTypeSymlink
		default:
			fileType = containertypes.FileEntryTypeFile
		}

		// Build the full path
		fullPath := basePath
		if fullPath != "/" && fullPath != "" {
			fullPath += "/"
		} else if fullPath == "" {
			fullPath = "/"
		}
		fullPath += name

		// Convert file mode to string
		mode := header.FileInfo().Mode().String()

		filesMap[name] = containertypes.FileEntry{
			Name:       name,
			Path:       fullPath,
			Type:       fileType,
			Size:       header.Size,
			Mode:       mode,
			ModTime:    header.ModTime.Format("2006-01-02T15:04:05"),
			LinkTarget: header.Linkname,
		}
	}

	// Convert map to slice
	files := make([]containertypes.FileEntry, 0, len(filesMap))
	for _, entry := range filesMap {
		files = append(files, entry)
	}

	return files, nil
}

// GetContainerFileContent reads the content of a file inside a container.
// Uses Docker's CopyFromContainer API which works on both running and stopped containers.
func (s *ContainerService) GetContainerFileContent(ctx context.Context, containerID, filePath string) (*containertypes.FileContentResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Maximum file size to read (1MB)
	const maxFileSize = 1024 * 1024

	// Use CopyFromContainer to get the file as a TAR stream
	tarStream, stat, err := dockerClient.CopyFromContainer(ctx, containerID, filePath)
	if err != nil {
		return nil, fmt.Errorf("file not found or not accessible: %w", err)
	}
	defer tarStream.Close()

	// Check if this is a directory
	if stat.Mode.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	fileSize := stat.Size
	truncated := fileSize > maxFileSize

	// Read the TAR to get file content
	tr := tar.NewReader(tarStream)
	header, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("failed to read file from archive: %w", err)
	}

	// Determine how much to read
	readSize := fileSize
	if truncated {
		readSize = maxFileSize
	}

	// Read file content
	content := make([]byte, readSize)
	n, err := io.ReadFull(tr, content)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}
	content = content[:n]

	// Check if content is binary (contains null bytes or non-printable characters)
	isBinary := false
	for _, b := range content {
		if b == 0 || (b < 32 && b != '\n' && b != '\r' && b != '\t') {
			isBinary = true
			break
		}
	}

	// Handle symlinks - the header contains the actual file content
	if header.Typeflag == tar.TypeSymlink {
		// For symlinks, return the link target as content
		return &containertypes.FileContentResponse{
			Path:      filePath,
			Content:   header.Linkname,
			Size:      int64(len(header.Linkname)),
			IsBinary:  false,
			Truncated: false,
		}, nil
	}

	return &containertypes.FileContentResponse{
		Path:      filePath,
		Content:   string(content),
		Size:      fileSize,
		IsBinary:  isBinary,
		Truncated: truncated,
	}, nil
}
