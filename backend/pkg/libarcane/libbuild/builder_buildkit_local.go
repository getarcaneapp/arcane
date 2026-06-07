package libbuild

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	docker "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	buildkitclient "github.com/moby/buildkit/client"
)

func (b *builder) newLocalBuildkitSessionInternal(ctx context.Context) (*buildSession, error) {
	if b.dockerClientProvider == nil {
		return nil, fmt.Errorf("docker service not available")
	}

	dockerClient, err := b.dockerClientProvider.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	bk, err := buildkitclient.New(ctx, "", docker.ClientOpts(dockerClient)...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker BuildKit: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := bk.Wait(waitCtx); err != nil {
		_ = bk.Close()
		return nil, fmt.Errorf("failed to wait for Docker BuildKit: %w", err)
	}

	return &buildSession{
		Client: bk,
		Close: func(_ error) error {
			return bk.Close()
		},
	}, nil
}

func requiresLocalBuildkitInternal(req imagetypes.BuildRequest) (bool, error) {
	fsInput, err := prepareBuildFilesystemInputInternal(req)
	if err != nil {
		return false, err
	}

	contents, err := readDockerfileContentsInternal(fsInput)
	if err != nil {
		return false, err
	}

	return dockerfileRequiresBuildkitInternal(contents), nil
}

func readDockerfileContentsInternal(input buildFilesystemInput) (string, error) {
	if input.dockerfileInline != "" {
		return input.dockerfileInline, nil
	}

	raw, err := os.ReadFile(input.fullDockerfilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read Dockerfile: %w", err)
	}

	return string(raw), nil
}

func dockerfileRequiresBuildkitInternal(contents string) bool {
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "# syntax=") {
			return true
		}

		if strings.HasPrefix(lower, "#") {
			continue
		}

		if strings.HasPrefix(lower, "run ") {
			if strings.Contains(lower, "--mount=") || strings.Contains(lower, "--network=") || strings.Contains(lower, "--security=") {
				return true
			}
		}

		if (strings.HasPrefix(lower, "copy ") || strings.HasPrefix(lower, "add ")) && strings.Contains(lower, "--link") {
			return true
		}
	}

	return false
}
