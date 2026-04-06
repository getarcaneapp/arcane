package libbuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	dockerutils "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	buildkit "github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"
)

func parseBuildkitCacheEntriesInternal(values []string) []buildkit.CacheOptionsEntry {
	entries := make([]buildkit.CacheOptionsEntry, 0, len(values))

	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		if !strings.Contains(raw, "type=") {
			entries = append(entries, buildkit.CacheOptionsEntry{
				Type:  "registry",
				Attrs: map[string]string{"ref": raw},
			})
			continue
		}

		entry := buildkit.CacheOptionsEntry{Attrs: map[string]string{}}
		for segment := range strings.SplitSeq(raw, ",") {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}

			parts := strings.SplitN(segment, "=", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key == "" || value == "" {
				continue
			}

			if key == "type" {
				entry.Type = value
				continue
			}

			entry.Attrs[key] = value
		}

		if entry.Type == "" {
			entry.Type = "registry"
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil
	}

	return entries
}

func normalizeEntitlementsInternal(entitlements []string, privileged bool) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(entitlements)+1)

	appendEntitlement := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	for _, entitlement := range entitlements {
		appendEntitlement(entitlement)
	}
	if privileged {
		appendEntitlement("security.insecure")
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func resolveBuildkitDockerfileMountInternal(contextDir, dockerfilePath string) (string, string) {
	dockerfileRelDir := filepath.Dir(filepath.FromSlash(dockerfilePath))
	if dockerfileRelDir == "." {
		return contextDir, dockerfilePath
	}

	return filepath.Join(contextDir, dockerfileRelDir), filepath.Base(dockerfilePath)
}

func createBuildkitLocalMountsInternal(contextDir, dockerfileDir string) (map[string]fsutil.FS, error) {
	contextMount, err := fsutil.NewFS(contextDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create context mount: %w", err)
	}

	if dockerfileDir == contextDir {
		return map[string]fsutil.FS{
			"context":    contextMount,
			"dockerfile": contextMount,
		}, nil
	}

	dockerfileMount, err := fsutil.NewFS(dockerfileDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create dockerfile mount: %w", err)
	}

	return map[string]fsutil.FS{
		"context":    contextMount,
		"dockerfile": dockerfileMount,
	}, nil
}

func (b *builder) buildSolveOptInternal(ctx context.Context, req imagetypes.BuildRequest, providerName string) (buildkit.SolveOpt, <-chan error, func(), error) {
	fsInput, err := prepareBuildFilesystemInputInternal(req)
	if err != nil {
		return buildkit.SolveOpt{}, nil, nil, err
	}

	contextDir, dockerfilePath, cleanup, err := prepareBuildContextInternal(fsInput)
	if err != nil {
		return buildkit.SolveOpt{}, nil, nil, err
	}

	dockerfileDir, dockerfileFilename := resolveBuildkitDockerfileMountInternal(contextDir, dockerfilePath)
	localMounts, err := createBuildkitLocalMountsInternal(contextDir, dockerfileDir)
	if err != nil {
		cleanup()
		return buildkit.SolveOpt{}, nil, nil, err
	}

	frontendAttrs := map[string]string{
		"filename": dockerfileFilename,
	}
	if strings.TrimSpace(req.Target) != "" {
		frontendAttrs["target"] = strings.TrimSpace(req.Target)
	}
	if req.NoCache {
		frontendAttrs["no-cache"] = ""
	}
	if req.Pull {
		frontendAttrs["image-resolve-mode"] = "pull"
	}
	if len(req.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(req.Platforms, ",")
	}
	for key, val := range req.BuildArgs {
		frontendAttrs[fmt.Sprintf("build-arg:%s", key)] = val
	}
	for key, val := range req.Labels {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		frontendAttrs[fmt.Sprintf("label:%s", k)] = val
	}

	solveOpt := buildkit.SolveOpt{
		Frontend:            "dockerfile.v0",
		FrontendAttrs:       frontendAttrs,
		LocalMounts:         localMounts,
		CacheImports:        parseBuildkitCacheEntriesInternal(req.CacheFrom),
		CacheExports:        parseBuildkitCacheEntriesInternal(req.CacheTo),
		AllowedEntitlements: normalizeEntitlementsInternal(req.Entitlements, req.Privileged),
	}

	var loadErrCh chan error
	exports := make([]buildkit.ExportEntry, 0, 2)
	if req.Push && providerName != "local" {
		exports = append(exports, buildkit.ExportEntry{
			Type: "image",
			Attrs: map[string]string{
				"name":           strings.Join(req.Tags, ","),
				"push":           "true",
				"oci-mediatypes": "true",
			},
		})
	}

	if providerName == "local" && (req.Load || req.Push) {
		attrs := map[string]string{
			"name":           strings.Join(req.Tags, ","),
			"oci-mediatypes": "true",
		}
		if req.Push {
			attrs["push"] = "true"
		}
		exports = append(exports, buildkit.ExportEntry{
			Type:  "image",
			Attrs: attrs,
		})
	} else if req.Load {
		exportEntry, errCh, err := b.buildLoadExportInternal(ctx, req.Tags)
		if err != nil {
			cleanup()
			return buildkit.SolveOpt{}, nil, nil, err
		}
		loadErrCh = errCh
		exports = append(exports, exportEntry)
	}

	if len(exports) > 0 {
		solveOpt.Exports = exports
	}

	return solveOpt, loadErrCh, cleanup, nil
}

func (b *builder) buildLoadExportInternal(ctx context.Context, tags []string) (buildkit.ExportEntry, chan error, error) {
	if b.dockerClientProvider == nil {
		return buildkit.ExportEntry{}, nil, errors.New("docker service not available")
	}

	dockerClient, err := b.dockerClientProvider.GetClient(ctx)
	if err != nil {
		return buildkit.ExportEntry{}, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	pr, pw := io.Pipe()
	loadErrCh := make(chan error, 1)
	go func() {
		defer pr.Close()
		loadResp, loadErr := dockerClient.ImageLoad(ctx, pr)
		if loadErr != nil {
			loadErrCh <- loadErr
			return
		}
		defer func() { _ = loadResp.Close() }()
		loadErrCh <- dockerutils.ConsumeJSONMessageStream(loadResp, nil)
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

func streamSolveStatusInternal(ctx context.Context, ch <-chan *buildkit.SolveStatus, w io.Writer, serviceName string) error {
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
				writeProgressEventInternal(w, event)
			}
		}
	}
}
