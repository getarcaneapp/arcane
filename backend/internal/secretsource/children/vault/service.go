// Package vault is the HashiCorp Vault / OpenBao secret provider. A binding
// reads one secret of a KV mount; each key of the secret is one variable.
package vault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/pkg/secretapi"
	"getarcane.dev/arcane/v3/pkg/vault"
	"getarcane.dev/types/v3"
)

type (
	// Provider reads secrets from one Vault or OpenBao server.
	Provider struct {
		client *vault.Client
	}
)

const (
	// DefaultMount is the KV mount a dev-mode server creates.
	DefaultMount = "secret"
)

var (
	// reservedMounts are API paths that are never KV mounts. Reading them with the
	// deploy token could return the token itself (auth/token/lookup-self) or
	// server internals, so bindings and browsing refuse them.
	reservedMounts = map[string]bool{"sys": true, "auth": true, "identity": true, "cubbyhole": true}
)

// CheckMount rejects a mount that is not a plain KV path.
func CheckMount(mount string) error {
	if err := checkPathInternal(mount); err != nil {
		return fmt.Errorf("invalid mount: %w", err)
	}
	first, _, _ := strings.Cut(mount, "/")
	if reservedMounts[strings.ToLower(first)] {
		return fmt.Errorf("%q is a system path, not a KV mount", first)
	}
	return nil
}

// NormalizeSettings validates settings and returns them in canonical form.
func NormalizeSettings(settings *types.SecretSourceVaultSettings) (*types.SecretSourceVaultSettings, error) {
	if settings == nil {
		return nil, errors.New("vault settings are required")
	}
	address, err := vault.ParseAddress(settings.Address)
	if err != nil {
		return nil, err
	}
	normalized := &types.SecretSourceVaultSettings{
		Address:    address.String(),
		Namespace:  strings.Trim(strings.TrimSpace(settings.Namespace), "/"),
		AuthMethod: strings.TrimSpace(settings.AuthMethod),
		SetupToken: settings.SetupToken,
	}
	switch normalized.AuthMethod {
	case "", types.SecretSourceVaultAuthToken:
		normalized.AuthMethod = types.SecretSourceVaultAuthToken
	case types.SecretSourceVaultAuthAppRole:
		normalized.RoleID = strings.TrimSpace(settings.RoleID)
		if normalized.RoleID == "" {
			return nil, errors.New("an AppRole role ID is required")
		}
		normalized.AppRoleMount = strings.Trim(strings.TrimSpace(settings.AppRoleMount), "/")
		if normalized.AppRoleMount == "" {
			normalized.AppRoleMount = "approle"
		}
	default:
		return nil, fmt.Errorf("unknown auth method %q; use token or approle", settings.AuthMethod)
	}
	return normalized, nil
}

// NormalizeTarget validates a binding target and returns it in canonical form.
func NormalizeTarget(target *types.SecretSourceVaultTarget) (*types.SecretSourceVaultTarget, error) {
	if target == nil {
		return nil, errors.New("a KV mount and secret path are required")
	}
	mount := strings.Trim(strings.TrimSpace(target.Mount), "/")
	if mount == "" {
		mount = DefaultMount
	}
	path := strings.Trim(strings.TrimSpace(target.Path), "/")
	if path == "" {
		return nil, errors.New("a secret path is required, e.g. apps/immich")
	}
	if err := CheckMount(mount); err != nil {
		return nil, err
	}
	if err := checkPathInternal(path); err != nil {
		return nil, fmt.Errorf("invalid secret path: %w", err)
	}
	version := target.KVVersion
	if version == 0 {
		version = 2
	}
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("KV version must be 1 or 2, got %d", target.KVVersion)
	}
	return &types.SecretSourceVaultTarget{Mount: mount, Path: path, KVVersion: version}, nil
}

// Describe names a target in messages, e.g. secret/apps/immich.
func Describe(target *types.SecretSourceVaultTarget) string {
	if target == nil {
		return "no target"
	}
	return target.Mount + "/" + target.Path
}

// ClientConfig turns settings and the credential into a client config.
func ClientConfig(settings types.SecretSourceVaultSettings, credential string) vault.Config {
	return vault.Config{
		Address:      settings.Address,
		Namespace:    settings.Namespace,
		AuthMethod:   settings.AuthMethod,
		RoleID:       settings.RoleID,
		AppRoleMount: settings.AppRoleMount,
		Secret:       credential,
	}
}

// New builds a provider.
func New(httpClient *http.Client, settings types.SecretSourceVaultSettings, credential string) (*Provider, error) {
	client, err := vault.NewClient(httpClient, ClientConfig(settings, credential))
	if err != nil {
		return nil, err
	}
	return &Provider{client: client}, nil
}

// Test logs in and looks the token up.
func (p *Provider) Test(ctx context.Context) types.SecretSourceTestResult {
	info, err := p.client.LookupSelf(ctx)
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}
	}
	name := info.DisplayName
	if name == "" {
		name = "the token"
	}
	message := fmt.Sprintf("Authenticated as %s", name)
	if len(info.Policies) > 0 {
		message += " with policies " + strings.Join(info.Policies, ", ")
	}
	mounts, mountsErr := p.client.KVMounts(ctx)
	if mountsErr != nil {
		return types.SecretSourceTestResult{OK: true, Message: message + "; mounts cannot be listed, so type the mount when binding"}
	}
	return types.SecretSourceTestResult{OK: true, Message: message, CanBrowse: true, VisibleCount: len(mounts)}
}

// Browse lists KV mounts, or the keys under a path of a mount.
func (p *Provider) Browse(ctx context.Context, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error) {
	switch query.Kind {
	case types.SecretSourceBrowseVaultMounts:
		mounts, err := p.client.KVMounts(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(mounts))
		for _, mount := range mounts {
			items = append(items, types.SecretSourceBrowseItem{ID: mount.Path, Name: mount.Path, Detail: "KV v" + mount.Version})
		}
		return sortedInternal(items), nil
	case types.SecretSourceBrowseVaultPaths:
		mount := strings.Trim(query.Mount, "/")
		if mount == "" {
			mount = DefaultMount
		}
		if err := CheckMount(mount); err != nil {
			return nil, err
		}
		if prefix := strings.Trim(query.Path, "/"); prefix != "" {
			if err := checkPathInternal(prefix); err != nil {
				return nil, fmt.Errorf("invalid path: %w", err)
			}
		}
		version := 2
		if query.KVVersion == 1 {
			version = 1
		}
		prefix := strings.Trim(query.Path, "/")
		keys, err := p.client.List(ctx, mount, version, prefix)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(keys))
		for _, key := range keys {
			full := key
			if prefix != "" {
				full = prefix + "/" + key
			}
			detail := "secret"
			if strings.HasSuffix(key, "/") {
				detail = "folder"
			}
			items = append(items, types.SecretSourceBrowseItem{ID: strings.TrimSuffix(full, "/"), Name: key, Detail: detail})
		}
		return sortedInternal(items), nil
	default:
		return nil, fmt.Errorf("unknown browse kind %q for Vault/OpenBao", query.Kind)
	}
}

// Fetch reads the target secret. Values that are objects, arrays, or null are
// skipped; numbers and booleans become their JSON text.
func (p *Provider) Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error) {
	if target.Vault == nil {
		return nil, nil, errors.New("binding has no Vault/OpenBao target")
	}
	data, _, err := p.client.Read(ctx, target.Vault.Mount, target.Vault.KVVersion, target.Vault.Path)
	if vault.IsNotFound(err) {
		return nil, nil, fmt.Errorf("no secret at %s", Describe(target.Vault))
	}
	if err != nil {
		return nil, nil, err
	}
	values, skipped := secretapi.StringValues(data)
	slices.Sort(skipped)
	return values, skipped, nil
}

// checkPathInternal rejects segments that would change which endpoint is
// called, such as "..".
func checkPathInternal(path string) error {
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "":
			return errors.New("empty path segment")
		case ".", "..":
			return fmt.Errorf("%q is not allowed in a path", segment)
		}
	}
	return nil
}

func sortedInternal(items []types.SecretSourceBrowseItem) []types.SecretSourceBrowseItem {
	slices.SortFunc(items, func(a, b types.SecretSourceBrowseItem) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return items
}
