package secretsource

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"time"

	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/cache"

	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	DockerClientFunc func(ctx context.Context) (*client.Client, error)

	EngineConfig struct {
		DockerClient DockerClientFunc
	}

	Request struct {
		Inputs      map[string]string            `json:"inputs"`
		Credentials map[string]string            `json:"credentials"`
		SourceID    string                       `json:"sourceId"`
		Path        string                       `json:"path"`
		Definition  types.SecretSourceDefinition `json:"definition"`
	}

	Engine struct {
		httpClient   *http.Client
		dockerClient DockerClientFunc
		tokens       *cache.Cache[string, loginToken]
	}

	loginToken struct {
		values   map[string]string
		sourceID string
	}
)

const (
	requestTimeout     = 30 * time.Second
	fetchTimeout       = 2 * time.Minute
	maxResponseBytes   = 1 << 20
	defaultTokenTTL    = 5 * time.Minute
	expiresInCaptureID = "expiresIn"
)

func NewEngine(cfg EngineConfig) *Engine {
	return &Engine{
		httpClient: httpx.NewPrivateNetworkHTTPClient(httpx.PrivateNetworkClientConfig{
			Timeout:               requestTimeout,
			ResponseHeaderTimeout: requestTimeout,
		}),
		dockerClient: cfg.DockerClient,
		tokens: &cache.Cache[string, loginToken]{
			TTL:             defaultTokenTTL,
			JanitorInterval: defaultTokenTTL,
		},
	}
}

func (e *Engine) Fetch(ctx context.Context, req Request) ([]types.EnvVariable, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	inputs, err := resolveFields("input", req.Definition.Inputs, req.Inputs)
	if err != nil {
		return nil, err
	}
	credentials, err := resolveFields("credential", req.Definition.Credentials, req.Credentials)
	if err != nil {
		return nil, err
	}

	switch req.Definition.Kind {
	case KindHTTP:
		cacheKey, marshalErr := json.Marshal(req, json.Deterministic(true))
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to key login cache: %w", marshalErr)
		}
		document, fetchErr := e.fetchHTTP(ctx, httpFetch{
			definition:  req.Definition,
			inputs:      inputs,
			credentials: credentials,
			path:        req.Path,
			sourceID:    req.SourceID,
			cacheKey:    kit.SHA256Hex(string(cacheKey)),
		})
		if fetchErr != nil {
			return nil, fetchErr
		}
		return extract(document, req.Definition.Extract)
	case KindContainer:
		return e.fetchContainer(ctx, req.Definition, templateData(inputs, credentials, req.Path, map[string]string{}))
	default:
		return nil, fmt.Errorf("unsupported secret source kind %q", req.Definition.Kind)
	}
}

func (e *Engine) ForgetSource(sourceID string) {
	var keys []string
	for key, token := range e.tokens.Range {
		if token.sourceID == sourceID {
			keys = append(keys, key)
		}
	}
	e.tokens.Delete(keys...)
}
