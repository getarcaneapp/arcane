package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	clitypes "github.com/getarcaneapp/arcane/cli/internal/types"
)

type contextKey string

const appContextKey contextKey = "arcane_app_context"

type OutputMode string

const (
	OutputModeText OutputMode = "text"
	OutputModeJSON OutputMode = "json"
)

type Options struct {
	EnvOverride    string
	OutputMode     OutputMode
	AssumeYes      bool
	NoColor        bool
	RequestTimeout time.Duration
}

type AppContext struct {
	cfg            *clitypes.Config
	envOverride    string
	outputMode     OutputMode
	assumeYes      bool
	noColor        bool
	requestTimeout time.Duration

	clientOnce sync.Once
	clientErr  error
	client     *client.Client

	unauthClientOnce sync.Once
	unauthClientErr  error
	unauthClient     *client.Client
}

func New(opts Options) (*AppContext, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	mode := opts.OutputMode
	if mode == "" {
		mode = OutputModeText
	}
	switch mode {
	case OutputModeText, OutputModeJSON:
	default:
		return nil, fmt.Errorf("invalid output mode %q (expected text or json)", mode)
	}

	return &AppContext{
		cfg:            cfg,
		envOverride:    strings.TrimSpace(opts.EnvOverride),
		outputMode:     mode,
		assumeYes:      opts.AssumeYes,
		noColor:        opts.NoColor,
		requestTimeout: opts.RequestTimeout,
	}, nil
}

func WithAppContext(ctx context.Context, app *AppContext) context.Context {
	return context.WithValue(ctx, appContextKey, app)
}

func From(ctx context.Context) (*AppContext, bool) {
	if ctx == nil {
		return nil, false
	}
	app, ok := ctx.Value(appContextKey).(*AppContext)
	return app, ok && app != nil
}

func (a *AppContext) Config() *clitypes.Config {
	if a == nil || a.cfg == nil {
		return nil
	}
	copyCfg := a.cfg.Clone()
	if copyCfg == nil {
		return nil
	}
	if a.envOverride != "" {
		copyCfg.DefaultEnvironment = a.envOverride
	}
	return copyCfg
}

func (a *AppContext) EnvID() string {
	if a == nil {
		return ""
	}
	if a.envOverride != "" {
		return a.envOverride
	}
	if a.cfg != nil {
		return a.cfg.DefaultEnvironment
	}
	return ""
}

func (a *AppContext) OutputMode() OutputMode {
	if a == nil {
		return OutputModeText
	}
	return a.outputMode
}

func (a *AppContext) IsJSON() bool {
	return a.OutputMode() == OutputModeJSON
}

func (a *AppContext) AssumeYes() bool {
	return a != nil && a.assumeYes
}

func (a *AppContext) NoColor() bool {
	return a != nil && a.noColor
}

func (a *AppContext) RequestTimeout() time.Duration {
	if a == nil {
		return 0
	}
	return a.requestTimeout
}

func (a *AppContext) Client() (*client.Client, error) {
	if a == nil {
		return client.NewFromConfig()
	}
	a.clientOnce.Do(func() {
		cfg := a.Config()
		a.client, a.clientErr = client.New(cfg)
		if a.clientErr != nil {
			return
		}
		if a.requestTimeout > 0 {
			a.client.SetTimeout(a.requestTimeout)
		}
	})
	return a.client, a.clientErr
}

func (a *AppContext) UnauthClient() (*client.Client, error) {
	if a == nil {
		return client.NewFromConfigUnauthenticated()
	}
	a.unauthClientOnce.Do(func() {
		cfg := a.Config()
		a.unauthClient, a.unauthClientErr = client.NewUnauthenticated(cfg)
		if a.unauthClientErr != nil {
			return
		}
		if a.requestTimeout > 0 {
			a.unauthClient.SetTimeout(a.requestTimeout)
		}
	})
	return a.unauthClient, a.unauthClientErr
}
