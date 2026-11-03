package projects

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/cli/cli/command"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/docker/compat"
	"kit.getarcane.app/docker/types"
)

type (
	Client struct {
		svc       api.Compose
		dockerCli command.Cli
		logWriter io.WriteCloser
	}

	ClientConfig struct {
		AuthConfigs    map[string]registry.AuthConfig
		DockerHost     string
		MaxConcurrency int
		Timeout        time.Duration
		AcceptPrompts  bool
		DemuxLogs      bool
	}

	plainComposeClientEntry struct {
		client *Client
		host   string
	}

	inspectCompatibleDockerCli struct {
		command.Cli

		apiClient client.APIClient
	}

	// writerConsumer is locked because compose streams several containers into one consumer.
	writerConsumer struct {
		out            io.Writer
		mu             sync.Mutex
		writeErrLogged bool
	}
)

const (
	// stderrLogLinePrefix lets the pkg/ws log parser classify stderr lines like container logs.
	stderrLogLinePrefix = "[STDERR] "
)

var plainComposeClientCache atomic.Pointer[plainComposeClientEntry]

func NewClient(ctx context.Context, config ClientConfig) (*Client, error) {
	cli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	if initializeErr := cli.Initialize(&flags.ClientOptions{
		Hosts: kit.Ternary(config.DockerHost != "", []string{config.DockerHost}, nil),
	}); initializeErr != nil {
		return nil, initializeErr
	}
	if composeAuthConfigs := buildComposeAuthConfigs(config.AuthConfigs); len(composeAuthConfigs) > 0 {
		configFile := cli.ConfigFile()
		if configFile.AuthConfigs == nil {
			configFile.AuthConfigs = map[string]clitypes.AuthConfig{}
		}
		maps.Copy(configFile.AuthConfigs, composeAuthConfigs)
	}

	composeCLI := wrapDockerCLIWithInspectCompatibility(cli)
	if config.DemuxLogs {
		composeCLI = wrapDockerCLIWithLogsDemux(cli)
	}

	var logWriter io.WriteCloser
	if progressWriter, ok := ctx.Value(types.ProgressWriterKey{}).(io.Writer); ok && progressWriter != nil {
		logWriter = docker.NewLogLineWriter(progressWriter)
	}

	prompt := kit.Ternary(config.AcceptPrompts, compose.AlwaysOkPrompt(), declinePrompt(ctx, logWriter))
	serviceOptions := []compose.Option{compose.WithPrompt(prompt)}
	if logWriter != nil {
		serviceOptions = append(serviceOptions,
			compose.WithEventProcessor(display.Plain(logWriter)),
			compose.WithOutputStream(logWriter),
			compose.WithErrorStream(logWriter),
		)
	}
	if config.MaxConcurrency > 0 {
		serviceOptions = append(serviceOptions, compose.WithMaxConcurrency(config.MaxConcurrency))
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

// declinePrompt declines destructive plans like volume recreation and logs the question so the user can opt in.
func declinePrompt(ctx context.Context, logWriter io.Writer) compose.Prompt {
	return func(message string, defaultValue bool) (bool, error) {
		slog.WarnContext(ctx, "compose prompt declined by default", "message", message)
		if logWriter != nil {
			_, _ = io.WriteString(logWriter, "WARNING: "+message+" Declined; enable volume recreation on deploy to apply this change.\n")
		}
		return defaultValue, nil
	}
}

func plainComposeClient(dockerHost string) (*Client, error) {
	current := plainComposeClientCache.Load()
	if current != nil && current.host == dockerHost {
		return current.client, nil
	}

	// Built on context.Background so request values and deadlines do not leak into the cached client.
	c, err := NewClient(context.Background(), ClientConfig{DockerHost: dockerHost}) //nolint:contextcheck,forbidigo // Cached Compose client must not retain request values or cancellation.
	if err != nil {
		return nil, err
	}
	if !plainComposeClientCache.CompareAndSwap(current, &plainComposeClientEntry{host: dockerHost, client: c}) {
		_ = c.Close()
		return plainComposeClient(dockerHost)
	}
	if current != nil {
		_ = current.client.Close()
	}
	return c, nil
}

func buildComposeAuthConfigs(authConfigs map[string]registry.AuthConfig) map[string]clitypes.AuthConfig {
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
	return kit.Ternary(len(composeAuthConfigs) == 0, nil, composeAuthConfigs)
}

func (c *inspectCompatibleDockerCli) Client() client.APIClient {
	return c.apiClient
}

func wrapDockerCLIWithInspectCompatibility(cli command.Cli) command.Cli {
	if cli == nil {
		return nil
	}

	return &inspectCompatibleDockerCli{
		Cli:       cli,
		apiClient: compat.WrapDockerAPIClientForInspectCompatibility(cli.Client()),
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

func (w *writerConsumer) Register(container string) {}

func (w *writerConsumer) Start(container string) {}

func (w *writerConsumer) Stop(container string) {}

func (w *writerConsumer) Status(container, msg string) {}

func (w *writerConsumer) Log(container, msg string) {
	w.write(container, msg)
}

func (w *writerConsumer) Err(container, msg string) {
	if msg == "" || strings.HasPrefix(msg, stderrLogLinePrefix) {
		w.write(container, msg)
		return
	}
	w.write(container, stderrLogLinePrefix+msg)
}

func (w *writerConsumer) write(container, msg string) {
	if w.out == nil {
		return
	}
	output := kit.Ternary(container != "", container+" | "+msg, msg)
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := io.WriteString(w.out, output); err != nil && !w.writeErrLogged {
		w.writeErrLogged = true
		slog.DebugContext(context.Background(), "project log output write failed; subsequent output may be truncated", "error", err) //nolint:forbidigo // Compose output writer has no request context.
	}
}
