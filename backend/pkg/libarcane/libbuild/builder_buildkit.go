package libbuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	buildkit "github.com/moby/buildkit/client"
)

func (b *builder) buildSolveOpt(ctx context.Context, req imagetypes.BuildRequest) (buildkit.SolveOpt, <-chan error, error) {
	contextDir := filepath.Clean(req.ContextDir)

	dockerfilePath := strings.TrimSpace(req.Dockerfile)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	fullDockerfilePath := dockerfilePath
	if !filepath.IsAbs(dockerfilePath) {
		fullDockerfilePath = filepath.Join(contextDir, dockerfilePath)
	}

	frontendAttrs := map[string]string{
		"filename": filepath.Base(fullDockerfilePath),
	}
	if strings.TrimSpace(req.Target) != "" {
		frontendAttrs["target"] = strings.TrimSpace(req.Target)
	}
	if len(req.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(req.Platforms, ",")
	}
	for key, val := range req.BuildArgs {
		frontendAttrs[fmt.Sprintf("build-arg:%s", key)] = val
	}

	solveOpt := buildkit.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalDirs: map[string]string{
			"context":    contextDir,
			"dockerfile": filepath.Dir(fullDockerfilePath),
		},
	}

	var loadErrCh chan error
	exports := make([]buildkit.ExportEntry, 0, 2)
	if req.Push {
		exports = append(exports, buildkit.ExportEntry{
			Type: "image",
			Attrs: map[string]string{
				"name":           strings.Join(req.Tags, ","),
				"push":           "true",
				"oci-mediatypes": "true",
			},
		})
	}
	if req.Load {
		exportEntry, errCh, err := b.buildLoadExport(ctx, req.Tags)
		if err != nil {
			return buildkit.SolveOpt{}, nil, err
		}
		loadErrCh = errCh
		exports = append(exports, exportEntry)
	}

	if len(exports) > 0 {
		solveOpt.Exports = exports
	}

	return solveOpt, loadErrCh, nil
}

func (b *builder) buildLoadExport(ctx context.Context, tags []string) (buildkit.ExportEntry, chan error, error) {
	if b.dockerClientProvider == nil {
		return buildkit.ExportEntry{}, nil, errors.New("docker service not available")
	}

	dockerClient, err := b.dockerClientProvider.GetClient()
	if err != nil {
		return buildkit.ExportEntry{}, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	pr, pw := io.Pipe()
	loadErrCh := make(chan error, 1)
	go func() {
		defer pr.Close()
		_, loadErr := dockerClient.ImageLoad(ctx, pr)
		loadErrCh <- loadErr
	}()

	exportAttrs := map[string]string{}
	if len(tags) > 0 {
		exportAttrs["name"] = strings.Join(tags, ",")
	}

	return buildkit.ExportEntry{
		Type:  "docker",
		Attrs: exportAttrs,
		Output: func(_ map[string]string) (io.WriteCloser, error) {
			return pw, nil
		},
	}, loadErrCh, nil
}

func streamSolveStatus(ctx context.Context, ch <-chan *buildkit.SolveStatus, w io.Writer, serviceName string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case status, ok := <-ch:
			if !ok {
				return nil
			}
			if status == nil {
				continue
			}
			for _, s := range status.Statuses {
				if s == nil {
					continue
				}
				event := imagetypes.ProgressEvent{
					Type:    "build",
					Service: serviceName,
					ID:      s.ID,
					Status:  s.Name,
				}
				if s.Current > 0 || s.Total > 0 {
					event.ProgressDetail = &imagetypes.ProgressDetail{
						Current: s.Current,
						Total:   s.Total,
					}
				}
				writeProgressEvent(w, event)
			}
		}
	}
}
