package cmdutil

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func TestRunScopedMerged(t *testing.T) {
	type row struct {
		types.EnvironmentTag

		Name string `json:"name"`
	}
	const tagged = `{"success":true,"data":[{"environmentId":"0","environmentName":"Alpha","name":"web"},{"environmentId":"b","environmentName":"Beta","name":"db"}],"pagination":{"totalItems":7}}`
	for _, tt := range []struct {
		name, environments, body, scope, wantErr, limit string
		json                                            bool
		wantOut                                         []string
	}{
		{
			name: "table names rows from their tags", environments: `[{"id":"b","name":"Beta","enabled":true},{"id":"0","name":"Alpha","enabled":true}]`,
			body: tagged, scope: "/api/environments/0,b/rows",
			wantOut: []string{"ENVIRONMENT", "Alpha", "web", "Beta", "db", "Showing: 2/7 rows"},
		},
		{
			name: "single environment names untagged rows from the scope", environments: `[{"id":"0","name":"Alpha","enabled":true}]`,
			body: `{"success":true,"data":[{"name":"web"}],"pagination":{"totalItems":1}}`, scope: "/api/environments/0/rows",
			wantOut: []string{"ENVIRONMENT", "Alpha", "web"},
		},
		{
			name: "failed environments are named and returned", environments: `[{"id":"0","name":"Alpha","enabled":true},{"id":"b","name":"Beta","enabled":true}]`,
			body: `{"success":true,"data":[{"environmentId":"0","environmentName":"Alpha","name":"web"}],"pagination":{"totalItems":1},` +
				`"failedEnvironments":[{"environment":{"id":"b","name":"b"},"message":"offline"}]}`,
			scope: "/api/environments/0,b/rows", wantErr: "Beta (b): offline", wantOut: []string{"Alpha", "web"},
		},
		{
			name:         "environments beyond the limit are reported and not sent",
			environments: `[{"id":"0","name":"Alpha","enabled":true},{"id":"b","name":"Beta","enabled":true},{"id":"c","name":"Gamma","enabled":true}]`,
			body:         tagged, scope: "/api/environments/0,b/rows", limit: "2",
			wantErr: "Gamma (c): not included: more than 2 environments are shown at once", wantOut: []string{"Alpha", "web", "Beta", "db"},
		},
		{
			name: "JSON passes the merged response through", environments: `[{"id":"0","name":"Alpha","enabled":true},{"id":"b","name":"Beta","enabled":true}]`,
			body: tagged, scope: "/api/environments/0,b/rows", json: true,
			wantOut: []string{`"environmentName": "Beta"`, `"totalItems": 7`},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == clitypes.Environments() {
					_, _ = fmt.Fprintf(w, `{"success":true,"data":%s}`, tt.environments)
					return
				}
				if r.URL.Path == clitypes.SettingsPublic("0") {
					_, _ = fmt.Fprintf(w, `{"settings":[{"key":"allEnvironmentsLimit","type":"number","value":%q}]}`, cmp.Or(tt.limit, "64"))
					return
				}
				requests = append(requests, r.URL.Path)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			t.Setenv("HOME", t.TempDir())
			require.NoError(t, config.SetConfigPath(filepath.Join(t.TempDir(), "arcanecli.yml")))
			t.Cleanup(func() { require.NoError(t, config.SetConfigPath("")) })
			cfg := config.DefaultConfig()
			cfg.ServerURL, cfg.APIKey = server.URL, "test-key"
			require.NoError(t, config.Save(t.Context(), cfg))
			app, appErr := runtime.New(runtime.Options{})
			require.NoError(t, appErr)

			var stdout, stderr bytes.Buffer
			oldOut, oldErr := output.Stdout(), output.Stderr()
			output.Configure(&stdout, &stderr, tt.json, true, "List rows")
			defer output.Configure(oldOut, oldErr, false, true, "Loading")
			cmd := &cobra.Command{Use: "list", Short: "List rows"}
			SetScope(ScopeMerged, cmd)
			cmd.Flags().Bool("json", tt.json, "")
			cmd.SetContext(context.WithValue(t.Context(), runtime.AppContextKey, app))
			runErr := RunScoped(cmd, nil, func(command *cobra.Command, _ []string) error {
				c, err := ClientFromCommand(command)
				require.NoError(t, err)
				return RunList(command, c, ListSpec[row]{
					Resource: "rows", Endpoint: clitypes.Environment(c.EnvID()) + "/rows", Headers: []string{"NAME"},
					Row: func(item row) []string { return []string{item.Name} },
				})
			})
			if tt.wantErr == "" {
				require.NoError(t, runErr)
			} else {
				require.ErrorContains(t, runErr, tt.wantErr)
				require.Contains(t, stderr.String(), tt.wantErr)
			}
			require.Equal(t, []string{tt.scope}, requests)
			for _, want := range tt.wantOut {
				require.Contains(t, stdout.String(), want)
			}
			require.True(t, app.AllEnvironments(), "selection is restored")
		})
	}
}

func TestRunScopedRefusals(t *testing.T) {
	for _, tt := range []struct {
		name, scope, status, wantErr string
	}{
		{name: "missing scope", wantErr: "list has no environment scope"},
		{name: "forbidden discovery", scope: ScopeRead, status: "403", wantErr: "pass --env <id>"},
		{name: "mutation confirms without names", scope: ScopeAll, status: "200", wantErr: "(List rows in all 2 environments?)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status == "403" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_, _ = fmt.Fprint(w, `{"success":true,"data":[{"id":"0","name":"Alpha","enabled":true},{"id":"b","name":"Beta","enabled":true}]}`)
			}))
			defer server.Close()
			t.Setenv("HOME", t.TempDir())
			require.NoError(t, config.SetConfigPath(filepath.Join(t.TempDir(), "arcanecli.yml")))
			t.Cleanup(func() { require.NoError(t, config.SetConfigPath("")) })
			require.NoError(t, config.Save(t.Context(), &clitypes.Config{
				ServerURL: server.URL,
				APIKey:    "test-key",
			}))
			app, appErr := runtime.New(runtime.Options{OutputMode: runtime.OutputModeJSON})
			require.NoError(t, appErr)
			cmd := &cobra.Command{Use: "list", Short: "List rows"}
			if tt.scope != "" {
				SetScope(tt.scope, cmd)
			}
			cmd.Flags().Bool("json", true, "")
			cmd.SetContext(context.WithValue(t.Context(), runtime.AppContextKey, app))
			ran := false
			runErr := RunScoped(cmd, nil, func(*cobra.Command, []string) error {
				ran = true
				return nil
			})
			require.ErrorContains(t, runErr, tt.wantErr)
			require.NotContains(t, runErr.Error(), "Alpha")
			require.False(t, ran)
		})
	}
}
