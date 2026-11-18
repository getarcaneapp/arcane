// Package onepassword is the 1Password secret provider, through a 1Password
// Connect server. Two mappings are supported:
//   - vault: each item is one variable; the item title is the key and its
//     password (or credential, or notes) is the value.
//   - item: each field of one item is one variable; the label is the key.
package onepassword

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/pkg/onepassword"
	"getarcane.dev/types/v3"
)

type (
	// Provider reads secrets from one Connect server.
	Provider struct {
		client *onepassword.Client
	}
)

const (
	// maxVaultItems caps how many items a vault binding reads, one request each.
	maxVaultItems = 500
)

// NormalizeSettings validates settings and returns them in canonical form.
func NormalizeSettings(settings *types.SecretSourceOnePasswordSettings) (*types.SecretSourceOnePasswordSettings, error) {
	if settings == nil {
		return nil, errors.New("1Password settings are required")
	}
	serverURL, err := onepassword.ParseServerURL(settings.ServerURL)
	if err != nil {
		return nil, err
	}
	return &types.SecretSourceOnePasswordSettings{ServerURL: serverURL.String()}, nil
}

// NormalizeTarget validates a binding target and returns it in canonical form.
func NormalizeTarget(target *types.SecretSourceOnePasswordTarget) (*types.SecretSourceOnePasswordTarget, error) {
	if target == nil {
		return nil, errors.New("a 1Password vault or item is required")
	}
	normalized := &types.SecretSourceOnePasswordTarget{
		Scope:   strings.TrimSpace(target.Scope),
		VaultID: strings.TrimSpace(target.VaultID),
		Name:    strings.TrimSpace(target.Name),
	}
	if normalized.VaultID == "" {
		return nil, errors.New("a 1Password vault is required")
	}
	switch normalized.Scope {
	case types.SecretSourceOnePasswordScopeVault:
	case types.SecretSourceOnePasswordScopeItem:
		normalized.ItemID = strings.TrimSpace(target.ItemID)
		if normalized.ItemID == "" {
			return nil, errors.New("a 1Password item is required")
		}
	default:
		return nil, fmt.Errorf("unknown 1Password scope %q; use vault or item", target.Scope)
	}
	return normalized, nil
}

// Describe names a target in messages, e.g. vault "Homelab".
func Describe(target *types.SecretSourceOnePasswordTarget) string {
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

// New builds a provider.
func New(httpClient *http.Client, settings types.SecretSourceOnePasswordSettings, token string) (*Provider, error) {
	client, err := onepassword.NewClient(httpClient, settings.ServerURL, token)
	if err != nil {
		return nil, err
	}
	return &Provider{client: client}, nil
}

// Test lists the vaults the token can read.
func (p *Provider) Test(ctx context.Context) types.SecretSourceTestResult {
	vaults, err := p.client.Vaults(ctx)
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
	switch query.Kind {
	case types.SecretSourceBrowseOnePasswordVaults:
		vaults, err := p.client.Vaults(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(vaults))
		for _, vault := range vaults {
			items = append(items, types.SecretSourceBrowseItem{ID: vault.ID, Name: vault.Name, Detail: vault.Description})
		}
		return sortedInternal(items), nil
	case types.SecretSourceBrowseOnePasswordItems:
		if strings.TrimSpace(query.VaultID) == "" {
			return nil, errors.New("a 1Password vault is required to list items")
		}
		vaultItems, err := p.client.Items(ctx, query.VaultID)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(vaultItems))
		for _, item := range vaultItems {
			items = append(items, types.SecretSourceBrowseItem{ID: item.ID, Name: item.Title, Detail: strings.ToLower(item.Category)})
		}
		return sortedInternal(items), nil
	default:
		return nil, fmt.Errorf("unknown browse kind %q for 1Password", query.Kind)
	}
}

// Fetch reads the target vault or item.
func (p *Provider) Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error) {
	if target.OnePassword == nil {
		return nil, nil, errors.New("binding has no 1Password target")
	}
	t := target.OnePassword
	switch t.Scope {
	case types.SecretSourceOnePasswordScopeVault:
		summaries, err := p.client.Items(ctx, t.VaultID)
		if err != nil {
			return nil, nil, err
		}
		if len(summaries) > maxVaultItems {
			return nil, nil, fmt.Errorf("vault has %d items; bind a vault with at most %d, or bind single items", len(summaries), maxVaultItems)
		}
		items := make([]*onepassword.Item, 0, len(summaries))
		for _, summary := range summaries {
			item, itemErr := p.client.GetItem(ctx, t.VaultID, summary.ID)
			if itemErr != nil {
				return nil, nil, fmt.Errorf("read item %q: %w", summary.Title, itemErr)
			}
			items = append(items, item)
		}
		return ItemsToValues(items)
	case types.SecretSourceOnePasswordScopeItem:
		item, err := p.client.GetItem(ctx, t.VaultID, t.ItemID)
		if err != nil {
			return nil, nil, err
		}
		return FieldsToValues(item.Fields)
	default:
		return nil, nil, fmt.Errorf("unknown 1Password scope %q", t.Scope)
	}
}

// ItemsToValues maps items to variables: the title is the key, the primary
// value the value. Items without one are skipped; duplicate titles are an
// error because which one wins would be arbitrary.
func ItemsToValues(items []*onepassword.Item) (map[string]string, []string, error) {
	values := make(map[string]string, len(items))
	var skipped, duplicates []string
	for _, item := range items {
		name := strings.TrimSpace(item.Title)
		value, ok := onepassword.PrimaryValue(item)
		if name == "" || !ok {
			skipped = append(skipped, displayNameInternal(item.Title, "untitled item"))
			continue
		}
		if _, exists := values[name]; exists {
			duplicates = append(duplicates, name)
			continue
		}
		values[name] = value
	}
	if len(duplicates) > 0 {
		slices.Sort(duplicates)
		return nil, nil, fmt.Errorf("more than one item is titled %s; rename them so each variable is defined once", strings.Join(duplicates, ", "))
	}
	slices.Sort(skipped)
	return values, skipped, nil
}

// FieldsToValues maps an item's fields to variables. Fields without a label
// and one-time-password fields (whose stored value is the OTP secret, not a
// code) are skipped.
func FieldsToValues(fields []onepassword.Field) (map[string]string, []string, error) {
	values := make(map[string]string, len(fields))
	var skipped, duplicates []string
	for _, field := range fields {
		name := strings.TrimSpace(field.Label)
		if name == "" || field.Type == onepassword.TypeOTP {
			skipped = append(skipped, displayNameInternal(field.Label, "unlabeled field"))
			continue
		}
		if _, exists := values[name]; exists {
			duplicates = append(duplicates, name)
			continue
		}
		values[name] = field.Value
	}
	if len(duplicates) > 0 {
		slices.Sort(duplicates)
		return nil, nil, fmt.Errorf("more than one field is labeled %s; rename them so each variable is defined once", strings.Join(duplicates, ", "))
	}
	slices.Sort(skipped)
	return values, skipped, nil
}

func displayNameInternal(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return "(" + fallback + ")"
	}
	return name
}

func sortedInternal(items []types.SecretSourceBrowseItem) []types.SecretSourceBrowseItem {
	slices.SortFunc(items, func(a, b types.SecretSourceBrowseItem) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return items
}
