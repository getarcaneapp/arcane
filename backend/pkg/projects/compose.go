package projects

import (
	"io"
	"strings"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/client"
)

type Client struct {
	svc       api.Compose
	dockerCli command.Cli
}

func NewClient() (*Client, error) {
	cli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	opts := flags.NewClientOptions()
	if err := cli.Initialize(opts); err != nil {
		return nil, err
	}

	composeCLI := wrapDockerCLIWithInspectCompatibilityInternal(cli)
	svc, err := compose.NewComposeService(composeCLI,
		compose.WithPrompt(compose.AlwaysOkPrompt()),
	)
	if err != nil {
		return nil, err
	}

	return &Client{svc: svc, dockerCli: composeCLI}, nil
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
	if c == nil || c.dockerCli == nil {
		return nil
	}
	if apiClient := c.dockerCli.Client(); apiClient != nil {
		_ = apiClient.Close()
	}
	return nil
}

type writerConsumer struct{ out io.Writer }

func (w writerConsumer) Register(_ string)  {}
func (w writerConsumer) Start(_ string)     {}
func (w writerConsumer) Stop(_ string)      {}
func (w writerConsumer) Status(_, _ string) {}
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
