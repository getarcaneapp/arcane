package cli

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/spf13/cobra"
)

const (
	defaultHealthTimeout = 5 * time.Second
	defaultHealthPort    = "3552"
	healthHost           = "127.0.0.1"
	healthPath           = "/api/health"
)

var healthTimeout time.Duration

var HealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check API health",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		timeout := healthTimeout
		if timeout <= 0 {
			timeout = defaultHealthTimeout
		}

		if err := runHealthCommandInternal(cmd.Context(), cfg, timeout); err != nil {
			return err
		}

		return nil
	},
}

func buildHealthURLInternal(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", errors.New("health config is nil")
	}

	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		port = defaultHealthPort
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", errors.WrapIff(err, "invalid health port %q", port)
	}

	hostPort := net.JoinHostPort(healthHost, port)

	u := &url.URL{
		Scheme: "http",
		Host:   hostPort,
		Path:   healthPath,
	}

	return u.String(), nil
}

func runHealthCommandInternal(ctx context.Context, cfg *config.Config, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultHealthTimeout
	}

	healthURL, err := buildHealthURLInternal(cfg)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodHead, healthURL, nil)
	if err != nil {
		return errors.WrapIf(err, "health check request creation failed")
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(request)
	if err != nil {
		return errors.WrapIf(err, "health check request failed")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

func init() {
	HealthCmd.Flags().DurationVar(&healthTimeout, "timeout", defaultHealthTimeout, "Health check timeout (e.g. 5s)")
	rootCmd.AddCommand(HealthCmd)
}
