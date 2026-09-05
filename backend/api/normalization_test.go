package api

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	authtypes "github.com/getarcaneapp/arcane/types/v2/auth"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
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
	t.Run("identity fields pass through unchanged", func(t *testing.T) {
		router := echo.New()
		api := humaecho.New(router, huma.DefaultConfig("test", "1"))
		registerNormalizationInternal(api)
		var received usertypes.CreateUser
		huma.Register(api, huma.Operation{OperationID: "create", Method: http.MethodPost, Path: "/create"}, func(_ context.Context, input *struct{ Body usertypes.CreateUser }) (*struct{}, error) {
			received = input.Body
			return &struct{}{}, nil
		})
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/create", strings.NewReader(`{"username":" Jose\u0301 ","email":" Jose\u0301@example.com ","displayName":" Jose\u0301 ","password":" password "}`))
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
		require.Equal(t, " Jose\u0301 ", received.Username)
		require.Equal(t, " Jose\u0301@example.com ", *received.Email)
		require.Equal(t, "José", *received.DisplayName)
		require.Equal(t, " password ", received.Password)

		var login authtypes.Login
		huma.Register(api, huma.Operation{OperationID: "login", Method: http.MethodPost, Path: "/login"}, func(_ context.Context, input *struct{ Body authtypes.Login }) (*struct{}, error) {
			login = input.Body
			return &struct{}{}, nil
		})
		recorder = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":" Jose\u0301 ","password":" password "}`))
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
		require.Equal(t, " Jose\u0301 ", login.Username)
		require.Equal(t, " password ", login.Password)
	})
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

type normalizationTimeoutReader struct {
	body     *strings.Reader
	closeErr error
	closed   bool
}

func (r *normalizationTimeoutReader) Read(data []byte) (int, error) {
	if r.body != nil {
		return r.body.Read(data)
	}
	return 0, normalizationTimeoutError{}
}

func (r *normalizationTimeoutReader) Close() error {
	r.closed = true
	return r.closeErr
}

type normalizationResponseRecorder struct {
	*httptest.ResponseRecorder

	setupErr error
	resetErr error
	calls    int
}

func (r *normalizationResponseRecorder) SetReadDeadline(deadline time.Time) error {
	r.calls++
	if r.calls == 1 {
		return r.setupErr
	}
	if deadline.IsZero() {
		return r.resetErr
	}
	return nil
}

type normalizationTimeoutError struct{}

func (normalizationTimeoutError) Error() string   { return "read timed out" }
func (normalizationTimeoutError) Timeout() bool   { return true }
func (normalizationTimeoutError) Temporary() bool { return true }

func TestNormalizationReadTimeout(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	for _, tc := range []struct {
		name                         string
		timeout                      time.Duration
		readTimeout                  bool
		setupErr, resetErr, closeErr error
		status                       int
		logMessage                   string
	}{
		{name: "read timeout", timeout: time.Second, readTimeout: true, status: http.StatusRequestTimeout},
		{name: "unsupported deadline", timeout: time.Second, setupErr: fmt.Errorf("writer: %w", http.ErrNotSupported), status: http.StatusNoContent},
		{name: "setting deadline fails", timeout: time.Second, setupErr: errors.New("deadline failed"), status: http.StatusInternalServerError},
		{name: "disabling deadline fails", timeout: -1, setupErr: errors.New("deadline failed"), status: http.StatusInternalServerError},
		{name: "reset fails", timeout: time.Second, resetErr: errors.New("reset failed"), status: http.StatusNoContent, logMessage: "failed to clear request body read deadline"},
		{name: "close fails", timeout: time.Second, closeErr: errors.New("close failed"), status: http.StatusNoContent, logMessage: "failed to close request body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			router := echo.New()
			api := humaecho.New(router, huma.DefaultConfig("test", "1"))
			registerNormalizationInternal(api)
			called := false
			huma.Register(api, huma.Operation{OperationID: "timeout", Method: http.MethodPost, Path: "/timeout", BodyReadTimeout: tc.timeout}, func(_ context.Context, _ *struct{ Body normalizationTestBody }) (*struct{}, error) {
				called = true
				return &struct{}{}, nil
			})
			recorder := &normalizationResponseRecorder{ResponseRecorder: httptest.NewRecorder(), setupErr: tc.setupErr, resetErr: tc.resetErr}
			body := &normalizationTimeoutReader{closeErr: tc.closeErr}
			if !tc.readTimeout {
				body.body = strings.NewReader(`{"name":"x"}`)
			}
			request := httptest.NewRequest(http.MethodPost, "/timeout", nil)
			request.Body = body
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.Equal(t, tc.status == http.StatusNoContent, called)
			if tc.status != http.StatusInternalServerError {
				require.True(t, body.closed)
			}
			if tc.logMessage != "" {
				require.Contains(t, logs.String(), tc.logMessage)
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
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
