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
	configtypes "github.com/docker/cli/cli/config/types"
	utilsregistry "github.com/getarcaneapp/arcane/backend/internal/utils/registry"
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
	providers            map[string]any
}

func NewBuilder(settings buildtypes.SettingsProvider, dockerClientProvider buildtypes.DockerClientProvider, registryAuthProvider buildtypes.RegistryAuthProvider) buildtypes.Builder {
	providers := map[string]any{
		"depot": newDepotBuildKitProviderInternal(settings),
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
	providerName, provider, err := b.resolveProviderInternal(req.Provider, settings.BuildProvider)
	if err != nil {
		return nil, err
	}

	buildCtx, cancel := timeouts.WithTimeout(ctx, settings.BuildTimeoutSecs, timeouts.DefaultBuildTimeout)
	defer cancel()

	req = normalizeBuildRequestInternal(req, providerName)
	req.Tags = normalizeTagsInternal(req.Tags)

	if err := validateBuildRequestInternal(req, providerName); err != nil {
		return nil, err
	}

	if providerName == "local" {
		return b.buildWithDockerInternal(buildCtx, req, progressWriter, serviceName)
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

	solveOpt, loadErrCh, err := b.buildSolveOptInternal(buildCtx, req)
	if err != nil {
		buildErr = err
		return nil, err
	}

	authProvider := authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		AuthConfigProvider: buildkitAuthConfigProviderInternal(authprovider.LoadAuthConfig(config.LoadDefaultConfigFile(os.Stderr)), b.registryAuthProvider),
	})
	solveOpt.Session = append(solveOpt.Session, authProvider)

	statusCh := make(chan *buildkit.SolveStatus, 16)
	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- streamSolveStatusInternal(buildCtx, statusCh, progressWriter, serviceName)
	}()

	writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
		Type:    "build",
		Phase:   "begin",
		Service: serviceName,
		Status:  "build started",
	})

	resp, err := session.Client.Solve(buildCtx, nil, solveOpt, statusCh)
	buildErr = err

	if err != nil {
		writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
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
			writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
				Type:    "build",
				Service: serviceName,
				Error:   loadErr.Error(),
			})
			return nil, loadErr
		}
	}

	writeProgressEventInternal(progressWriter, imagetypes.ProgressEvent{
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

func buildkitAuthConfigProviderInternal(defaultProvider authprovider.AuthConfigProvider, registryAuthProvider buildtypes.RegistryAuthProvider) authprovider.AuthConfigProvider {
	return func(ctx context.Context, host string, scope []string, cacheCheck authprovider.ExpireCachedAuthCheck) (configtypes.AuthConfig, error) {
		if registryAuthProvider != nil {
			authHeader, err := registryAuthProvider.GetRegistryAuthForHost(ctx, host)
			if err != nil {
				slog.WarnContext(ctx, "failed to resolve build registry auth from database, falling back to docker config", "registry", host, "error", err)
			} else if strings.TrimSpace(authHeader) != "" {
				decodedCfg, decodeErr := utilsregistry.DecodeAuthHeader(authHeader)
				if decodeErr != nil {
					slog.WarnContext(ctx, "failed to decode build registry auth header, falling back to docker config", "registry", host, "error", decodeErr)
				} else {
					authConfig := configtypes.AuthConfig{
						Username:      decodedCfg.Username,
						Password:      decodedCfg.Password,
						Auth:          decodedCfg.Auth,
						ServerAddress: decodedCfg.ServerAddress,
						IdentityToken: decodedCfg.IdentityToken,
						RegistryToken: decodedCfg.RegistryToken,
					}
					if strings.TrimSpace(authConfig.ServerAddress) == "" {
						authConfig.ServerAddress = host
					}
					return authConfig, nil
				}
			}
		}

		if defaultProvider == nil {
			return configtypes.AuthConfig{}, nil
		}

		return defaultProvider(ctx, host, scope, cacheCheck)
	}
}

func (b *builder) resolveProviderInternal(override string, defaultProvider string) (string, buildProvider, error) {
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
