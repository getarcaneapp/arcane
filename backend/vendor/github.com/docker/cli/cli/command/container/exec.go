package container

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// ExecOptions group options for `exec` command
type ExecOptions struct {
	DetachKeys  string
	Interactive bool
	TTY         bool
	Detach      bool
	User        string
	Privileged  bool
	Env         opts.ListOpts
	Workdir     string
	Command     []string
	EnvFile     opts.ListOpts
}

// NewExecOptions creates a new ExecOptions
func NewExecOptions() ExecOptions {
	return ExecOptions{
		Env:     opts.NewListOpts(opts.ValidateEnv),
		EnvFile: opts.NewListOpts(nil),
	}
}

// NewExecCommand creates a new cobra.Command for `docker exec`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewExecCommand(dockerCLI command.Cli) *cobra.Command {
	return newExecCommand(dockerCLI)
}

func newExecCommand(dockerCLI command.Cli) *cobra.Command {
	options := NewExecOptions()

	cmd := &cobra.Command{
		Use:   "exec [OPTIONS] CONTAINER COMMAND [ARG...]",
		Short: "Execute a command in a running container",
		Args:  cli.RequiresMinArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			containerIDorName := args[0]
			options.Command = args[1:]
			return RunExec(cmd.Context(), dockerCLI, containerIDorName, options)
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false, func(ctr container.Summary) bool {
			return ctr.State != container.StatePaused
		}),
		Annotations: map[string]string{
			"category-top": "2",
			"aliases":      "docker container exec, docker exec",
		},
	}

	flags := cmd.Flags()
	flags.SetInterspersed(false)

	flags.StringVar(&options.DetachKeys, "detach-keys", "", "Override the key sequence for detaching a container")
	flags.BoolVarP(&options.Interactive, "interactive", "i", false, "Keep STDIN open even if not attached")
	flags.BoolVarP(&options.TTY, "tty", "t", false, "Allocate a pseudo-TTY")
	flags.BoolVarP(&options.Detach, "detach", "d", false, "Detached mode: run command in the background")
	flags.StringVarP(&options.User, "user", "u", "", `Username or UID (format: "<name|uid>[:<group|gid>]")`)
	flags.BoolVar(&options.Privileged, "privileged", false, "Give extended privileges to the command")
	flags.VarP(&options.Env, "env", "e", "Set environment variables")
	flags.SetAnnotation("env", "version", []string{"1.25"})
	flags.Var(&options.EnvFile, "env-file", "Read in a file of environment variables")
	flags.SetAnnotation("env-file", "version", []string{"1.25"})
	flags.StringVarP(&options.Workdir, "workdir", "w", "", "Working directory inside the container")
	flags.SetAnnotation("workdir", "version", []string{"1.35"})

	_ = cmd.RegisterFlagCompletionFunc("env", completion.EnvVarNames)
	_ = cmd.RegisterFlagCompletionFunc("env-file", completion.FileNames)

	return cmd
}

// RunExec executes an `exec` command
func RunExec(ctx context.Context, dockerCLI command.Cli, containerIDorName string, options ExecOptions) error {
	execOptions, err := parseExec(options, dockerCLI.ConfigFile())
	if err != nil {
		return err
	}

	apiClient := dockerCLI.Client()

	// We need to check the tty _before_ we do the ContainerExecCreate, because
	// otherwise if we error out we will leak execIDs on the server (and
	// there's no easy way to clean those up). But also in order to make "not
	// exist" errors take precedence we do a dummy inspect first.
	if _, err := apiClient.ContainerInspect(ctx, containerIDorName); err != nil {
		return err
	}
	if !options.Detach {
		if err := dockerCLI.In().CheckTty(execOptions.AttachStdin, execOptions.Tty); err != nil {
			return err
		}
	}

	fillConsoleSize(execOptions, dockerCLI)

	response, err := apiClient.ContainerExecCreate(ctx, containerIDorName, *execOptions)
	if err != nil {
		return err
	}

	execID := response.ID
	if execID == "" {
		return errors.New("exec ID empty")
	}

	if options.Detach {
		return apiClient.ContainerExecStart(ctx, execID, container.ExecStartOptions{
			Detach:      options.Detach,
			Tty:         execOptions.Tty,
			ConsoleSize: execOptions.ConsoleSize,
		})
	}
	return interactiveExec(ctx, dockerCLI, execOptions, execID)
}

func fillConsoleSize(execOptions *container.ExecOptions, dockerCli command.Cli) {
	if execOptions.Tty {
		height, width := dockerCli.Out().GetTtySize()
		execOptions.ConsoleSize = &[2]uint{height, width}
	}
}

func interactiveExec(ctx context.Context, dockerCli command.Cli, execOptions *container.ExecOptions, execID string) error {
	// Interactive exec requested.
	var (
		out, stderr io.Writer
		in          io.ReadCloser
	)

	if execOptions.AttachStdin {
		in = dockerCli.In()
	}
	if execOptions.AttachStdout {
		out = dockerCli.Out()
	}
	if execOptions.AttachStderr {
		if execOptions.Tty {
			stderr = dockerCli.Out()
		} else {
			stderr = dockerCli.Err()
		}
	}
	fillConsoleSize(execOptions, dockerCli)

	apiClient := dockerCli.Client()
	resp, err := apiClient.ContainerExecAttach(ctx, execID, container.ExecAttachOptions{
		Tty:         execOptions.Tty,
		ConsoleSize: execOptions.ConsoleSize,
	})
	if err != nil {
		return err
	}
	defer resp.Close()

	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		errCh <- func() error {
			streamer := hijackedIOStreamer{
				streams:      dockerCli,
				inputStream:  in,
				outputStream: out,
				errorStream:  stderr,
				resp:         resp,
				tty:          execOptions.Tty,
				detachKeys:   execOptions.DetachKeys,
			}

			return streamer.stream(ctx)
		}()
	}()

	if execOptions.Tty && dockerCli.In().IsTerminal() {
		if err := MonitorTtySize(ctx, dockerCli, execID, true); err != nil {
			_, _ = fmt.Fprintln(dockerCli.Err(), "Error monitoring TTY size:", err)
		}
	}

	if err := <-errCh; err != nil {
		logrus.Debugf("Error hijack: %s", err)
		return err
	}

	return getExecExitStatus(ctx, apiClient, execID)
}

func getExecExitStatus(ctx context.Context, apiClient client.ContainerAPIClient, execID string) error {
	resp, err := apiClient.ContainerExecInspect(ctx, execID)
	if err != nil {
		// If we can't connect, then the daemon probably died.
		if !client.IsErrConnectionFailed(err) {
			return err
		}
		return cli.StatusError{StatusCode: -1}
	}
	status := resp.ExitCode
	if status != 0 {
		return cli.StatusError{StatusCode: status}
	}
	return nil
}

// parseExec parses the specified args for the specified command and generates
// an ExecConfig from it.
func parseExec(execOpts ExecOptions, configFile *configfile.ConfigFile) (*container.ExecOptions, error) {
	execOptions := &container.ExecOptions{
		User:       execOpts.User,
		Privileged: execOpts.Privileged,
		Tty:        execOpts.TTY,
		Cmd:        execOpts.Command,
		WorkingDir: execOpts.Workdir,
	}

	// collect all the environment variables for the container
	var err error
	if execOptions.Env, err = opts.ReadKVEnvStrings(execOpts.EnvFile.GetSlice(), execOpts.Env.GetSlice()); err != nil {
		return nil, err
	}

	// If -d is not set, attach to everything by default
	if !execOpts.Detach {
		execOptions.AttachStdout = true
		execOptions.AttachStderr = true
		if execOpts.Interactive {
			execOptions.AttachStdin = true
		}
	}

	if execOpts.DetachKeys != "" {
		execOptions.DetachKeys = execOpts.DetachKeys
	} else {
		execOptions.DetachKeys = configFile.DetachKeys
	}
	return execOptions, nil
}
