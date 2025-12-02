package dockerutil

import (
	"context"
	"io"
	"sync"

	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	dockerclient "github.com/docker/docker/client"
)

// Client represents an active docker object.
type Client struct {
	cli command.Cli

	featuresOnce  sync.Once
	featuresCache map[Feature]bool
}

// NewClient initializes a new docker client.
func NewClient(cli command.Cli) *Client {
	return &Client{cli: cli}
}

// API returns a new docker API client.
func (c *Client) API(name string) (dockerclient.APIClient, error) {
	if name == "" {
		name = c.cli.CurrentContext()
	}
	return NewClientAPI(c.cli, name)
}

// LoadImage imports an image to docker.
func (c *Client) LoadImage(ctx context.Context, name string, status progress.Writer) (io.WriteCloser, func(), error) {
	dapi, err := c.API(name)
	if err != nil {
		return nil, nil, err
	}

	pr, pw := io.Pipe()
	done := make(chan struct{})

	var w *waitingWriter
	w = &waitingWriter{
		PipeWriter: pw,
		f: func() {
			handleErr := func(err error) {
				pr.CloseWithError(err)
				w.mu.Lock()
				w.err = err
				w.mu.Unlock()
			}

			resp, err := dapi.ImageLoad(ctx, pr)
			defer close(done)
			if err != nil {
				handleErr(err)
				return
			}

			status = progress.ResetTime(status)
			if err := progress.Wrap("importing to docker", status.Write, func(l progress.SubLogger) error {
				return fromReader(l, resp.Body)
			}); err != nil {
				handleErr(err)
			}
		},
		done: done,
	}
	return w, func() {
		pr.Close()
	}, nil
}

func (c *Client) Features(ctx context.Context, name string) map[Feature]bool {
	c.featuresOnce.Do(func() {
		c.featuresCache = c.features(ctx, name)
	})
	return c.featuresCache
}

func (c *Client) features(ctx context.Context, name string) map[Feature]bool {
	features := make(map[Feature]bool)
	if dapi, err := c.API(name); err == nil {
		if info, err := dapi.Info(ctx); err == nil {
			for _, v := range info.DriverStatus {
				switch v[0] {
				case "driver-type":
					if v[1] == "io.containerd.snapshotter.v1" {
						features[OCIImporter] = true
					}
				}
			}
		}
	}
	return features
}

type waitingWriter struct {
	*io.PipeWriter
	f    func()
	once sync.Once
	mu   sync.Mutex
	err  error
	done chan struct{}
}

func (w *waitingWriter) Write(dt []byte) (int, error) {
	w.once.Do(func() {
		go w.f()
	})
	return w.PipeWriter.Write(dt)
}

func (w *waitingWriter) Close() error {
	err := w.PipeWriter.Close()
	<-w.done
	if err == nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.err
	}
	return err
}
