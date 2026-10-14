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

	"github.com/getarcaneapp/arcane/types/v2/base"
	kit "go.getarcane.app/kit/pkg"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	environmentsPathPrefix    = "/api/environments/"
	environmentRequestTimeout = 20 * time.Second
	environmentResponseLimit  = 64 << 20
	environmentConcurrency    = 8
)

var errEnvironmentResponseTooLarge = errors.New("response too large")

type EnvironmentResolver func(ctx context.Context, id string) (base.EnvironmentRef, error)

type EnvironmentScope struct {
	IDs     []string
	Request *http.Request // clone of the outer request captured before auth; carries no scope
	Handler http.Handler  // the Echo instance, for in-process re-dispatch
	Resolve EnvironmentResolver
}

type EnvironmentResult[R any] struct {
	Environment base.EnvironmentRef
	Value       R
}

type EnvironmentCollection[R any] struct {
	Results  []EnvironmentResult[R]
	Failures []base.EnvironmentFailure
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

func CollectEnvironments[R any](ctx context.Context, overrides url.Values) (EnvironmentCollection[R], error) {
	scope, ok := EnvironmentScopeFromContext(ctx)
	if !ok {
		return EnvironmentCollection[R]{}, errors.New("request has no environment scope")
	}

	collectCtx, cancel := context.WithTimeout(ctx, environmentRequestTimeout)
	defer cancel()
	outcomes := make([]environmentOutcome[R], len(scope.IDs))
	var group errgroup.Group
	group.SetLimit(environmentConcurrency)
	for i, id := range scope.IDs {
		group.Go(func() (workerErr error) {
			defer func() { outcomes[i].err = workerErr }()
			defer utils.RecoverToError(&workerErr, "environment aggregate worker", "environmentId", id)
			outcomes[i].value, workerErr = dispatchEnvironment[R](collectCtx, scope, id, overrides)
			return workerErr
		})
	}
	_ = group.Wait()

	collection := EnvironmentCollection[R]{Results: make([]EnvironmentResult[R], 0, len(scope.IDs))}
	for i, id := range scope.IDs {
		outcome := outcomes[i]
		if outcome.err != nil {
			collection.Failures = append(collection.Failures, base.EnvironmentFailure{Environment: base.EnvironmentRef{ID: id, Name: id}, Message: outcome.err.Error()})
			continue
		}
		ref := base.EnvironmentRef{ID: id, Name: id}
		if scope.Resolve != nil {
			if resolved, err := scope.Resolve(ctx, id); err == nil {
				ref = resolved
			}
		}
		collection.Results = append(collection.Results, EnvironmentResult[R]{Environment: ref, Value: outcome.value})
	}
	return collection, nil
}

func dispatchEnvironment[R any](ctx context.Context, scope *EnvironmentScope, id string, overrides url.Values) (R, error) {
	var value R
	rest, ok := strings.CutPrefix(scope.Request.URL.EscapedPath(), environmentsPathPrefix)
	if !ok {
		return value, errors.New("request is not environment-scoped")
	}
	_, suffix, hasSuffix := strings.Cut(rest, "/")
	target, err := url.Parse(environmentsPathPrefix + url.PathEscape(id) + kit.Ternary(hasSuffix, "/"+suffix, ""))
	if err != nil {
		return value, fmt.Errorf("invalid environment path: %w", err)
	}

	limit := time.Now().Add(environmentRequestTimeout)
	deadline, hasDeadline := ctx.Deadline()
	requestCtx, cancel := context.WithDeadline(scope.Request.Context(), kit.Ternary(hasDeadline && deadline.Before(limit), deadline, limit))
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	request := scope.Request.Clone(WithEnvironmentScope(requestCtx, nil))
	request.Body, request.GetBody, request.ContentLength = http.NoBody, nil, 0
	request.URL.Path, request.URL.RawPath = target.Path, target.RawPath
	query := request.URL.Query()
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
	request.URL.RawQuery = query.Encode()
	request.RequestURI = request.URL.RequestURI()
	request.Header.Set("Accept", "application/json")
	request.Header.Del("Accept-Encoding")
	request.Header.Del("If-None-Match")
	request.Header.Del("If-Modified-Since")

	writer := &boundedResponseWriter{header: http.Header{}}
	scope.Handler.ServeHTTP(writer, request)
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
	status   int
	body     bytes.Buffer
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
