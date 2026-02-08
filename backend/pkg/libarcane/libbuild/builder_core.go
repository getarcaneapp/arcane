package libbuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/getarcaneapp/arcane/backend/internal/utils/timeouts"
	buildtypes "github.com/getarcaneapp/arcane/types/builds"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session/auth/authprovider"
)

type builder struct {
	settings             buildtypes.SettingsProvider
	dockerClientProvider buildtypes.DockerClientProvider
	registryAuthProvider buildtypes.RegistryAuthProvider
	providers            map[string]interface{}
}

func NewBuilder(settings buildtypes.SettingsProvider, dockerClientProvider buildtypes.DockerClientProvider, registryAuthProvider buildtypes.RegistryAuthProvider) buildtypes.Builder {
	providers := map[string]interface{}{
		"depot": newDepotBuildKitProvider(settings),
	}

	return &builder{
		settings:             settings,
		dockerClientProvider: dockerClientProvider,
		registryAuthProvider: registryAuthProvider,
		providers:            providers,
	}
}

func (b *builder) BuildImage(ctx context.Context, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string) (*imagetypes.BuildResult, error) {
	if b.settings == nil {
		return nil, errors.New("settings provider not available")
	}

	if strings.TrimSpace(req.ContextDir) == "" {
		return nil, errors.New("contextDir is required")
	}

	settings := b.settings.BuildSettings()
	providerName, provider, err := b.resolveProvider(req.Provider, settings.BuildProvider)
	if err != nil {
		return nil, err
	}

	buildCtx, cancel := timeouts.WithTimeout(ctx, settings.BuildTimeoutSecs, timeouts.DefaultBuildTimeout)
	defer cancel()

	req = normalizeBuildRequest(req, providerName)
	req.Tags = normalizeTags(req.Tags)

	if err := validateBuildRequest(req, providerName); err != nil {
		return nil, err
	}

	if providerName == "local" {
		return b.buildWithDocker(buildCtx, req, progressWriter, serviceName)
	}

	if provider == nil {
		return nil, errors.New("build provider not available")
	}

	session, err := provider.NewSession(buildCtx, req)
	if err != nil {
		return nil, err
	}

	var buildErr error
	defer func() {
		if cerr := session.Close(buildErr); cerr != nil {
			slog.WarnContext(ctx, "build session close error", "provider", providerName, "error", cerr)
		}
	}()

	solveOpt, loadErrCh, err := b.buildSolveOpt(buildCtx, req)
	if err != nil {
		buildErr = err
		return nil, err
	}

	authProvider := authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		ConfigFile: config.LoadDefaultConfigFile(os.Stderr),
	})
	solveOpt.Session = append(solveOpt.Session, authProvider)

	statusCh := make(chan *buildkit.SolveStatus, 16)
	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- streamSolveStatus(buildCtx, statusCh, progressWriter, serviceName)
	}()

	writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
		Type:    "build",
		Phase:   "begin",
		Service: serviceName,
		Status:  "build started",
	})

	resp, err := session.Client.Solve(buildCtx, nil, solveOpt, statusCh)
	buildErr = err

	if err != nil {
		writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
			Type:    "build",
			Service: serviceName,
			Error:   err.Error(),
		})
		return nil, err
	}

	if streamErr := <-streamErrCh; streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		slog.WarnContext(ctx, "build progress stream error", "provider", providerName, "error", streamErr)
	}

	if loadErrCh != nil {
		if loadErr := <-loadErrCh; loadErr != nil {
			buildErr = loadErr
			writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
				Type:    "build",
				Service: serviceName,
				Error:   loadErr.Error(),
			})
			return nil, loadErr
		}
	}

	writeProgressEvent(progressWriter, imagetypes.ProgressEvent{
		Type:    "build",
		Phase:   "complete",
		Service: serviceName,
		Status:  "build complete",
	})

	digest := ""
	if resp != nil {
		if v, ok := resp.ExporterResponse["containerimage.digest"]; ok {
			digest = v
		}
	}

	return &imagetypes.BuildResult{
		Provider: providerName,
		Tags:     req.Tags,
		Digest:   digest,
	}, nil
}

func (b *builder) resolveProvider(override string, defaultProvider string) (string, buildProvider, error) {
	providerName := strings.ToLower(strings.TrimSpace(override))
	if providerName == "" {
		providerName = strings.ToLower(strings.TrimSpace(defaultProvider))
	}
	if providerName == "" {
		providerName = "local"
	}
	if providerName == "local" {
		return providerName, nil, nil
	}
	providerRaw, ok := b.providers[providerName]
	if !ok {
		return "", nil, fmt.Errorf("unknown build provider: %s", providerName)
	}
	provider, ok := providerRaw.(buildProvider)
	if !ok || provider == nil {
		return "", nil, fmt.Errorf("invalid build provider: %s", providerName)
	}
	return providerName, provider, nil
}
