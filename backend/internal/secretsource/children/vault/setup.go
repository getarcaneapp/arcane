package vault

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"getarcane.dev/arcane/v3/pkg/secretapi"
	"getarcane.dev/arcane/v3/pkg/vault"
	"getarcane.dev/types/v3"
)

type (
	// Setup writes project secrets with the source's setup token. The deploy
	// token is never used to write.
	Setup struct {
		client *vault.Client
		// current is the secret at the target from the last RemoteValues call,
		// with its version, so Write can merge and detect concurrent changes.
		current        map[string]jsontext.Value
		currentVersion int
		currentLoaded  bool
	}
)

var (
	// ErrNoSetupToken means the source has no setup token, so it cannot write.
	ErrNoSetupToken = errors.New("the source has no setup token")

	unsafePathChars = regexp.MustCompile(`[^a-z0-9._-]+`)
)

// NewSetup builds a writer that authenticates with setupToken.
func NewSetup(httpClient *http.Client, settings types.SecretSourceVaultSettings, setupToken string) (*Setup, error) {
	if !settings.SetupToken || strings.TrimSpace(setupToken) == "" {
		return nil, ErrNoSetupToken
	}
	client, err := vault.NewClient(httpClient, vault.Config{
		Address:    settings.Address,
		Namespace:  settings.Namespace,
		AuthMethod: vault.AuthToken,
		Secret:     setupToken,
	})
	if err != nil {
		return nil, err
	}
	return &Setup{client: client}, nil
}

// SuggestPath turns an Arcane project name into a KV path such as
// "arcane/immich".
func SuggestPath(projectName string) string {
	slug := strings.Trim(unsafePathChars.ReplaceAllString(strings.ToLower(projectName), "-"), "-.")
	if slug == "" {
		slug = "project"
	}
	return "arcane/" + slug
}

// NormalizeSetupTarget validates a kv-path target and fills its defaults.
func NormalizeSetupTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error) {
	if mode := strings.TrimSpace(target.Mode); mode != types.SecretSourceSetupModeKVPath {
		return types.SecretSourceSetupTarget{}, fmt.Errorf("unknown setup mode %q for Vault/OpenBao; use %s", target.Mode, types.SecretSourceSetupModeKVPath)
	}
	path := strings.TrimSpace(target.SecretPath)
	if path == "" || path == "/" {
		path = SuggestPath(arcaneProjectName)
	}
	// NormalizeTarget also refuses system mounts such as sys and auth.
	bindingTarget, err := NormalizeTarget(&types.SecretSourceVaultTarget{Mount: target.Mount, Path: path, KVVersion: target.KVVersion})
	if err != nil {
		return types.SecretSourceSetupTarget{}, err
	}
	return types.SecretSourceSetupTarget{
		Mode:       types.SecretSourceSetupModeKVPath,
		Mount:      bindingTarget.Mount,
		SecretPath: bindingTarget.Path,
		KVVersion:  bindingTarget.KVVersion,
	}, nil
}

// BindingTarget is what deploys read after setup.
func BindingTarget(target types.SecretSourceSetupTarget) *types.SecretSourceVaultTarget {
	return &types.SecretSourceVaultTarget{Mount: target.Mount, Path: target.SecretPath, KVVersion: target.KVVersion}
}

// RemoteValues reads the secret at the target. A missing secret is empty.
// Non-string values are reported as their JSON text.
func (s *Setup) RemoteValues(ctx context.Context, target types.SecretSourceSetupTarget) (map[string]string, error) {
	data, version, err := s.client.Read(ctx, target.Mount, target.KVVersion, target.SecretPath)
	if vault.IsNotFound(err) {
		// A soft-deleted secret answers 404 but keeps its version, which
		// check-and-set must match.
		data, err = map[string]jsontext.Value{}, nil
		version = 0
		if target.KVVersion == 2 {
			version, err = s.client.CurrentVersion(ctx, target.Mount, target.SecretPath)
		}
	}
	if err != nil {
		return nil, err
	}
	s.current, s.currentVersion, s.currentLoaded = data, version, true

	values := make(map[string]string, len(data))
	text, _ := secretapi.StringValues(data)
	for key, raw := range data {
		if value, ok := text[key]; ok {
			values[key] = value
			continue
		}
		values[key] = string(raw)
	}
	return values, nil
}

// Write adds the create keys and replaces the overwrite keys, keeping every
// other key of the secret. On KV version 2 the write fails if the secret
// changed since RemoteValues read it.
func (s *Setup) Write(ctx context.Context, target types.SecretSourceSetupTarget, values map[string]string, create, overwrite []string) error {
	if !s.currentLoaded {
		if _, err := s.RemoteValues(ctx, target); err != nil {
			return err
		}
	}
	// Existing keys are written back byte for byte, so numbers and nested
	// values are kept exactly.
	merged := make(map[string]any, len(s.current)+len(create))
	for key, raw := range s.current {
		merged[key] = raw
	}
	for _, key := range create {
		if _, exists := s.current[key]; exists {
			return fmt.Errorf("%s already exists at %s; choose to replace it instead", key, Describe(BindingTarget(target)))
		}
		merged[key] = values[key]
	}
	for _, key := range overwrite {
		merged[key] = values[key]
	}
	cas := -1
	if target.KVVersion == 2 {
		cas = s.currentVersion
	}
	if err := s.client.Write(ctx, target.Mount, target.KVVersion, target.SecretPath, merged, cas); err != nil {
		if apiErr, ok := errors.AsType[*secretapi.APIError](err); ok && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Message, "check-and-set") {
			return fmt.Errorf("the secret at %s changed while setting up; run setup again", Describe(BindingTarget(target)))
		}
		return err
	}
	s.currentLoaded = false
	return nil
}
