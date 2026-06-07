package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	runtimectx "github.com/getarcaneapp/arcane/cli/internal/runtime"
	"github.com/spf13/cobra"
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

var jsonOutput bool

// DoctorCmd runs environment and connection diagnostics.
var DoctorCmd = &cobra.Command{
	Use:          "doctor",
	Short:        "Run CLI diagnostics",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		app, ok := runtimectx.From(cmd.Context())
		if !ok {
			created, err := runtimectx.New(runtimectx.Options{})
			if err != nil {
				return err
			}
			app = created
		}

		cfg := app.Config()
		if cfg == nil {
			return fmt.Errorf("configuration unavailable")
		}

		rep := report{Healthy: true}
		path, _ := config.ConfigPath()
		rep.Checks = append(rep.Checks, checkResult{
			Name:    "config_path",
			Status:  "ok",
			Details: path,
		})

		if strings.TrimSpace(cfg.ServerURL) == "" {
			rep.Healthy = false
			rep.Checks = append(rep.Checks, checkResult{
				Name:    "server_url",
				Status:  "fail",
				Details: "server_url is not configured (run `arcane config set server-url <url>`)",
			})
		} else {
			rep.Checks = append(rep.Checks, checkResult{Name: "server_url", Status: "ok", Details: cfg.ServerURL})
		}

		env := cfg.DefaultEnvironment
		if app.EnvID() != "" {
			env = app.EnvID()
		}
		if strings.TrimSpace(env) == "" {
			env = "0"
		}
		rep.Checks = append(rep.Checks, checkResult{Name: "environment", Status: "ok", Details: env})

		if cfg.HasAuth() {
			rep.Checks = append(rep.Checks, checkResult{Name: "auth", Status: "ok", Details: "credentials configured"})
		} else {
			rep.Healthy = false
			rep.Checks = append(rep.Checks, checkResult{
				Name:    "auth",
				Status:  "fail",
				Details: "authentication is not configured (run `arcane config set api-key ...` or `arcane auth login`)",
			})
		}

		if strings.TrimSpace(cfg.ServerURL) != "" && cfg.HasAuth() {
			c, err := app.Client()
			if err != nil {
				rep.Healthy = false
				rep.Checks = append(rep.Checks, checkResult{Name: "api_connection", Status: "fail", Details: err.Error()})
			} else {
				if err := c.TestConnection(cmd.Context()); err != nil {
					rep.Healthy = false
					rep.Checks = append(rep.Checks, checkResult{Name: "api_connection", Status: "fail", Details: err.Error()})
				} else {
					rep.Checks = append(rep.Checks, checkResult{Name: "api_connection", Status: "ok", Details: "connected"})
				}
			}
		} else if strings.TrimSpace(cfg.ServerURL) == "" {
			rep.Checks = append(rep.Checks, checkResult{Name: "api_connection", Status: "skip", Details: "server_url missing"})
		} else {
			rep.Checks = append(rep.Checks, checkResult{Name: "api_connection", Status: "skip", Details: "auth missing"})
		}

		if jsonOutput || app.IsJSON() {
			b, err := json.MarshalIndent(rep, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal doctor output: %w", err)
			}
			fmt.Println(string(b))
		} else {
			output.Header("Arcane CLI Diagnostics")
			rows := make([][]string, 0, len(rep.Checks))
			for _, item := range rep.Checks {
				rows = append(rows, []string{item.Name, item.Status, item.Details})
			}
			output.Table([]string{"CHECK", "STATUS", "DETAILS"}, rows)
			if rep.Healthy {
				output.Success("Doctor checks passed")
			} else {
				output.Warning("Doctor found issues")
			}
		}

		if !rep.Healthy {
			return fmt.Errorf("diagnostics failed")
		}
		return nil
	},
}

func init() {
	DoctorCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
