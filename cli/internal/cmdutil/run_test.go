package cmdutil

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func TestRun(t *testing.T) {
	type record struct {
		Name string `json:"name"`
	}
	renderFailure := errors.New("render failed")
	for _, tt := range []struct {
		name      string
		json      bool
		status    int
		body      string
		renderErr error
		wantErr   string
		wantText  string
		wantCalls int
	}{
		{name: "json data only", json: true, status: http.StatusOK, body: `{"success":true,"data":{"name":"arcane"}}`, wantText: "{\n  \"name\": \"arcane\"\n}\n"},
		{
			name: "unknown fields retained", json: true, status: http.StatusOK,
			body:     `{"success":true,"data":{"name":"arcane","future":{"number":9007199254740993}}}`,
			wantText: "{\n  \"name\": \"arcane\",\n  \"future\": {\n    \"number\": 9007199254740993\n  }\n}\n",
		},
		{name: "JSON skips typed decoding", json: true, status: http.StatusOK, body: `{"success":true,"data":{"name":7}}`, wantText: "{\n  \"name\": 7\n}\n"},
		{name: "null data", json: true, status: http.StatusOK, body: `{"success":true,"data":null}`, wantText: "null\n"},
		{name: "missing data", json: true, status: http.StatusOK, body: `{"success":true}`, wantText: "null\n"},
		{name: "typed render", status: http.StatusOK, body: `{"success":true,"data":{"name":"arcane"}}`, wantText: "name: arcane\n", wantCalls: 1},
		{name: "render failure", status: http.StatusOK, body: `{"success":true,"data":{"name":"arcane"}}`, renderErr: renderFailure, wantErr: "render failed", wantCalls: 1},
		{name: "http failure", status: http.StatusUnauthorized, body: `{"error":"unauthorized"}`, wantErr: "failed to get record: request failed with status 401"},
		{name: "api failure", json: true, status: http.StatusOK, body: `{"success":false,"error":"unavailable"}`, wantErr: "failed to get record: API error: unavailable"},
		{name: "malformed response", status: http.StatusOK, body: `invalid`, wantErr: "failed to get record: failed to decode response"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Method + " " + r.URL.RequestURI()
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			c, newErr := client.New(&types.Config{ServerURL: server.URL, APIKey: "test-key"})
			require.NoError(t, newErr)
			var stdout, stderr bytes.Buffer
			cmd := &cobra.Command{Use: "get", Short: "Get record"}
			cmd.SetContext(t.Context())
			cmd.Flags().Bool("json", tt.json, "")
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			oldOut, oldErr := output.Stdout(), output.Stderr()
			output.Configure(&stdout, &stderr, tt.json, true, cmd.Short)
			defer output.Configure(oldOut, oldErr, false, true, "Loading")
			renderCalls := 0
			runErr := Run(cmd, c, Spec[record]{
				Method:         http.MethodGet,
				Path:           "/api/record?details=true",
				FailureMessage: "failed to get record",
				Render: func(item record) error {
					renderCalls++
					require.Equal(t, record{Name: "arcane"}, item)
					if tt.renderErr != nil {
						return tt.renderErr
					}
					_, printErr := fmt.Fprintf(cmd.OutOrStdout(), "name: %s\n", item.Name)
					return printErr
				},
			})
			if tt.wantErr == "" {
				require.NoError(t, runErr)
			} else {
				require.ErrorContains(t, runErr, tt.wantErr)
			}
			if tt.renderErr != nil {
				require.ErrorIs(t, runErr, tt.renderErr)
			}
			require.Equal(t, "GET /api/record?details=true", <-requests)
			require.Equal(t, tt.wantCalls, renderCalls)
			require.Equal(t, tt.wantText, stdout.String())
		})
	}
}

func TestRunRequiresRender(t *testing.T) {
	cmd := &cobra.Command{Use: "get"}
	cmd.SetContext(t.Context())
	require.ErrorContains(t, Run(cmd, nil, Spec[string]{Method: http.MethodGet}), "requires Render in text mode")
}

func TestRunDelete(t *testing.T) {
	for _, tt := range []struct {
		name                       string
		force, yes, json, terminal bool
		answer, response, wantErr  string
		body                       any
		wantRequests               int
	}{
		{name: "force", force: true, wantRequests: 1},
		{name: "yes", yes: true, wantRequests: 1},
		{name: "cancel", terminal: true, answer: "n\n"},
		{name: "non TTY", wantErr: "confirmation required: re-run with --yes"},
		{name: "JSON requires consent", json: true, wantErr: "confirmation required: re-run with --yes"},
		{name: "JSON raw data", json: true, force: true, wantRequests: 1, body: map[string]bool{"remove": true}},
		{name: "JSON yes", json: true, yes: true, wantRequests: 1},
		{name: "API failure", force: true, wantRequests: 1, response: `{"success":false,"error":"denied"}`, wantErr: "failed to delete record: API error: denied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestBody, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- r.Method + " " + r.URL.Path + " " + string(requestBody)
				_, _ = fmt.Fprint(w, cmp.Or(tt.response, `{"success":true,"data":{"message":"removed","future":9007199254740993}}`))
			}))
			defer server.Close()
			c, newErr := client.NewUnauthenticated(&types.Config{ServerURL: server.URL})
			require.NoError(t, newErr)
			var stdout, stderr bytes.Buffer
			cmd := &cobra.Command{Use: "delete"}
			cmd.SetContext(t.Context())
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader("y\n"))
			cmd.Flags().Bool("json", tt.json, "")
			cmd.Flags().Bool("yes", tt.yes, "")
			if tt.terminal {
				master, terminal, terminalErr := pty.Open()
				require.NoError(t, terminalErr)
				defer func() { _ = master.Close(); _ = terminal.Close() }()
				t.Setenv("TERM", "xterm")
				cmd.SetIn(confirmationInput{Reader: strings.NewReader(tt.answer), terminal: terminal})
			}
			oldOut, oldErr := output.Stdout(), output.Stderr()
			output.Configure(&stdout, &stderr, tt.json, true, "Delete record")
			defer output.Configure(oldOut, oldErr, false, true, "Loading")
			runErr := RunDelete(cmd, c, DeleteSpec{
				Force: tt.force, Prompt: "Delete record?", Path: "/api/record", Body: tt.body,
				FailureMessage: "failed to delete record", SuccessMessage: "Record removed",
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, runErr, tt.wantErr)
			} else {
				require.NoError(t, runErr)
			}
			require.Len(t, requests, tt.wantRequests)
			if tt.wantRequests > 0 {
				request := <-requests
				require.Contains(t, request, "DELETE /api/record ")
				if tt.body != nil {
					require.Contains(t, request, `{"remove":true}`)
				}
			}
			switch {
			case tt.wantErr != "":
				require.Empty(t, stdout.String())
			case tt.wantRequests == 0:
				require.Empty(t, stdout.String())
				require.Contains(t, stderr.String(), "Cancelled\n")
			case tt.json:
				require.JSONEq(t, `{"message":"removed","future":9007199254740993}`, stdout.String())
				require.Empty(t, stderr.String())
			default:
				require.Contains(t, stdout.String(), "Record removed")
			}
		})
	}
}
