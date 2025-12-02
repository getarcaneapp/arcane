package container

import (
	"context"
	"fmt"
	"os"
	gosignal "os/signal"
	"runtime"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/moby/sys/signal"
	"github.com/sirupsen/logrus"
)

// resizeTtyTo resizes tty to specific height and width
func resizeTtyTo(ctx context.Context, apiClient client.ContainerAPIClient, id string, height, width uint, isExec bool) error {
	if height == 0 && width == 0 {
		return nil
	}

	options := container.ResizeOptions{
		Height: height,
		Width:  width,
	}

	var err error
	if isExec {
		err = apiClient.ContainerExecResize(ctx, id, options)
	} else {
		err = apiClient.ContainerResize(ctx, id, options)
	}

	if err != nil {
		logrus.Debugf("Error resize: %s\r", err)
	}
	return err
}

// resizeTty is to resize the tty with cli out's tty size
func resizeTty(ctx context.Context, cli command.Cli, id string, isExec bool) error {
	height, width := cli.Out().GetTtySize()
	return resizeTtyTo(ctx, cli.Client(), id, height, width, isExec)
}

// initTtySize is to init the tty's size to the same as the window, if there is an error, it will retry 10 times.
func initTtySize(ctx context.Context, cli command.Cli, id string, isExec bool, resizeTtyFunc func(ctx context.Context, cli command.Cli, id string, isExec bool) error) {
	rttyFunc := resizeTtyFunc
	if rttyFunc == nil {
		rttyFunc = resizeTty
	}
	if err := rttyFunc(ctx, cli, id, isExec); err != nil {
		go func() {
			var err error
			for retry := 0; retry < 10; retry++ {
				time.Sleep(time.Duration(retry+1) * 10 * time.Millisecond)
				if err = rttyFunc(ctx, cli, id, isExec); err == nil {
					break
				}
			}
			if err != nil {
				fmt.Fprintln(cli.Err(), "failed to resize tty, using default size")
			}
		}()
	}
}

// MonitorTtySize updates the container tty size when the terminal tty changes size
func MonitorTtySize(ctx context.Context, cli command.Cli, id string, isExec bool) error {
	initTtySize(ctx, cli, id, isExec, resizeTty)
	if runtime.GOOS == "windows" {
		go func() {
			prevH, prevW := cli.Out().GetTtySize()
			for {
				time.Sleep(time.Millisecond * 250)
				h, w := cli.Out().GetTtySize()

				if prevW != w || prevH != h {
					resizeTty(ctx, cli, id, isExec)
				}
				prevH = h
				prevW = w
			}
		}()
	} else {
		sigchan := make(chan os.Signal, 1)
		gosignal.Notify(sigchan, signal.SIGWINCH)
		go func() {
			for range sigchan {
				resizeTty(ctx, cli, id, isExec)
			}
		}()
	}
	return nil
}
