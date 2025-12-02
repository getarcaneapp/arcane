package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/formatter"
	flagsHelper "github.com/docker/cli/cli/flags"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// StatsOptions defines options for [RunStats].
type StatsOptions struct {
	// All allows including both running and stopped containers. The default
	// is to only include running containers.
	All bool

	// NoStream disables streaming stats. If enabled, stats are collected once,
	// and the result is printed.
	NoStream bool

	// NoTrunc disables truncating the output. The default is to truncate
	// output such as container-IDs.
	NoTrunc bool

	// Format is a custom template to use for presenting the stats.
	// Refer to [flagsHelper.FormatHelp] for accepted formats.
	Format string

	// Containers is the list of container names or IDs to include in the stats.
	// If empty, all containers are included. It is mutually exclusive with the
	// Filters option, and an error is produced if both are set.
	Containers []string

	// Filters provides optional filters to filter the list of containers and their
	// associated container-events to include in the stats if no list of containers
	// is set. If no filter is provided, all containers are included. Filters and
	// Containers are currently mutually exclusive, and setting both options
	// produces an error.
	//
	// These filters are used both to collect the initial list of containers and
	// to refresh the list of containers based on container-events, accepted
	// filters are limited to the intersection of filters accepted by "events"
	// and "container list".
	//
	// Currently only "label" / "label=value" filters are accepted. Additional
	// filter options may be added in future (within the constraints described
	// above), but may require daemon-side validation as the list of accepted
	// filters can differ between daemon- and API versions.
	Filters *filters.Args
}

// NewStatsCommand creates a new [cobra.Command] for "docker stats".
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewStatsCommand(dockerCLI command.Cli) *cobra.Command {
	return newStatsCommand(dockerCLI)
}

func newStatsCommand(dockerCLI command.Cli) *cobra.Command {
	options := StatsOptions{}

	cmd := &cobra.Command{
		Use:   "stats [OPTIONS] [CONTAINER...]",
		Short: "Display a live stream of container(s) resource usage statistics",
		Args:  cli.RequiresMinArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Containers = args
			return RunStats(cmd.Context(), dockerCLI, &options)
		},
		Annotations: map[string]string{
			"aliases": "docker container stats, docker stats",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false),
	}

	flags := cmd.Flags()
	flags.BoolVarP(&options.All, "all", "a", false, "Show all containers (default shows just running)")
	flags.BoolVar(&options.NoStream, "no-stream", false, "Disable streaming stats and only pull the first result")
	flags.BoolVar(&options.NoTrunc, "no-trunc", false, "Do not truncate output")
	flags.StringVar(&options.Format, "format", "", flagsHelper.FormatHelp)
	return cmd
}

// acceptedStatsFilters is the list of filters accepted by [RunStats] (through
// the [StatsOptions.Filters] option).
//
// TODO(thaJeztah): don't hard-code the list of accept filters, and expand
// to the intersection of filters accepted by both "container list" and
// "system events". Validating filters may require an initial API call
// to both endpoints ("container list" and "system events").
var acceptedStatsFilters = map[string]bool{
	"label": true,
}

// RunStats displays a live stream of resource usage statistics for one or more containers.
// This shows real-time information on CPU usage, memory usage, and network I/O.
//
//nolint:gocyclo
func RunStats(ctx context.Context, dockerCLI command.Cli, options *StatsOptions) error {
	apiClient := dockerCLI.Client()

	// waitFirst is a WaitGroup to wait first stat data's reach for each container
	waitFirst := &sync.WaitGroup{}
	// closeChan is a non-buffered channel used to collect errors from goroutines.
	closeChan := make(chan error)
	cStats := stats{}

	showAll := len(options.Containers) == 0
	if showAll {
		// If no names were specified, start a long-running goroutine which
		// monitors container events. We make sure we're subscribed before
		// retrieving the list of running containers to avoid a race where we
		// would "miss" a creation.
		started := make(chan struct{})

		if options.Filters == nil {
			f := filters.NewArgs()
			options.Filters = &f
		}

		if err := options.Filters.Validate(acceptedStatsFilters); err != nil {
			return err
		}

		eh := newEventHandler()
		if options.All {
			eh.setHandler(events.ActionCreate, func(e events.Message) {
				s := NewStats(e.Actor.ID)
				if cStats.add(s) {
					waitFirst.Add(1)
					go collect(ctx, s, apiClient, !options.NoStream, waitFirst)
				}
			})
		}

		eh.setHandler(events.ActionStart, func(e events.Message) {
			s := NewStats(e.Actor.ID)
			if cStats.add(s) {
				waitFirst.Add(1)
				go collect(ctx, s, apiClient, !options.NoStream, waitFirst)
			}
		})

		if !options.All {
			eh.setHandler(events.ActionDie, func(e events.Message) {
				cStats.remove(e.Actor.ID)
			})
		}

		// monitorContainerEvents watches for container creation and removal (only
		// used when calling `docker stats` without arguments).
		monitorContainerEvents := func(started chan<- struct{}, c chan events.Message, stopped <-chan struct{}) {
			// Create a copy of the custom filters so that we don't mutate
			// the original set of filters. Custom filters are used both
			// to list containers and to filter events, but the "type" filter
			// is not valid for filtering containers.
			f := options.Filters.Clone()
			f.Add("type", string(events.ContainerEventType))
			eventChan, errChan := apiClient.Events(ctx, events.ListOptions{
				Filters: f,
			})

			// Whether we successfully subscribed to eventChan or not, we can now
			// unblock the main goroutine.
			close(started)
			defer close(c)

			for {
				select {
				case <-stopped:
					return
				case event := <-eventChan:
					c <- event
				case err := <-errChan:
					closeChan <- err
					return
				}
			}
		}

		eventChan := make(chan events.Message)
		go eh.watch(eventChan)
		stopped := make(chan struct{})
		go monitorContainerEvents(started, eventChan, stopped)
		defer close(stopped)
		<-started

		// Fetch the initial list of containers and collect stats for them.
		// After the initial list was collected, we start listening for events
		// to refresh the list of containers.
		cs, err := apiClient.ContainerList(ctx, container.ListOptions{
			All:     options.All,
			Filters: *options.Filters,
		})
		if err != nil {
			return err
		}
		for _, ctr := range cs {
			s := NewStats(ctr.ID)
			if cStats.add(s) {
				waitFirst.Add(1)
				go collect(ctx, s, apiClient, !options.NoStream, waitFirst)
			}
		}

		// make sure each container get at least one valid stat data
		waitFirst.Wait()
	} else {
		// TODO(thaJeztah): re-implement options.Containers as a filter so that
		// only a single code-path is needed, and custom filters can be combined
		// with a list of container names/IDs.

		if options.Filters != nil && options.Filters.Len() > 0 {
			return errors.New("filtering is not supported when specifying a list of containers")
		}

		// Create the list of containers, and start collecting stats for all
		// containers passed.
		for _, ctr := range options.Containers {
			s := NewStats(ctr)
			if cStats.add(s) {
				waitFirst.Add(1)
				go collect(ctx, s, apiClient, !options.NoStream, waitFirst)
			}
		}

		// We don't expect any asynchronous errors: closeChan can be closed.
		close(closeChan)

		// make sure each container get at least one valid stat data
		waitFirst.Wait()

		var errs []error
		cStats.mu.RLock()
		for _, c := range cStats.cs {
			if err := c.GetError(); err != nil {
				errs = append(errs, err)
			}
		}
		cStats.mu.RUnlock()
		if err := errors.Join(errs...); err != nil {
			return err
		}
	}

	format := options.Format
	if len(format) == 0 {
		if len(dockerCLI.ConfigFile().StatsFormat) > 0 {
			format = dockerCLI.ConfigFile().StatsFormat
		} else {
			format = formatter.TableFormatKey
		}
	}
	if daemonOSType == "" {
		// Get the daemonOSType if not set already. The daemonOSType variable
		// should already be set when collecting stats as part of "collect()",
		// so we unlikely hit this code in practice.
		daemonOSType = dockerCLI.ServerInfo().OSType
	}

	// Buffer to store formatted stats text.
	// Once formatted, it will be printed in one write to avoid screen flickering.
	var statsTextBuffer bytes.Buffer

	statsCtx := formatter.Context{
		Output: &statsTextBuffer,
		Format: NewStatsFormat(format, daemonOSType),
	}

	var err error
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		var ccStats []StatsEntry
		cStats.mu.RLock()
		for _, c := range cStats.cs {
			ccStats = append(ccStats, c.GetStatistics())
		}
		cStats.mu.RUnlock()

		if !options.NoStream {
			// Start by moving the cursor to the top-left
			_, _ = fmt.Fprint(&statsTextBuffer, "\033[H")
		}

		if err = statsFormatWrite(statsCtx, ccStats, daemonOSType, !options.NoTrunc); err != nil {
			break
		}

		if !options.NoStream {
			for _, line := range strings.Split(statsTextBuffer.String(), "\n") {
				// In case the new text is shorter than the one we are writing over,
				// we'll append the "erase line" escape sequence to clear the remaining text.
				_, _ = fmt.Fprintln(&statsTextBuffer, line, "\033[K")
			}

			// We might have fewer containers than before, so let's clear the remaining text
			_, _ = fmt.Fprint(&statsTextBuffer, "\033[J")
		}

		_, _ = fmt.Fprint(dockerCLI.Out(), statsTextBuffer.String())
		statsTextBuffer.Reset()

		if len(cStats.cs) == 0 && !showAll {
			break
		}
		if options.NoStream {
			break
		}
		select {
		case err, ok := <-closeChan:
			if ok {
				if err != nil {
					// Suppress "unexpected EOF" errors in the CLI so that
					// it shuts down cleanly when the daemon restarts.
					if errors.Is(err, io.ErrUnexpectedEOF) {
						return nil
					}
					return err
				}
			}
		default:
			// just skip
		}
	}
	return err
}

// newEventHandler initializes and returns an eventHandler
func newEventHandler() *eventHandler {
	return &eventHandler{handlers: make(map[events.Action]func(events.Message))}
}

// eventHandler allows for registering specific events to setHandler.
type eventHandler struct {
	handlers map[events.Action]func(events.Message)
}

func (eh *eventHandler) setHandler(action events.Action, handler func(events.Message)) {
	eh.handlers[action] = handler
}

// watch ranges over the passed in event chan and processes the events based on the
// handlers created for a given action.
// To stop watching, close the event chan.
func (eh *eventHandler) watch(c <-chan events.Message) {
	for e := range c {
		h, exists := eh.handlers[e.Action]
		if !exists {
			continue
		}
		if e.Actor.ID == "" {
			logrus.WithField("event", e).Errorf("event handler: received %s event with empty ID", e.Action)
			continue
		}

		logrus.WithField("event", e).Debugf("event handler: received %s event for: %s", e.Action, e.Actor.ID)
		go h(e)
	}
}
