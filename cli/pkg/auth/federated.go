package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/ci"
	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/config"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	federatedtypes "github.com/getarcaneapp/arcane/types/federated"
	"github.com/spf13/cobra"
)

const maxFederatedErrorBody = 4096

var federatedCmd = &cobra.Command{
	Use:          "federated",
	Short:        "Exchange a CI OIDC token for a short-lived Arcane token",
	SilenceUsage: true,
	Long: `Exchange an external OIDC token for a short-lived Arcane bearer token.

GitHub Actions example:
  permissions: { id-token: write, contents: read }
  steps:
    - run: |
        eval "$(arcane-cli auth federated \
          --server https://arcane.example.com \
          --audience https://arcane.example.com --export)"
    - run: arcane-cli projects up my-app`,
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("server")
		audience, _ := cmd.Flags().GetString("audience")
		provider, _ := cmd.Flags().GetString("provider")
		persist, _ := cmd.Flags().GetBool("persist")
		exportOutput, _ := cmd.Flags().GetBool("export")

		if cmdutil.JSONOutputEnabled(cmd) && exportOutput {
			return errors.New("--json and --export cannot be used together")
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if strings.TrimSpace(server) != "" {
			cfg.ServerURL = strings.TrimSpace(server)
		}
		if strings.TrimSpace(audience) == "" {
			audience = cfg.FederatedAudience
		}

		subjectToken, tokenSource, err := resolveFederatedSubjectTokenInternal(cmd, provider, audience)
		if err != nil {
			return err
		}

		c, err := client.NewUnauthenticated(cfg)
		if err != nil {
			return err
		}

		tokenResp, err := exchangeFederatedTokenInternal(cmd, c, strings.TrimSpace(subjectToken), strings.TrimSpace(audience))
		if err != nil {
			return err
		}
		if tokenResp.AccessToken == "" {
			return errors.New("federated token exchange failed: empty access token")
		}

		expiresAt := time.Now().UTC().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

		if persist {
			cfg.JWTToken = tokenResp.AccessToken
			cfg.APIKey = ""
			cfg.RefreshToken = ""
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save federated token: %w", err)
			}
		}

		switch {
		case cmdutil.JSONOutputEnabled(cmd):
			resultBytes, err := json.MarshalIndent(map[string]any{
				"token":           tokenResp.AccessToken,
				"tokenType":       tokenResp.TokenType,
				"expiresIn":       tokenResp.ExpiresIn,
				"expiresAt":       expiresAt.Format(time.RFC3339),
				"issuedTokenType": tokenResp.IssuedTokenType,
				"source":          tokenSource,
				"persisted":       persist,
			}, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
		case exportOutput:
			fmt.Printf("export ARCANE_TOKEN=%s\n", shellQuoteInternal(tokenResp.AccessToken))
			fmt.Printf("export ARCANE_TOKEN_EXPIRES_AT=%s\n", shellQuoteInternal(expiresAt.Format(time.RFC3339)))
		default:
			output.Success("Federated token exchange successful")
			output.KeyValue("Token source", tokenSource)
			output.KeyValue("Expires at", expiresAt.Format(time.RFC3339))
			if persist {
				path, _ := config.ConfigPath()
				output.KeyValue("JWT token saved to config", path)
			} else {
				output.Info("Use --export, --json, or --persist to consume the token.")
			}
		}

		return nil
	},
}

func init() {
	AuthCmd.AddCommand(federatedCmd)

	federatedCmd.Flags().String("server", "", "Arcane server URL for this exchange")
	federatedCmd.Flags().String("audience", "", "Audience to request from the external OIDC provider")
	federatedCmd.Flags().String("token", "", "External OIDC token to exchange")
	federatedCmd.Flags().String("token-file", "", "Path to a file containing the external OIDC token")
	federatedCmd.Flags().Bool("token-stdin", false, "Read the external OIDC token from stdin")
	federatedCmd.Flags().String("provider", ci.ProviderAuto, "OIDC token source: auto, github, gitlab, or generic")
	federatedCmd.Flags().Bool("persist", false, "Persist the exchanged Arcane token in CLI config")
	federatedCmd.Flags().Bool("export", false, "Print shell exports for ARCANE_TOKEN and ARCANE_TOKEN_EXPIRES_AT")
	federatedCmd.Flags().Bool("json", false, "Output in JSON format")
}

func resolveFederatedSubjectTokenInternal(cmd *cobra.Command, provider string, audience string) (string, string, error) {
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), "flag", nil
	}

	tokenFile, _ := cmd.Flags().GetString("token-file")
	if strings.TrimSpace(tokenFile) != "" {
		data, err := os.ReadFile(strings.TrimSpace(tokenFile))
		if err != nil {
			return "", "", fmt.Errorf("failed to read token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return "", "", errors.New("token file is empty")
		}
		return token, "file", nil
	}

	tokenStdin, _ := cmd.Flags().GetBool("token-stdin")
	if tokenStdin || stdinHasDataInternal() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", fmt.Errorf("failed to read token from stdin: %w", err)
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return "", "", errors.New("stdin token is empty")
		}
		return token, "stdin", nil
	}

	return ci.DetectToken(cmd.Context(), provider, audience, os.Getenv, &http.Client{Timeout: 10 * time.Second})
}

func exchangeFederatedTokenInternal(cmd *cobra.Command, c *client.Client, subjectToken string, audience string) (*federatedtypes.FederatedTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", federatedtypes.TokenExchangeGrantType)
	form.Set("subject_token", subjectToken)
	form.Set("subject_token_type", federatedtypes.SubjectTokenTypeJWT)
	form.Set("requested_token_type", federatedtypes.RequestedTokenTypeAccessJWT)
	if strings.TrimSpace(audience) != "" {
		form.Set("audience", strings.TrimSpace(audience))
	}

	resp, err := c.RequestRaw(cmd.Context(), http.MethodPost, types.Endpoints.AuthFederated(), strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if err != nil {
		return nil, fmt.Errorf("federated token exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFederatedErrorBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read federated token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("federated token exchange failed (status %d): %s", resp.StatusCode, redactedFederatedExchangeMessageInternal(body))
	}

	var tokenResp federatedtypes.FederatedTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse federated token response: %w", err)
	}
	return &tokenResp, nil
}

func redactedFederatedExchangeMessageInternal(body []byte) string {
	var oauthErr struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"` //nolint:tagliatelle // RFC 6749 wire shape is snake_case.
	}
	if err := json.Unmarshal(body, &oauthErr); err == nil {
		if strings.TrimSpace(oauthErr.ErrorDescription) != "" {
			return strings.TrimSpace(oauthErr.ErrorDescription)
		}
		if strings.TrimSpace(oauthErr.Error) != "" {
			return strings.TrimSpace(oauthErr.Error)
		}
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return "request rejected"
	}
	return msg
}

func stdinHasDataInternal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) == 0
}

func shellQuoteInternal(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
