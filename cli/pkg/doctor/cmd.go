package doctor

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/runtime"
)

type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

type report struct {
	Healthy bool          `json:"healthy"`
	Checks  []checkResult `json:"checks"`
}

// Command runs environment and connection diagnostics.
func Command() *cobra.Command {
	return &cobra.Command{
		Use:          "doctor",
		Short:        "Run CLI diagnostics",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, ok := runtime.From(cmd.Context())
			if !ok {
				created, err := runtime.New(runtime.Options{})
				if err != nil {
					return err
				}
				app = created
			}

			cfg := app.Config()
			if cfg == nil {
				return errors.New("configuration unavailable")
			}

			path, _ := config.ConfigPath()
			env := cmp.Or(app.EnvID(), cfg.DefaultEnvironment)
			if strings.TrimSpace(env) == "" {
				env = "0"
			}
			serverConfigured, hasAuth := strings.TrimSpace(cfg.ServerURL) != "", cfg.HasAuth()
			checks := []checkResult{
				{Name: "config_path", Status: "ok", Details: path},
				{
					Name:    "server_url",
					Status:  kit.Ternary(serverConfigured, "ok", "fail"),
					Details: kit.Ternary(serverConfigured, cfg.ServerURL, "server_url is not configured (run `arcane config set server-url <url>`)"),
				},
				{Name: "environment", Status: "ok", Details: env},
				{
					Name:    "auth",
					Status:  kit.Ternary(hasAuth, "ok", "fail"),
					Details: kit.Ternary(hasAuth, "credentials configured", "authentication is not configured (run `arcane config set api-key ...` or `arcane auth login`)"),
				},
			}

			switch {
			case !serverConfigured:
				checks = append(checks, checkResult{Name: "api_connection", Status: "skip", Details: "server_url missing"})
			case !hasAuth:
				checks = append(checks, checkResult{Name: "api_connection", Status: "skip", Details: "auth missing"})
			default:
				c, err := app.Client()
				if err == nil {
					err = c.TestConnection(cmd.Context())
				}
				if err != nil {
					checks = append(checks, checkResult{Name: "api_connection", Status: "fail", Details: err.Error()})
				} else {
					checks = append(checks, checkResult{Name: "api_connection", Status: "ok", Details: "connected"})
				}
			}
			rep := report{
				Healthy: !slices.ContainsFunc(checks, func(item checkResult) bool { return item.Status == "fail" }),
				Checks:  checks,
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				if printErr := cmdutil.PrintJSON(rep); printErr != nil {
					return printErr
				}
			} else {
				output.Header("Arcane CLI Diagnostics")
				rows := make([][]string, len(rep.Checks))
				for i, item := range rep.Checks {
					rows[i] = []string{item.Name, item.Status, item.Details}
				}
				if tableErr := output.Table([]string{"CHECK", "STATUS", "DETAILS"}, rows); tableErr != nil {
					return tableErr
				}
				if rep.Healthy {
					output.Success("Doctor checks passed")
				} else {
					output.Warning("Doctor found issues")
				}
			}

			if !rep.Healthy {
				return errors.New("diagnostics failed")
			}
			return nil
		},
	}
}
