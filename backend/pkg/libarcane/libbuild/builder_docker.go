package libbuild

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/build"
	dockerimage "github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/moby/go-archive"
)

type dockerProgressDetail struct {
	Current int64 `json:"current,omitempty"`
	Total   int64 `json:"total,omitempty"`
}

type dockerStreamMessage struct {
	Stream         string                `json:"stream,omitempty"`
	Status         string                `json:"status,omitempty"`
	ID             string                `json:"id,omitempty"`
	ProgressDetail *dockerProgressDetail `json:"progressDetail,omitempty"`
	Error          string                `json:"error,omitempty"`
	ErrorDetail    *struct {
		Message string `json:"message,omitempty"`
	} `json:"errorDetail,omitempty"`
}

type dockerBuildInput struct {
	contextDir    string
	relDockerfile string
	platform      string
	buildArgs     map[string]*string
}

func prepareDockerBuildInput(req imagetypes.BuildRequest) (dockerBuildInput, bool, error) {
	contextDir := filepath.Clean(req.ContextDir)
	if contextDir == "" {
		return dockerBuildInput{}, false, errors.New("contextDir is required")
	}

	if len(req.Platforms) > 1 {
		return dockerBuildInput{}, true, fmt.Errorf("docker build fallback does not support multi-platform builds")
	}

	platform := ""
	if len(req.Platforms) == 1 {
		platform = strings.TrimSpace(req.Platforms[0])
	}

	dockerfilePath := strings.TrimSpace(req.Dockerfile)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	fullDockerfilePath := dockerfilePath
	if !filepath.IsAbs(dockerfilePath) {
		fullDockerfilePath = filepath.Join(contextDir, dockerfilePath)
	}

	relDockerfile, relErr := filepath.Rel(contextDir, fullDockerfilePath)
	if relErr != nil || strings.HasPrefix(relDockerfile, "..") {
		return dockerBuildInput{}, true, fmt.Errorf("docker build fallback requires Dockerfile to be inside the build context")
	}
	relDockerfile = filepath.ToSlash(relDockerfile)

	buildArgs := map[string]*string{}
	for key, val := range req.BuildArgs {
		v := val
		buildArgs[key] = &v
	}

	return dockerBuildInput{
		contextDir:    contextDir,
		relDockerfile: relDockerfile,
		platform:      platform,
		buildArgs:     buildArgs,
	}, false, nil
}

func createBuildContext(contextDir string) (io.ReadCloser, error) {
	excludes := readDockerignore(contextDir)
	return archive.TarWithOptions(contextDir, &archive.TarOptions{ExcludePatterns: excludes})
}

func (b *builder) performDockerBuild(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	buildContext io.Reader,
	buildOpts build.ImageBuildOptions,
	progressWriter io.Writer,
	serviceName string,
) error {
	resp, err := dockerClient.ImageBuild(ctx, buildContext, buildOpts)
	if err != nil {
		writeProgressEvent(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: err.Error()})
		return err
	}
	defer resp.Body.Close()

	return streamDockerMessages(ctx, resp.Body, progressWriter, serviceName)
}

func (b *builder) pushDockerImages(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	tags []string,
	progressWriter io.Writer,
	serviceName string,
) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			continue
		}
		writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
			Type:    "build",
			Service: serviceName,
			Status:  fmt.Sprintf("pushing %s", tag),
		})
		pushOptions := dockerimage.PushOptions{}
		if b.registryAuthProvider != nil {
			authHeader, authErr := b.registryAuthProvider.GetRegistryAuthForImage(ctx, tag)
			if authErr != nil {
				writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
					Type:    "build",
					Service: serviceName,
					Status:  fmt.Sprintf("registry auth unavailable for %s", tag),
				})
			} else if authHeader != "" {
				pushOptions.RegistryAuth = authHeader
			}
		}
		pushResp, pushErr := dockerClient.ImagePush(ctx, tag, pushOptions)
		if pushErr != nil {
			writeProgressEvent(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: pushErr.Error()})
			return pushErr
		}
		if pushResp != nil {
			if err := streamDockerMessages(ctx, pushResp, progressWriter, serviceName); err != nil {
				_ = pushResp.Close()
				return err
			}
			_ = pushResp.Close()
		}
	}
	return nil
}

func (b *builder) buildWithDocker(ctx context.Context, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string) (*imagetypes.BuildResult, error) {
	if b.dockerClientProvider == nil {
		return nil, errors.New("docker service not available")
	}

	dockerClient, err := b.dockerClientProvider.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	input, reportProgress, err := prepareDockerBuildInput(req)
	if err != nil {
		if reportProgress {
			writeProgressEvent(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: err.Error()})
		}
		return nil, err
	}

	buildContext, err := createBuildContext(input.contextDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create build context: %w", err)
	}
	defer buildContext.Close()

	writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
		Type:    "build",
		Phase:   "begin",
		Service: serviceName,
		Status:  "build started",
	})

	buildOpts := build.ImageBuildOptions{
		Tags:        req.Tags,
		Dockerfile:  input.relDockerfile,
		Remove:      true,
		ForceRemove: true,
		Target:      strings.TrimSpace(req.Target),
		BuildArgs:   input.buildArgs,
	}
	if input.platform != "" {
		buildOpts.Platform = input.platform
	}

	if err := b.performDockerBuild(ctx, dockerClient, buildContext, buildOpts, progressWriter, serviceName); err != nil {
		return nil, err
	}

	if req.Push {
		if err := b.pushDockerImages(ctx, dockerClient, req.Tags, progressWriter, serviceName); err != nil {
			return nil, err
		}
	}

	writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
		Type:    "build",
		Phase:   "complete",
		Service: serviceName,
		Status:  "build complete",
	})

	return &imagetypes.BuildResult{
		Provider: "local",
		Tags:     req.Tags,
	}, nil
}

func streamDockerMessages(ctx context.Context, reader io.Reader, w io.Writer, serviceName string) error {
	decoder := json.NewDecoder(reader)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var msg dockerStreamMessage
		if err := decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		errMsg := strings.TrimSpace(msg.Error)
		if errMsg == "" && msg.ErrorDetail != nil {
			errMsg = strings.TrimSpace(msg.ErrorDetail.Message)
		}
		if errMsg != "" {
			writeProgressEvent(w, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: errMsg})
			return errors.New(errMsg)
		}

		status := strings.TrimSpace(msg.Status)
		if status == "" {
			status = strings.TrimSpace(msg.Stream)
		}
		if status == "" {
			continue
		}

		event := imagetypes.ProgressEvent{
			Type:    "build",
			Service: serviceName,
			ID:      strings.TrimSpace(msg.ID),
			Status:  status,
		}
		if msg.ProgressDetail != nil {
			event.ProgressDetail = &imagetypes.ProgressDetail{
				Current: msg.ProgressDetail.Current,
				Total:   msg.ProgressDetail.Total,
			}
		}
		writeProgressEvent(w, event)
	}
}

func readDockerignore(contextDir string) []string {
	ignorePath := filepath.Join(contextDir, ".dockerignore")
	file, err := os.Open(ignorePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	patterns := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}

	return patterns
}
