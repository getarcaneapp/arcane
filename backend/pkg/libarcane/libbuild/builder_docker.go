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
	"strconv"
	"strings"

	dockerutils "github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/moby/go-archive"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	dockerregistry "github.com/moby/moby/api/types/registry"
	dockerclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type dockerBuildInput struct {
	contextDir           string
	fullDockerfilePath   string
	relDockerfile        string
	dockerfileOutsideCtx bool
	platform             string
	buildArgs            map[string]*string
	labels               map[string]string
	cacheFrom            []string
	noCache              bool
	pullParent           bool
	networkMode          string
	isolation            string
	shmSize              int64
	ulimits              []*dockercontainer.Ulimit
	extraHosts           []string
}

func parseUlimitsInternal(values map[string]string) []*dockercontainer.Ulimit {
	if len(values) == 0 {
		return nil
	}

	out := make([]*dockercontainer.Ulimit, 0, len(values))
	for name, raw := range values {
		name = strings.TrimSpace(name)
		raw = strings.TrimSpace(raw)
		if name == "" || raw == "" {
			continue
		}

		parts := strings.Split(raw, ":")
		if len(parts) == 1 {
			single, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			if err != nil {
				continue
			}
			out = append(out, &dockercontainer.Ulimit{Name: name, Soft: single, Hard: single})
			continue
		}

		soft, softErr := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		hard, hardErr := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if softErr != nil || hardErr != nil {
			continue
		}

		out = append(out, &dockercontainer.Ulimit{Name: name, Soft: soft, Hard: hard})
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func prepareDockerBuildInputInternal(req imagetypes.BuildRequest) (dockerBuildInput, bool, error) {
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
	dockerfileOutsideCtx := relErr != nil || strings.HasPrefix(relDockerfile, "..")
	if dockerfileOutsideCtx {
		relDockerfile = filepath.Base(fullDockerfilePath)
	} else {
		relDockerfile = filepath.ToSlash(relDockerfile)
	}

	buildArgs := map[string]*string{}
	for key, val := range req.BuildArgs {
		v := val
		buildArgs[key] = &v
	}

	labels := map[string]string{}
	for key, val := range req.Labels {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		labels[k] = val
	}
	if len(labels) == 0 {
		labels = nil
	}

	cacheFrom := make([]string, 0, len(req.CacheFrom))
	for _, source := range req.CacheFrom {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		cacheFrom = append(cacheFrom, source)
	}
	if len(cacheFrom) == 0 {
		cacheFrom = nil
	}

	extraHosts := make([]string, 0, len(req.ExtraHosts))
	for _, host := range req.ExtraHosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		extraHosts = append(extraHosts, host)
	}
	if len(extraHosts) == 0 {
		extraHosts = nil
	}

	return dockerBuildInput{
		contextDir:           contextDir,
		fullDockerfilePath:   fullDockerfilePath,
		relDockerfile:        relDockerfile,
		dockerfileOutsideCtx: dockerfileOutsideCtx,
		platform:             platform,
		buildArgs:            buildArgs,
		labels:               labels,
		cacheFrom:            cacheFrom,
		noCache:              req.NoCache,
		pullParent:           req.Pull,
		networkMode:          strings.TrimSpace(req.Network),
		isolation:            strings.TrimSpace(req.Isolation),
		shmSize:              req.ShmSize,
		ulimits:              parseUlimitsInternal(req.Ulimits),
		extraHosts:           extraHosts,
	}, false, nil
}

func createBuildContextInternal(contextDir string) (io.ReadCloser, error) {
	excludes := readDockerignoreInternal(contextDir)
	return archive.TarWithOptions(contextDir, &archive.TarOptions{ExcludePatterns: excludes})
}

func copyFileWithModeInternal(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyContextTreeInternal(srcRoot, dstRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(dstRoot, relPath)
		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		if d.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		return copyFileWithModeInternal(path, targetPath, info.Mode())
	})
}

func prepareDockerBuildContextInternal(input dockerBuildInput) (string, string, func(), error) {
	if !input.dockerfileOutsideCtx {
		return input.contextDir, input.relDockerfile, func() {}, nil
	}

	stagingDir, err := os.MkdirTemp("", "arcane-docker-build-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create temporary build directory: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(stagingDir)
	}

	if err := copyContextTreeInternal(input.contextDir, stagingDir); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to stage build context: %w", err)
	}

	const stagedDockerfile = ".arcane.external.Dockerfile"
	stagedDockerfilePath := filepath.Join(stagingDir, stagedDockerfile)
	dockerfileInfo, err := os.Stat(input.fullDockerfilePath)
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to stat Dockerfile: %w", err)
	}

	if err := copyFileWithModeInternal(input.fullDockerfilePath, stagedDockerfilePath, dockerfileInfo.Mode()); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to stage Dockerfile: %w", err)
	}

	return stagingDir, stagedDockerfile, cleanup, nil
}

func (b *builder) performDockerBuildInternal(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	buildContext io.Reader,
	buildOpts dockerclient.ImageBuildOptions,
	progressWriter io.Writer,
	serviceName string,
) error {
	resp, err := dockerClient.ImageBuild(ctx, buildContext, buildOpts)
	if err != nil {
		writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: err.Error()})
		return err
	}
	defer resp.Body.Close()

	return streamDockerMessagesInternal(ctx, resp.Body, progressWriter, serviceName)
}

func (b *builder) pushDockerImagesInternal(
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
		writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
			Type:    "build",
			Service: serviceName,
			Status:  fmt.Sprintf("pushing %s", tag),
		})
		pushOptions := dockerclient.ImagePushOptions{}
		if b.registryAuthProvider != nil {
			authHeader, authErr := b.registryAuthProvider.GetRegistryAuthForImage(ctx, tag)
			if authErr != nil {
				writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
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
			writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: pushErr.Error()})
			return pushErr
		}
		if pushResp != nil {
			if err := streamDockerMessagesInternal(ctx, pushResp, progressWriter, serviceName); err != nil {
				_ = pushResp.Close()
				return err
			}
			_ = pushResp.Close()
		}
	}
	return nil
}

func parseOCIPlatformInternal(value string) (ocispec.Platform, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) < 2 {
		return ocispec.Platform{}, fmt.Errorf("invalid platform: %q", value)
	}

	platform := ocispec.Platform{
		OS:           strings.TrimSpace(parts[0]),
		Architecture: strings.TrimSpace(parts[1]),
	}
	if len(parts) >= 3 {
		platform.Variant = strings.TrimSpace(parts[2])
	}

	if platform.OS == "" || platform.Architecture == "" {
		return ocispec.Platform{}, fmt.Errorf("invalid platform: %q", value)
	}

	return platform, nil
}

func buildDockerImageOptionsInternal(
	req imagetypes.BuildRequest,
	input dockerBuildInput,
	dockerfileForBuild string,
	authConfigs map[string]dockerregistry.AuthConfig,
) (dockerclient.ImageBuildOptions, error) {
	buildOpts := dockerclient.ImageBuildOptions{
		Tags:        req.Tags,
		Dockerfile:  dockerfileForBuild,
		Remove:      true,
		ForceRemove: true,
		Target:      strings.TrimSpace(req.Target),
		BuildArgs:   input.buildArgs,
		Labels:      input.labels,
		CacheFrom:   input.cacheFrom,
		NoCache:     input.noCache,
		PullParent:  input.pullParent,
		NetworkMode: input.networkMode,
		Isolation:   dockercontainer.Isolation(input.isolation),
		ShmSize:     input.shmSize,
		Ulimits:     input.ulimits,
		ExtraHosts:  input.extraHosts,
		AuthConfigs: authConfigs,
	}
	if len(buildOpts.AuthConfigs) == 0 {
		buildOpts.AuthConfigs = nil
	}

	if input.platform != "" {
		platform, parseErr := parseOCIPlatformInternal(input.platform)
		if parseErr != nil {
			return dockerclient.ImageBuildOptions{}, parseErr
		}
		buildOpts.Platforms = []ocispec.Platform{platform}
	}

	return buildOpts, nil
}

func (b *builder) buildWithDockerInternal(ctx context.Context, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string) (*imagetypes.BuildResult, error) {
	if b.dockerClientProvider == nil {
		return nil, errors.New("docker service not available")
	}

	dockerClient, err := b.dockerClientProvider.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	input, reportProgress, err := prepareDockerBuildInputInternal(req)
	if err != nil {
		if reportProgress {
			writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: err.Error()})
		}
		return nil, err
	}

	buildContextDir, dockerfileForBuild, cleanupBuildContext, err := prepareDockerBuildContextInternal(input)
	if err != nil {
		return nil, err
	}
	defer cleanupBuildContext()

	buildContext, err := createBuildContextInternal(buildContextDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create build context: %w", err)
	}
	defer buildContext.Close()

	writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
		Type:    "build",
		Phase:   "begin",
		Service: serviceName,
		Status:  "build started",
	})

	var authConfigs map[string]dockerregistry.AuthConfig
	if b.registryAuthProvider != nil {
		loadedAuthConfigs, authErr := b.registryAuthProvider.GetAllRegistryAuthConfigs(ctx)
		if authErr != nil {
			writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
				Type:    "build",
				Service: serviceName,
				Status:  "registry auth unavailable for build",
			})
		} else {
			authConfigs = loadedAuthConfigs
		}
	}

	buildOpts, buildOptsErr := buildDockerImageOptionsInternal(req, input, dockerfileForBuild, authConfigs)
	if buildOptsErr != nil {
		writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: buildOptsErr.Error()})
		return nil, buildOptsErr
	}

	if err := b.performDockerBuildInternal(ctx, dockerClient, buildContext, buildOpts, progressWriter, serviceName); err != nil {
		return nil, err
	}

	if req.Push {
		if err := b.pushDockerImagesInternal(ctx, dockerClient, req.Tags, progressWriter, serviceName); err != nil {
			return nil, err
		}
	}

	writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
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

func streamDockerMessagesInternal(ctx context.Context, reader io.Reader, w io.Writer, serviceName string) error {
	streamErr := dockerutils.ConsumeJSONMessageStream(reader, func(line []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var msg jsonstream.Message
		if unmarshalErr := json.Unmarshal(line, &msg); unmarshalErr != nil {
			return unmarshalErr
		}
		status := strings.TrimSpace(msg.Status)
		if status == "" {
			status = strings.TrimSpace(msg.Stream)
		}
		if status == "" {
			return nil
		}

		event := imagetypes.ProgressEvent{
			Type:    "build",
			Service: serviceName,
			ID:      strings.TrimSpace(msg.ID),
			Status:  status,
		}
		if msg.Progress != nil {
			event.ProgressDetail = &imagetypes.ProgressDetail{
				Current: msg.Progress.Current,
				Total:   msg.Progress.Total,
			}
		}
		writeProgressEventInternal(w, event)
		return nil
	})
	if streamErr != nil {
		errMsg := strings.TrimSpace(streamErr.Error())
		if errMsg == "" {
			errMsg = "docker stream reported an unknown error"
		}
		writeProgressEventInternal(w, imagetypes.ProgressEvent{Type: "build", Service: serviceName, Error: errMsg})
		return streamErr
	}
	return nil
}

func readDockerignoreInternal(contextDir string) []string {
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
