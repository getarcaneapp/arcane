package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const rusticImage = volumehelper.DefaultToolsImage

type rusticRepositoryInternal struct {
	environment []string
	mounts      []mount.Mount
}

type rusticSnapshotInternal struct {
	ID      string `json:"id"`
	Summary struct {
		TotalBytesProcessed int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

// RusticService executes backup repository operations through Arcane's tools image.
type RusticService struct {
	imageService *ImageService
	runMu        sync.Mutex
}

func NewRusticService(imageService *ImageService) *RusticService {
	return &RusticService{imageService: imageService}
}

func (s *RusticService) ensureImageInternal(ctx context.Context, dockerClient *client.Client) error {
	if _, err := dockerClient.ImageInspect(ctx, rusticImage); err == nil {
		return nil
	}
	if s.imageService == nil {
		return errors.New("image service is unavailable")
	}
	if err := s.imageService.PullImage(ctx, rusticImage, io.Discard, systemUser, nil); err != nil {
		return fmt.Errorf("failed to pull Arcane tools image for Rustic: %w", err)
	}
	return nil
}

func (s *RusticService) runInternal(ctx context.Context, dockerClient *client.Client, repository rusticRepositoryInternal, password string, command []string, extraMounts ...mount.Mount) (string, error) {
	if s == nil {
		return "", errors.New("rustic service is unavailable")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := s.ensureImageInternal(ctx, dockerClient); err != nil {
		return "", err
	}
	environment := append([]string{}, repository.environment...)
	environment = append(environment,
		"RUSTIC_PASSWORD="+password,
		"RUSTIC_NO_PROGRESS=true",
		"RUSTIC_LOG_LEVEL=error",
	)
	mounts := append([]mount.Mount{}, repository.mounts...)
	mounts = append(mounts, extraMounts...)
	hostConfig := volumehelper.HostConfig(rusticImage, nil, mounts)
	hostConfig.AutoRemove = false
	created, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      rusticImage,
			Entrypoint: []string{"rustic"},
			Cmd:        command,
			Env:        environment,
			Labels:     volumehelper.Labels(),
		},
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Rustic container: %w", err)
	}
	defer func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), created.ID, volumehelper.RemoveOptions())
	}()
	if _, err := dockerClient.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start Rustic container: %w", err)
	}
	wait := dockerClient.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var status container.WaitResponse
	select {
	case err := <-wait.Error:
		if err != nil {
			return "", fmt.Errorf("failed to wait for Rustic container: %w", err)
		}
	case status = <-wait.Result:
	}
	logs, err := dockerClient.ContainerLogs(ctx, created.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", fmt.Errorf("failed to read Rustic output: %w", err)
	}
	defer func() { _ = logs.Close() }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		return "", fmt.Errorf("failed to decode Rustic output: %w", err)
	}
	if status.StatusCode != 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("rustic exited with code %d: %s", status.StatusCode, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}
