package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"go.getarcane.app/kit/normalization"
)

// Install before routes so Huma validates normalized input.
func registerNormalizationInternal(api huma.API) {
	api.OpenAPI().OnAddOperation = append(api.OpenAPI().OnAddOperation, func(_ *huma.OpenAPI, op *huma.Operation) {
		typ := normalizationBodyTypeInternal(api, op)
		if typ == nil {
			return
		}
		if _, err := normalization.HasRules(typ); err != nil {
			panic(fmt.Errorf("operation %s normalization: %w", op.OperationID, err))
		}
	})
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		typ := normalizationBodyTypeInternal(api, op)
		if typ == nil {
			next(ctx)
			return
		}
		hasRules, err := normalization.HasRules(typ)
		if err != nil {
			writeNormalizationErrorInternal(api, ctx, http.StatusInternalServerError, "invalid request normalization configuration")
			return
		}
		if !hasRules || !isNormalizationJSONInternal(ctx.Header("Content-Type")) {
			next(ctx)
			return
		}

		body, err := readNormalizationBodyInternal(ctx)
		if err != nil {
			status := http.StatusInternalServerError
			if statusErr, ok := errors.AsType[huma.StatusError](err); ok {
				status = statusErr.GetStatus()
			}
			writeNormalizationErrorInternal(api, ctx, status, err.Error())
			return
		}
		if normalized, normalizeErr := normalization.NormalizeJSON(body, typ); normalizeErr == nil {
			body = normalized
		}
		// Let Huma report malformed JSON and incompatible values as usual.
		request := humaecho.Unwrap(ctx).Request()
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		next(ctx)
	})
}

func normalizationBodyTypeInternal(api huma.API, op *huma.Operation) reflect.Type {
	if op.RequestBody == nil {
		return nil
	}
	media := op.RequestBody.Content["application/json"]
	if media == nil || media.Schema == nil {
		return nil
	}
	return normalizationSchemaTypeInternal(api.OpenAPI().Components.Schemas, media.Schema)
}

func normalizationSchemaTypeInternal(registry huma.Registry, schema *huma.Schema) reflect.Type {
	if schema.Ref != "" {
		return registry.TypeFromRef(schema.Ref)
	}
	if schema.Type == "array" && schema.Items != nil {
		if item := normalizationSchemaTypeInternal(registry, schema.Items); item != nil {
			return reflect.SliceOf(item)
		}
	}
	return nil
}

func isNormalizationJSONInternal(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mediaType == "application/json" || strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"))
}

func writeNormalizationErrorInternal(api huma.API, ctx huma.Context, status int, message string) {
	if err := huma.WriteErr(api, ctx, status, message); err != nil {
		slog.WarnContext(ctx.Context(), "failed to write normalization response", "error", err)
	}
}

// Read the original request with the same limits Huma applies.
// It closes the original reader and reports errors with Huma HTTP status codes.
func readNormalizationBodyInternal(ctx huma.Context) ([]byte, error) {
	op := ctx.Operation()
	if op.BodyReadTimeout != 0 {
		controller := http.NewResponseController(humaecho.Unwrap(ctx).Response())
		deadline := time.Time{}
		if op.BodyReadTimeout > 0 {
			deadline = time.Now().Add(op.BodyReadTimeout)
		}
		if err := controller.SetReadDeadline(deadline); err != nil {
			// Some response writers do not expose connection deadlines.
			if !errors.Is(err, http.ErrNotSupported) {
				return nil, huma.Error500InternalServerError("cannot configure request body read deadline", err)
			}
		} else if op.BodyReadTimeout > 0 {
			defer func() {
				if err := controller.SetReadDeadline(time.Time{}); err != nil {
					slog.WarnContext(ctx.Context(), "failed to clear request body read deadline", "error", err)
				}
			}()
		}
	}
	reader := ctx.BodyReader()
	if reader == nil {
		return nil, nil
	}
	if closer, ok := reader.(io.Closer); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				slog.WarnContext(ctx.Context(), "failed to close request body", "error", err)
			}
		}()
	}
	if op.MaxBodyBytes > 0 {
		reader = io.LimitReader(reader, op.MaxBodyBytes)
	}
	body, err := io.ReadAll(reader)
	// Huma rejects bodies at exactly MaxBodyBytes as well as larger ones.
	if op.MaxBodyBytes > 0 && int64(len(body)) == op.MaxBodyBytes {
		return nil, huma.Error413RequestEntityTooLarge(fmt.Sprintf("request body is too large limit=%d bytes", op.MaxBodyBytes))
	}
	if err != nil {
		if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
			return nil, huma.Error408RequestTimeout("request body read timeout")
		}
		return nil, huma.Error500InternalServerError("cannot read request body", err)
	}
	return body, nil
}
