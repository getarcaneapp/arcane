package httpx

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"golang.org/x/sync/errgroup"
	kit "kit.getarcane.app"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

const (
	environmentsPathPrefix    = "/api/environments/"
	environmentRequestTimeout = 20 * time.Second
	environmentResponseLimit  = 64 << 20
	EnvironmentConcurrency    = 8
)

var errEnvironmentResponseTooLarge = errors.New("response too large")

type EnvironmentResolver func(ctx context.Context, id string) (types.EnvironmentRef, error)

type EnvironmentScope struct {
	Handler http.Handler  // the Echo instance, for in-process re-dispatch
	Request *http.Request // clone of the outer request captured before auth; carries no scope
	Resolve EnvironmentResolver
	IDs     []string
}

type EnvironmentResult[R any] struct {
	Value       R
	Environment types.EnvironmentRef
}

type EnvironmentCollection[R any] struct {
	Results  []EnvironmentResult[R]
	Failures []types.EnvironmentFailure
}

type environmentScopeKey struct{}

type environmentOutcome[R any] struct {
	value R
	err   error
}

func WithEnvironmentScope(ctx context.Context, scope *EnvironmentScope) context.Context {
	return context.WithValue(ctx, environmentScopeKey{}, scope)
}

func EnvironmentScopeFromContext(ctx context.Context) (*EnvironmentScope, bool) {
	scope, _ := ctx.Value(environmentScopeKey{}).(*EnvironmentScope)
	return scope, scope != nil
}

func IsEnvironmentFanOutRequest(ctx context.Context) bool {
	_, ok := ctx.Value(environmentScopeKey{}).(*EnvironmentScope)
	return ok
}

func CollectEnvironments[R any](ctx context.Context, overrides url.Values) (EnvironmentCollection[R], error) {
	scope, ok := EnvironmentScopeFromContext(ctx)
	if !ok {
		return EnvironmentCollection[R]{}, errors.New("request has no environment scope")
	}

	collectCtx, cancel := context.WithTimeout(ctx, environmentRequestTimeout)
	defer cancel()
	outcomes := make([]environmentOutcome[R], len(scope.IDs))
	var group errgroup.Group
	group.SetLimit(EnvironmentConcurrency)
	for i, id := range scope.IDs {
		group.Go(func() error {
			value, err := dispatchEnvironment[R](collectCtx, scope, id, overrides)
			outcomes[i] = environmentOutcome[R]{value: value, err: err}
			return nil
		})
	}
	_ = group.Wait()

	results := make([]EnvironmentResult[R], 0, len(scope.IDs))
	var failures []types.EnvironmentFailure
	for i, id := range scope.IDs {
		if outcomes[i].err != nil {
			failures = append(failures, types.EnvironmentFailure{
				Environment: types.EnvironmentRef{ID: id, Name: id},
				Message:     outcomes[i].err.Error(),
			})
			continue
		}
		resolved, err := scope.Resolve(ctx, id)
		results = append(results, EnvironmentResult[R]{
			Environment: kit.Ternary(err == nil, resolved, types.EnvironmentRef{ID: id, Name: id}),
			Value:       outcomes[i].value,
		})
	}
	return EnvironmentCollection[R]{Results: results, Failures: failures}, nil
}

func CollectEnvironmentData[T any](ctx context.Context, overrides url.Values, merge func([]types.EnvironmentData[T]) T) (types.ApiResponse[T], error) {
	collection, err := CollectEnvironments[types.ApiResponse[T]](ctx, overrides)
	if err != nil {
		return types.ApiResponse[T]{}, err
	}
	environments := make([]types.EnvironmentData[T], 0, len(collection.Results))
	for _, result := range collection.Results {
		environments = append(environments, types.EnvironmentData[T]{
			Environment: result.Environment,
			Data:        result.Value.Data,
		})
	}
	return types.ApiResponse[T]{
		Success:            true,
		Data:               merge(environments),
		Environments:       environments,
		FailedEnvironments: collection.Failures,
	}, nil
}

func dispatchEnvironment[R any](ctx context.Context, scope *EnvironmentScope, id string, overrides url.Values) (value R, err error) {
	defer utils.RecoverToError(&err, "environment aggregate worker", "environmentId", id)
	rest, ok := strings.CutPrefix(scope.Request.URL.EscapedPath(), environmentsPathPrefix)
	if !ok {
		return value, errors.New("request is not environment-scoped")
	}
	_, suffix, hasSuffix := strings.Cut(rest, "/")
	query := scope.Request.URL.Query()
	for key, values := range overrides {
		if len(values) == 0 {
			query.Del(key)
			continue
		}
		query[key] = slices.Clone(values)
	}
	if query.Get("sort") == "environment" {
		query.Del("sort")
		query.Del("order")
	}
	target, err := url.Parse(environmentsPathPrefix + url.PathEscape(id) + kit.Ternary(hasSuffix, "/"+suffix, "") +
		kit.Ternary(len(query) > 0, "?", "") + query.Encode())
	if err != nil {
		return value, fmt.Errorf("invalid environment path: %w", err)
	}

	deadline, _ := ctx.Deadline()
	requestCtx, cancel := context.WithDeadline(scope.Request.Context(), deadline)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	header := scope.Request.Header.Clone()
	header.Set("Accept", "application/json")
	header.Del("Accept-Encoding")
	header.Del("If-None-Match")
	header.Del("If-Modified-Since")

	writer := &boundedResponseWriter{header: http.Header{}}
	scope.Handler.ServeHTTP(writer, (&http.Request{
		Method:     scope.Request.Method,
		URL:        target,
		Proto:      scope.Request.Proto,
		ProtoMajor: scope.Request.ProtoMajor,
		ProtoMinor: scope.Request.ProtoMinor,
		Header:     header,
		Body:       http.NoBody,
		Host:       scope.Request.Host,
		RemoteAddr: scope.Request.RemoteAddr,
		RequestURI: target.RequestURI(),
		TLS:        scope.Request.TLS,
	}).WithContext(WithEnvironmentScope(requestCtx, nil)))
	status := cmp.Or(writer.status, http.StatusOK)
	switch {
	case writer.overflow:
		return value, errEnvironmentResponseTooLarge
	case requestCtx.Err() != nil && (writer.status == 0 || status >= http.StatusMultipleChoices):
		return value, errors.New(kit.Ternary(errors.Is(requestCtx.Err(), context.DeadlineExceeded), "request timed out", "request cancelled"))
	case writer.header.Get("Content-Encoding") != "":
		return value, errors.New("unexpected encoded response")
	case status < http.StatusOK || status >= http.StatusMultipleChoices:
		var body struct {
			Detail  string `json:"detail"`
			Title   string `json:"title"`
			Message string `json:"message"`
			Error   string `json:"error"`
			Data    struct {
				Error string `json:"error"`
			} `json:"data"`
		}
		_ = json.Unmarshal(writer.body.Bytes(), &body)
		return value, errors.New(cmp.Or(body.Detail, body.Title, body.Message, body.Data.Error, body.Error, http.StatusText(status), fmt.Sprintf("status %d", status)))
	}
	if decodeErr := json.Unmarshal(writer.body.Bytes(), &value); decodeErr != nil {
		return value, fmt.Errorf("invalid response: %w", decodeErr)
	}
	return value, nil
}

type boundedResponseWriter struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	overflow bool
}

func (w *boundedResponseWriter) Header() http.Header {
	return w.header
}

func (w *boundedResponseWriter) WriteHeader(status int) {
	w.status = cmp.Or(w.status, status)
}

func (w *boundedResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if w.overflow || w.body.Len()+len(p) > environmentResponseLimit {
		w.overflow = true
		return 0, errEnvironmentResponseTooLarge
	}
	return w.body.Write(p)
}

func (w *boundedResponseWriter) Flush() {}
