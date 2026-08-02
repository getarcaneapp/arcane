package projects

import (
	"context"
	"io"
	"maps"
	"strings"

	"github.com/docker/cli/cli/command"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"

	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
)

type Client struct {
	svc       api.Compose
	dockerCli command.Cli
	logWriter io.WriteCloser
}

func NewClient(ctx context.Context, authConfigs map[string]registry.AuthConfig) (*Client, error) {
	cli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	opts := flags.NewClientOptions()
	if err := cli.Initialize(opts); err != nil {
		return nil, err
	}
	if composeAuthConfigs := buildComposeAuthConfigsInternal(authConfigs); len(composeAuthConfigs) > 0 {
		configFile := cli.ConfigFile()
		if configFile.AuthConfigs == nil {
			configFile.AuthConfigs = map[string]clitypes.AuthConfig{}
		}
		maps.Copy(configFile.AuthConfigs, composeAuthConfigs)
	}

	composeCLI := wrapDockerCLIWithInspectCompatibilityInternal(cli)

	serviceOptions := []compose.Option{
		compose.WithPrompt(compose.AlwaysOkPrompt()),
	}

	// When the caller streams operation output, render compose's own progress
	// events exactly as `docker compose --progress=plain` prints them.
	var logWriter io.WriteCloser
	if progressWriter, ok := ctx.Value(dockerutils.ProgressWriterKey{}).(io.Writer); ok && progressWriter != nil {
		logWriter = dockerutils.NewLogLineWriter(progressWriter)
		serviceOptions = append(serviceOptions,
			compose.WithEventProcessor(display.Plain(logWriter)),
			compose.WithOutputStream(logWriter),
			compose.WithErrorStream(logWriter),
		)
	}

	svc, err := compose.NewComposeService(composeCLI, serviceOptions...)
	if err != nil {
		if logWriter != nil {
			_ = logWriter.Close()
		}
		return nil, err
	}

	return &Client{svc: svc, dockerCli: composeCLI, logWriter: logWriter}, nil
}

func buildComposeAuthConfigsInternal(authConfigs map[string]registry.AuthConfig) map[string]clitypes.AuthConfig {
	if len(authConfigs) == 0 {
		return nil
	}

	composeAuthConfigs := make(map[string]clitypes.AuthConfig, len(authConfigs))
	for host, authConfig := range authConfigs {
		key := strings.TrimSpace(host)
		if key == "" {
			continue
		}
		// Docker CLI auth lookup still uses the legacy index URL for Docker Hub.
		if key == "docker.io" {
			key = "https://index.docker.io/v1/"
		}
		composeAuthConfigs[key] = clitypes.AuthConfig{
			Username:      authConfig.Username,
			Password:      authConfig.Password,
			Auth:          authConfig.Auth,
			ServerAddress: authConfig.ServerAddress,
			IdentityToken: authConfig.IdentityToken,
			RegistryToken: authConfig.RegistryToken,
		}
	}
	if len(composeAuthConfigs) == 0 {
		return nil
	}

	return composeAuthConfigs
}

type inspectCompatibleDockerCli struct {
	command.Cli

	apiClient client.APIClient
}

func (c *inspectCompatibleDockerCli) Client() client.APIClient {
	return c.apiClient
}

func wrapDockerCLIWithInspectCompatibilityInternal(cli command.Cli) command.Cli {
	if cli == nil {
		return nil
	}

	return &inspectCompatibleDockerCli{
		Cli:       cli,
		apiClient: libarcane.WrapDockerAPIClientForInspectCompatibility(cli.Client()),
	}
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.logWriter != nil {
		_ = c.logWriter.Close()
	}
	if c.dockerCli == nil {
		return nil
	}
	if apiClient := c.dockerCli.Client(); apiClient != nil {
		_ = apiClient.Close()
	}
	return nil
}

type writerConsumer struct{ out io.Writer }

func (w writerConsumer) Register(container string)    {}
func (w writerConsumer) Start(container string)       {}
func (w writerConsumer) Stop(container string)        {}
func (w writerConsumer) Status(container, msg string) {}
func (w writerConsumer) Log(container, msg string) {
	w.write(container, msg)
}

func (w writerConsumer) Err(container, msg string) {
	w.write(container, msg)
}

func (w writerConsumer) write(container, msg string) {
	if w.out == nil {
		return
	}
	output := msg
	if container != "" {
		output = container + " | " + msg
	}
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	_, _ = io.WriteString(w.out, output)
}
