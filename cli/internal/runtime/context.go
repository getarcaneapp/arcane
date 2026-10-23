package runtime

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

type contextKey string

const (
	// AppContextKey is the context key under which the CLI's AppContext is stored;
	// set it with context.WithValue and read it back with From.
	AppContextKey contextKey = "arcane_app_context"

	OutputModeText OutputMode = "text"
	OutputModeJSON OutputMode = "json"
)

type OutputMode string

type Options struct {
	EnvOverride    string
	OutputMode     OutputMode
	AssumeYes      bool
	NoColor        bool
	RequestTimeout time.Duration
}

type AppContext struct {
	Options
	AllConfirmed     bool
	cfg              *clitypes.Config
	selectedEnv      string
	environmentsOnce sync.Once
	environments     []types.Environment
	environmentsErr  error

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

	mode := cmp.Or(opts.OutputMode, OutputModeText)
	switch mode {
	case OutputModeText, OutputModeJSON:
	default:
		return nil, fmt.Errorf("invalid output mode %q (expected text or json)", mode)
	}

	opts.OutputMode = mode
	opts.EnvOverride = strings.TrimSpace(opts.EnvOverride)
	return &AppContext{Options: opts, cfg: cfg}, nil
}

func From(ctx context.Context) (*AppContext, bool) {
	if ctx == nil {
		return nil, false
	}
	app, ok := ctx.Value(AppContextKey).(*AppContext)
	return app, ok && app != nil
}

func (a *AppContext) Config() *clitypes.Config {
	if a == nil || a.cfg == nil {
		return nil
	}
	return a.cfg.Clone()
}

func (a *AppContext) EnvID() string {
	if a == nil {
		return ""
	}
	return cmp.Or(a.selectedEnv, a.EnvOverride, "0")
}

func (a *AppContext) IsJSON() bool {
	return a != nil && a.OutputMode == OutputModeJSON
}

func (a *AppContext) Client() (*client.Client, error) {
	if a == nil {
		return client.NewFromConfig()
	}
	a.clientOnce.Do(func() {
		a.client, a.clientErr = client.New(a.Config())
		if a.clientErr != nil {
			return
		}
		a.client.SetEnvironment(a.EnvID())
		if a.RequestTimeout > 0 {
			a.client.SetTimeout(a.RequestTimeout)
		}
	})
	return a.client, a.clientErr
}

func (a *AppContext) UnauthClient() (*client.Client, error) {
	if a == nil {
		return client.NewFromConfigUnauthenticated()
	}
	a.unauthClientOnce.Do(func() {
		a.unauthClient, a.unauthClientErr = client.NewUnauthenticated(a.Config())
		if a.unauthClientErr != nil {
			return
		}
		a.unauthClient.SetEnvironment(a.EnvID())
		if a.RequestTimeout > 0 {
			a.unauthClient.SetTimeout(a.RequestTimeout)
		}
	})
	return a.unauthClient, a.unauthClientErr
}

func (a *AppContext) AllEnvironments() bool {
	return a != nil && a.EnvOverride == "" && a.selectedEnv == ""
}

// SelectEnvironment pins subsequent requests without changing persisted configuration.
func (a *AppContext) SelectEnvironment(id string) {
	a.selectedEnv = id
	if a.client != nil {
		a.client.SetEnvironment(a.EnvID())
	}
}

// Environments loads the complete visible, enabled environment list once per invocation.
func (a *AppContext) Environments(ctx context.Context) ([]types.Environment, error) {
	a.environmentsOnce.Do(func() {
		c, err := a.Client()
		if err != nil {
			a.environmentsErr = err
			return
		}
		response, err := c.GetJSON[[]types.Environment](ctx, clitypes.Environments()+"?limit=-1")
		if statusErr, ok := errors.AsType[*client.StatusError](err); ok && statusErr.StatusCode == http.StatusForbidden {
			a.environmentsErr = errors.New("these credentials cannot list environments; pass --env <id> to choose one")
			return
		}
		if err != nil {
			a.environmentsErr = fmt.Errorf("failed to discover environments: %w", err)
			return
		}
		a.environments = slices.DeleteFunc(response.Data, func(env types.Environment) bool { return !env.Enabled })
		slices.SortFunc(a.environments, func(left, right types.Environment) int {
			return cmp.Or(cmp.Compare(strings.ToLower(cmp.Or(left.Name, left.ID)), strings.ToLower(cmp.Or(right.Name, right.ID))), cmp.Compare(left.ID, right.ID))
		})
	})
	return a.environments, a.environmentsErr
}
