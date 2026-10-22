package auth

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/ci"
	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

const (
	maxFederatedErrorBody    = 4096
	maxFederatedResponseBody = 1 << 20
)

func newFederatedCmd() *cobra.Command {
	var opts struct {
		server     string
		audience   string
		provider   string
		token      string
		tokenFile  string
		tokenStdin bool
		persist    bool
		export     bool
	}
	cmd := &cobra.Command{
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
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmdutil.JSONOutputEnabled(cmd) && opts.export {
				return errors.New("flags --json and --export cannot be used together")
			}
			audience := opts.audience

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if strings.TrimSpace(opts.server) != "" {
				cfg.ServerURL = strings.TrimSpace(opts.server)
			}
			if strings.TrimSpace(audience) == "" {
				audience = cfg.FederatedAudience
			}

			subjectToken, tokenSource, err := resolveFederatedSubjectToken(cmd, opts.provider, audience, opts.token, opts.tokenFile, opts.tokenStdin)
			if err != nil {
				return err
			}

			c, err := client.NewUnauthenticated(cfg)
			if err != nil {
				return err
			}

			tokenResp, err := exchangeFederatedToken(cmd, c, strings.TrimSpace(subjectToken), strings.TrimSpace(audience))
			if err != nil {
				return err
			}
			if tokenResp.AccessToken == "" {
				return errors.New("federated token exchange failed: empty access token")
			}

			expiresAt := time.Now().UTC().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

			if opts.persist {
				cfg.JWTToken = tokenResp.AccessToken
				cfg.APIKey = ""
				cfg.RefreshToken = ""
				if saveErr := config.SaveTokens(cmd.Context(), cfg); saveErr != nil {
					return fmt.Errorf("failed to save federated token: %w", saveErr)
				}
			}

			switch {
			case cmdutil.JSONOutputEnabled(cmd):
				return cmdutil.PrintJSON(struct {
					Token           string `json:"token"`
					TokenType       string `json:"tokenType"`
					ExpiresIn       int    `json:"expiresIn"`
					ExpiresAt       string `json:"expiresAt"`
					IssuedTokenType string `json:"issuedTokenType"`
					Source          string `json:"source"`
					Persisted       bool   `json:"persisted"`
				}{
					Token:           tokenResp.AccessToken,
					TokenType:       tokenResp.TokenType,
					ExpiresIn:       tokenResp.ExpiresIn,
					ExpiresAt:       expiresAt.Format(time.RFC3339),
					IssuedTokenType: tokenResp.IssuedTokenType,
					Source:          tokenSource,
					Persisted:       opts.persist,
				})
			case opts.export:
				_, _ = fmt.Fprintf(output.Stdout(), "export ARCANE_TOKEN=%s\n", "'"+strings.ReplaceAll(tokenResp.AccessToken, "'", "'\\''")+"'")
				_, _ = fmt.Fprintf(output.Stdout(), "export ARCANE_TOKEN_EXPIRES_AT=%s\n", "'"+strings.ReplaceAll(expiresAt.Format(time.RFC3339), "'", "'\\''")+"'")
			default:
				output.Success("Federated token exchange successful")
				output.KeyValue("Token source", tokenSource)
				output.KeyValue("Expires at", expiresAt.Format(time.RFC3339))
				if opts.persist {
					path, _ := config.AuthPath()
					output.KeyValue("Session saved to", path)
				} else {
					output.Info("Use --export, --json, or --persist to consume the token.")
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&opts.server, "server", "", "Arcane server URL for this exchange")
	cmd.Flags().StringVar(&opts.audience, "audience", "", "Audience to request from the external OIDC provider")
	cmd.Flags().StringVar(&opts.token, "token", "", "External OIDC token to exchange")
	cmd.Flags().StringVar(&opts.tokenFile, "token-file", "", "Path to a file containing the external OIDC token")
	cmd.Flags().BoolVar(&opts.tokenStdin, "token-stdin", false, "Read the external OIDC token from stdin")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.provider, "provider", "", ci.ProviderAuto, "OIDC token source",
		ci.ProviderAuto, ci.ProviderGitHub, ci.ProviderGitLab, ci.ProviderGeneric)
	cmd.Flags().BoolVar(&opts.persist, "persist", false, "Persist the exchanged Arcane token in CLI config")
	cmd.Flags().BoolVar(&opts.export, "export", false, "Print shell exports for ARCANE_TOKEN and ARCANE_TOKEN_EXPIRES_AT")
	return cmd
}

func resolveFederatedSubjectToken(cmd *cobra.Command, provider, audience, federatedToken, federatedTokenFile string, federatedTokenStdin bool) (string, string, error) {
	token := federatedToken
	if strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), "flag", nil
	}

	if strings.TrimSpace(federatedTokenFile) != "" {
		data, err := os.ReadFile(strings.TrimSpace(federatedTokenFile))
		if err != nil {
			return "", "", fmt.Errorf("failed to read token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return "", "", errors.New("token file is empty")
		}
		return token, "file", nil
	}

	input := cmd.InOrStdin()
	stdinHasData := false
	if file, ok := input.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := file.Stat()
		stdinHasData = err == nil && info.Mode()&os.ModeCharDevice == 0
	}
	if federatedTokenStdin || stdinHasData {
		data, err := output.ReadWithContext(cmd.Context(), input, io.ReadAll)
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

func exchangeFederatedToken(cmd *cobra.Command, c *client.Client, subjectToken, audience string) (*types.FederatedTokenResponse, error) {
	form := url.Values{
		"grant_type":           {types.TokenExchangeGrantType},
		"subject_token":        {subjectToken},
		"subject_token_type":   {types.SubjectTokenTypeJWT},
		"requested_token_type": {types.RequestedTokenTypeAccessJWT},
	}
	if strings.TrimSpace(audience) != "" {
		form.Set("audience", strings.TrimSpace(audience))
	}

	resp, err := c.RequestRaw(cmd.Context(), http.MethodPost, clitypes.AuthFederated(), strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if err != nil {
		return nil, fmt.Errorf("federated token exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxFederatedErrorBody))
		var oauthErr struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"` //nolint:tagliatelle // RFC 6749 wire shape is snake_case.
		}
		_ = json.Unmarshal(body, &oauthErr)
		message := cmp.Or(strings.TrimSpace(oauthErr.ErrorDescription), strings.TrimSpace(oauthErr.Error), strings.TrimSpace(string(body)), "request rejected")
		return nil, fmt.Errorf("federated token exchange failed (status %d): %s", resp.StatusCode, message)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFederatedResponseBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read federated token response: %w", err)
	}

	var tokenResp types.FederatedTokenResponse
	if unmarshalErr := json.Unmarshal(body, &tokenResp); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse federated token response: %w", unmarshalErr)
	}
	return &tokenResp, nil
}
