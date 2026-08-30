package projects

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"

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

type composeClientConfigInternal struct {
	isLogDemuxEnabled bool
	serviceOptions    []compose.Option
}

type composeClientOptionInternal func(*composeClientConfigInternal)

// streamDemuxedLogsInternal wires the log-parity wrapper so non-TTY container
// log responses keep stderr metadata as Arcane's [STDERR] marker.
func streamDemuxedLogsInternal(config *composeClientConfigInternal) {
	config.isLogDemuxEnabled = true
}

func withComposeServiceOptionsInternal(options ...compose.Option) composeClientOptionInternal {
	return func(config *composeClientConfigInternal) {
		config.serviceOptions = append(config.serviceOptions, options...)
	}
}

// NewClient builds a compose client. dockerHost, when non-empty, pins the
// docker CLI to that daemon endpoint instead of letting it resolve one from
// the environment.
func NewClient(ctx context.Context, dockerHost string, authConfigs map[string]registry.AuthConfig, prompt compose.Prompt, options ...composeClientOptionInternal) (*Client, error) {
	config := composeClientConfigInternal{}
	for _, option := range options {
		option(&config)
	}

	cli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	opts := flags.NewClientOptions()
	if dockerHost != "" {
		opts.Hosts = []string{dockerHost}
	}
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
	if config.isLogDemuxEnabled {
		composeCLI = wrapDockerCLIWithLogsDemuxInternal(cli)
	}

	// When the caller streams operation output, render compose's own progress
	// events exactly as `docker compose --progress=plain` prints them.
	var logWriter io.WriteCloser
	if progressWriter, ok := ctx.Value(dockerutils.ProgressWriterKey{}).(io.Writer); ok && progressWriter != nil {
		logWriter = dockerutils.NewLogLineWriter(progressWriter)
	}

	if prompt == nil {
		// Compose consults the prompt before destructive plan decisions (today:
		// recreating a volume whose config diverged, which loses its data).
		// Without an interactive channel, answer with the default (decline) and
		// surface the question in the deploy log so the user can opt in.
		prompt = func(message string, defaultValue bool) (bool, error) {
			slog.WarnContext(ctx, "compose prompt declined by default", "message", message)
			if logWriter != nil {
				_, _ = io.WriteString(logWriter, "WARNING: "+message+" Declined; enable volume recreation on deploy to apply this change.\n")
			}
			return defaultValue, nil
		}
	}

	serviceOptions := []compose.Option{
		compose.WithPrompt(prompt),
	}
	if logWriter != nil {
		serviceOptions = append(serviceOptions,
			compose.WithEventProcessor(display.Plain(logWriter)),
			compose.WithOutputStream(logWriter),
			compose.WithErrorStream(logWriter),
		)
	}
	serviceOptions = append(serviceOptions, config.serviceOptions...)

	svc, err := compose.NewComposeService(composeCLI, serviceOptions...)
	if err != nil {
		if logWriter != nil {
			_ = logWriter.Close()
		}
		return nil, err
	}

	return &Client{svc: svc, dockerCli: composeCLI, logWriter: logWriter}, nil
}

type plainComposeClientEntryInternal struct {
	dockerHost string
	client     *Client
}

var plainComposeClient atomic.Pointer[plainComposeClientEntryInternal]

func plainComposeClientInternal(ctx context.Context, dockerHost string) (*Client, bool, error) {
	if dockerHost == "" {
		c, err := NewClient(ctx, "", nil, nil)
		return c, false, err
	}

	if entry := plainComposeClient.Load(); entry != nil && entry.dockerHost == dockerHost {
		return entry.client, true, nil
	}

	// Built on context.Background so request-scoped values (progress
	// writers, deadlines) do not leak into the long-lived client.
	c, err := NewClient(context.Background(), dockerHost, nil, nil) //nolint:contextcheck // deliberate: see comment above
	if err != nil {
		return nil, false, err
	}
	// Concurrent first calls may race and briefly duplicate a client; the
	// last store wins and later calls converge on it.
	plainComposeClient.Store(&plainComposeClientEntryInternal{dockerHost: dockerHost, client: c})
	return c, true, nil
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

// writerConsumer serializes compose log events: compose may stream several
// containers concurrently into one consumer.
type writerConsumer struct {
	out            io.Writer
	mu             sync.Mutex
	writeErrLogged bool
}

// stderrLogLinePrefixInternal marks stderr content so the project-log parser
// (pkg/libarcane/ws) can classify lines the same way container logs do.
const stderrLogLinePrefixInternal = "[STDERR] "

func (w *writerConsumer) Register(container string)    {}
func (w *writerConsumer) Start(container string)       {}
func (w *writerConsumer) Stop(container string)        {}
func (w *writerConsumer) Status(container, msg string) {}
func (w *writerConsumer) Log(container, msg string) {
	w.write(container, msg)
}

func (w *writerConsumer) Err(container, msg string) {
	if msg == "" || strings.HasPrefix(msg, stderrLogLinePrefixInternal) {
		w.write(container, msg)
		return
	}
	w.write(container, stderrLogLinePrefixInternal+msg)
}

func (w *writerConsumer) write(container, msg string) {
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := io.WriteString(w.out, output); err != nil && !w.writeErrLogged {
		w.writeErrLogged = true
		slog.Debug("project log output write failed; subsequent output may be truncated", "error", err)
	}
}
