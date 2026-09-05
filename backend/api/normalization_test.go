package api

import (
	"context"

	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type normalizationTestBody struct {
	Name     string  `json:"name" minLength:"1" maxLength:"1" unorm:"nfc" trim:"true"`
	Optional *string `json:"optional,omitempty" unorm:"nfc" trim:"true"`
	Password string  `json:"password,omitempty"`
	Number   uint64  `json:"number,omitempty"`
}

func TestNormalizationBeforeHumaValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		status int
	}{
		{"normalized length", `{"name":" e\u0301 ","password":" e\u0301 ","number":9007199254740993}`, http.StatusOK},
		{"required whitespace", `{"name":" \t "}`, http.StatusUnprocessableEntity},
		{"too long", `{"name":" ab "}`, http.StatusUnprocessableEntity},
		{"wrong type", `{"name":12}`, http.StatusUnprocessableEntity},
		{"malformed", `{"name":`, http.StatusBadRequest},
		{"missing required", `{}`, http.StatusUnprocessableEntity},
		{"empty required body", ``, http.StatusBadRequest},
		{"nil optional", `{"name":"x","optional":null}`, http.StatusOK},
		{"empty optional", `{"name":"x","optional":" "}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := echo.New()
			cfg := huma.DefaultConfig("test", "1")
			cfg.Formats = map[string]huma.Format{"application/json": jsonV2Format, "json": jsonV2Format}
			api := humaecho.New(router, cfg)
			registerNormalizationInternal(api)
			called := false
			var received normalizationTestBody
			huma.Register(api, huma.Operation{OperationID: "normalize", Method: http.MethodPost, Path: "/normalize"}, func(_ context.Context, input *struct{ Body normalizationTestBody }) (*struct{ Body normalizationTestBody }, error) {
				called = true
				received = input.Body
				return &struct{ Body normalizationTestBody }{Body: input.Body}, nil
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/normalize", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.Equal(t, tc.status == http.StatusOK, called)
			if tc.status == http.StatusOK {
				var result normalizationTestBody
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
				switch tc.name {
				case "normalized length":
					require.Equal(t, "é", result.Name)
					require.Equal(t, " e\u0301 ", result.Password)
					require.Equal(t, uint64(9007199254740993), result.Number)
					require.Nil(t, result.Optional)
				case "nil optional":
					require.Nil(t, result.Optional)
				case "empty optional":
					require.NotNil(t, received.Optional)
					require.Empty(t, *received.Optional)
				}
			}
		})
	}
}

func TestNormalizationBodyLimits(t *testing.T) {
	router := echo.New()
	api := humaecho.New(router, huma.DefaultConfig("test", "1"))
	registerNormalizationInternal(api)
	huma.Register(api, huma.Operation{OperationID: "limit", Method: http.MethodPost, Path: "/limit", MaxBodyBytes: 32}, func(_ context.Context, _ *struct{ Body normalizationTestBody }) (*struct{}, error) {
		t.Fatal("oversized body must not reach application code")
		return nil, nil
	})
	for _, size := range []int{32, 33, 128} {
		recorder := httptest.NewRecorder()
		body := `{"name":"x"}` + strings.Repeat(" ", size-len(`{"name":"x"}`))
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/limit", strings.NewReader(body)))
		require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	}
}

type normalizationTimeoutReader struct{}

func (normalizationTimeoutReader) Read(_ []byte) (int, error) {
	return 0, normalizationTimeoutError{}
}

type normalizationTimeoutError struct{}

func (normalizationTimeoutError) Error() string   { return "read timed out" }
func (normalizationTimeoutError) Timeout() bool   { return true }
func (normalizationTimeoutError) Temporary() bool { return true }

func TestNormalizationReadTimeout(t *testing.T) {
	router := echo.New()
	api := humaecho.New(router, huma.DefaultConfig("test", "1"))
	registerNormalizationInternal(api)
	huma.Register(api, huma.Operation{OperationID: "timeout", Method: http.MethodPost, Path: "/timeout", BodyReadTimeout: time.Second}, func(_ context.Context, _ *struct{ Body normalizationTestBody }) (*struct{}, error) {
		t.Fatal("timed out body must not reach application code")
		return nil, nil
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/timeout", nil)
	request.Body = io.NopCloser(normalizationTimeoutReader{})
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusRequestTimeout, recorder.Code)
}

func TestNormalizationRejectsInvalidTagsAtRegistration(t *testing.T) {
	router := echo.New()
	api := humaecho.New(router, huma.DefaultConfig("test", "1"))
	registerNormalizationInternal(api)
	require.Panics(t, func() {
		huma.Register(api, huma.Operation{OperationID: "invalid", Method: http.MethodPost, Path: "/invalid"}, func(_ context.Context, _ *struct {
			Body struct {
				Name string `json:"name" unorm:"invalid"`
			}
		}) (*struct{}, error) {
			return nil, nil
		})
	})
}

func TestEchoSerializerNormalization(t *testing.T) {
	router := echo.New()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":" e\u0301 ","password":" secret "}`))
	ctx := router.NewContext(request, httptest.NewRecorder())
	var result normalizationTestBody
	require.NoError(t, (jsonV2Serializer{}).Deserialize(ctx, &result))
	require.Equal(t, "é", result.Name)
	require.Equal(t, " secret ", result.Password)
}

func TestEchoSerializerRejectsNormalizedEmptyName(t *testing.T) {
	router := echo.New()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":" "}`))
	ctx := router.NewContext(request, httptest.NewRecorder())
	var result normalizationTestBody
	err := (jsonV2Serializer{}).Deserialize(ctx, &result)
	var httpError *echo.HTTPError
	require.ErrorAs(t, err, &httpError)
	require.Equal(t, http.StatusUnprocessableEntity, httpError.Code)
}
