// Package protonpass is the Proton Pass secret provider. Proton Pass
// encrypts everything client-side, so Arcane does not talk to Proton
// directly: a pass-kit sidecar runs Proton's pass-cli with a personal access
// token and serves vaults, items, and values as JSON. Two mappings are
// supported, as with 1Password:
//   - vault: each item is one variable; the title is the key and its login
//     password (or note, or first hidden field) is the value.
//   - item: each custom field of one item is one variable.
package protonpass

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/pkg/httpsecrets"
	"getarcane.dev/arcane/v3/pkg/secretapi"
	"getarcane.dev/types/v3"
)

type (
	// Provider reads from one pass-kit.
	Provider struct {
		client *httpsecrets.Client
	}

	kitVault struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	kitItem struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Type  string `json:"type"`
	}
)

// NormalizeSettings validates settings and returns them in canonical form.
func NormalizeSettings(settings *types.SecretSourceProtonPassSettings) (*types.SecretSourceProtonPassSettings, error) {
	if settings == nil {
		return nil, errors.New("settings for Proton Pass are required")
	}
	base, err := secretapi.ParseBaseURL(settings.KitURL, "the pass-kit URL", "http://pass-kit:8080")
	if err != nil {
		return nil, err
	}
	return &types.SecretSourceProtonPassSettings{KitURL: base.String()}, nil
}

// NormalizeTarget validates a binding target and returns it in canonical form.
func NormalizeTarget(target *types.SecretSourceProtonPassTarget) (*types.SecretSourceProtonPassTarget, error) {
	if target == nil {
		return nil, errors.New("a Proton Pass vault or item is required")
	}
	normalized := &types.SecretSourceProtonPassTarget{
		Scope:   strings.TrimSpace(target.Scope),
		VaultID: strings.TrimSpace(target.VaultID),
		Name:    strings.TrimSpace(target.Name),
	}
	if normalized.VaultID == "" {
		return nil, errors.New("a Proton Pass vault is required")
	}
	if err := checkSegmentInternal(normalized.VaultID); err != nil {
		return nil, err
	}
	switch normalized.Scope {
	case types.SecretSourceOnePasswordScopeVault:
	case types.SecretSourceOnePasswordScopeItem:
		normalized.ItemID = strings.TrimSpace(target.ItemID)
		if normalized.ItemID == "" {
			return nil, errors.New("a Proton Pass item is required")
		}
		if err := checkSegmentInternal(normalized.ItemID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown Proton Pass scope %q; use vault or item", target.Scope)
	}
	return normalized, nil
}

// checkSegmentInternal rejects IDs that would change the kit path.
func checkSegmentInternal(id string) error {
	if strings.ContainsAny(id, "/?#") || id == "." || id == ".." {
		return fmt.Errorf("%q is not a Proton Pass ID", id)
	}
	return nil
}

// Describe names a target in messages, e.g. vault "Arcane".
func Describe(target *types.SecretSourceProtonPassTarget) string {
	if target == nil {
		return "no target"
	}
	name := target.Name
	if name == "" {
		name = target.VaultID
		if target.Scope == types.SecretSourceOnePasswordScopeItem {
			name = target.ItemID
		}
	}
	return fmt.Sprintf("%s %q", target.Scope, name)
}

// New builds a provider. token is the kit's optional bearer token.
func New(httpClient *http.Client, settings types.SecretSourceProtonPassSettings, token string) (*Provider, error) {
	client, err := httpsecrets.NewClient(httpClient, settings.KitURL, token)
	if err != nil {
		return nil, err
	}
	return &Provider{client: client}, nil
}

func (p *Provider) vaultsInternal(ctx context.Context) ([]kitVault, error) {
	object, err := p.client.Get(ctx, "vaults")
	if err != nil {
		return nil, err
	}
	var vaults []kitVault
	if raw, ok := object["vaults"]; !ok || json.Unmarshal(raw, &vaults) != nil {
		return nil, fmt.Errorf("pass-kit: %w", secretapi.ErrUnexpectedShape)
	}
	return vaults, nil
}

// Test lists the vaults the token can read.
func (p *Provider) Test(ctx context.Context) types.SecretSourceTestResult {
	vaults, err := p.vaultsInternal(ctx)
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}
	}
	return types.SecretSourceTestResult{
		OK:           true,
		Message:      fmt.Sprintf("Connected; the token can read %d vaults", len(vaults)),
		CanBrowse:    true,
		VisibleCount: len(vaults),
	}
}

// Browse lists vaults, or the items of a vault.
func (p *Provider) Browse(ctx context.Context, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error) {
	var items []types.SecretSourceBrowseItem
	switch query.Kind {
	case types.SecretSourceBrowseProtonPassVaults:
		vaults, err := p.vaultsInternal(ctx)
		if err != nil {
			return nil, err
		}
		for _, vault := range vaults {
			items = append(items, types.SecretSourceBrowseItem{ID: vault.ID, Name: vault.Name})
		}
	case types.SecretSourceBrowseProtonPassItems:
		vaultID := strings.TrimSpace(query.VaultID)
		if vaultID == "" {
			return nil, errors.New("a Proton Pass vault is required to list items")
		}
		if err := checkSegmentInternal(vaultID); err != nil {
			return nil, err
		}
		object, err := p.client.Get(ctx, "vaults/"+vaultID+"/items")
		if err != nil {
			return nil, err
		}
		var kitItems []kitItem
		if raw, ok := object["items"]; !ok || json.Unmarshal(raw, &kitItems) != nil {
			return nil, fmt.Errorf("pass-kit: %w", secretapi.ErrUnexpectedShape)
		}
		for _, item := range kitItems {
			items = append(items, types.SecretSourceBrowseItem{ID: item.ID, Name: item.Title, Detail: item.Type})
		}
	default:
		return nil, fmt.Errorf("unknown browse kind %q for Proton Pass", query.Kind)
	}
	slices.SortFunc(items, func(a, b types.SecretSourceBrowseItem) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	return items, nil
}

// Fetch reads the target vault or item. The kit does the mapping and
// returns entries it cannot map as null, which become skipped names.
func (p *Provider) Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error) {
	if target.ProtonPass == nil {
		return nil, nil, errors.New("binding has no Proton Pass target")
	}
	t := target.ProtonPass
	path := "secrets/" + t.VaultID
	if t.Scope == types.SecretSourceOnePasswordScopeItem {
		path += "/" + t.ItemID
	}
	object, err := p.client.Get(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	values, skipped := secretapi.StringValues(object)
	slices.Sort(skipped)
	return values, skipped, nil
}
