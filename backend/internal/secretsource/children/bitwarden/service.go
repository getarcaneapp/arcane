// Package bitwarden is the Bitwarden/Vaultwarden secret provider. It reads
// through a Bitwarden CLI `bw serve` endpoint, which holds the unlocked vault
// session, so Arcane never sees the master password or vault keys.
//
// Two mappings are supported:
//   - folder or collection: each item is one variable; the item name is the
//     key and the login password (or a secure note's text) is the value.
//   - item: each custom field of one item is one variable.
package bitwarden

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/pkg/bitwarden"
	"getarcane.dev/types/v3"
)

// Provider reads secrets from one `bw serve` endpoint.
type Provider struct {
	client *bitwarden.Client
}

// NormalizeSettings validates settings and returns them in canonical form.
func NormalizeSettings(settings *types.SecretSourceBitwardenSettings) (*types.SecretSourceBitwardenSettings, error) {
	if settings == nil {
		return nil, errors.New("bitwarden settings are required")
	}
	serveURL, err := bitwarden.ParseServeURL(settings.ServeURL)
	if err != nil {
		return nil, err
	}
	return &types.SecretSourceBitwardenSettings{ServeURL: serveURL.String()}, nil
}

// NormalizeTarget validates a binding target and returns it in canonical form.
func NormalizeTarget(target *types.SecretSourceBitwardenTarget) (*types.SecretSourceBitwardenTarget, error) {
	if target == nil {
		return nil, errors.New("a Bitwarden folder, collection, or item is required")
	}
	scope := strings.TrimSpace(target.Scope)
	switch scope {
	case types.SecretSourceBitwardenScopeFolder, types.SecretSourceBitwardenScopeCollection, types.SecretSourceBitwardenScopeItem:
	default:
		return nil, fmt.Errorf("unknown Bitwarden scope %q; use folder, collection, or item", target.Scope)
	}
	id := strings.TrimSpace(target.ID)
	if id == "" {
		return nil, fmt.Errorf("a Bitwarden %s is required", scope)
	}
	return &types.SecretSourceBitwardenTarget{Scope: scope, ID: id, Name: strings.TrimSpace(target.Name)}, nil
}

// Describe names a target in messages, e.g. folder "billing-api".
func Describe(target *types.SecretSourceBitwardenTarget) string {
	if target == nil {
		return "no target"
	}
	name := target.Name
	if name == "" {
		name = target.ID
	}
	return fmt.Sprintf("%s %q", target.Scope, name)
}

// New builds a provider for a `bw serve` endpoint.
func New(httpClient *http.Client, settings types.SecretSourceBitwardenSettings) (*Provider, error) {
	client, err := bitwarden.NewClient(httpClient, settings.ServeURL)
	if err != nil {
		return nil, err
	}
	return &Provider{client: client}, nil
}

// Test checks that `bw serve` answers, its vault is unlocked, and it can
// reach the server; it reports how many items the account can see.
func (p *Provider) Test(ctx context.Context) types.SecretSourceTestResult {
	status, err := p.client.Status(ctx)
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}
	}
	if status.Status != "unlocked" {
		return types.SecretSourceTestResult{
			OK:      false,
			Message: fmt.Sprintf("bw serve is reachable, but the vault for %s is %s; unlock it in the bw serve container", accountInternal(status), status.Status),
		}
	}
	if syncErr := p.client.Sync(ctx); syncErr != nil {
		return types.SecretSourceTestResult{OK: false, Message: "vault is unlocked, but syncing with the server failed: " + syncErr.Error()}
	}
	items, err := p.client.ListItems(ctx, bitwarden.ItemFilter{})
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}
	}
	return types.SecretSourceTestResult{
		OK:           true,
		Message:      fmt.Sprintf("Vault unlocked for %s on %s", accountInternal(status), status.ServerURL),
		CanBrowse:    true,
		VisibleCount: len(items),
	}
}

// Browse lists folders, collections, or items for the binding pickers.
func (p *Provider) Browse(ctx context.Context, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error) {
	switch query.Kind {
	case types.SecretSourceBrowseBitwardenFolders:
		folders, err := p.client.ListFolders(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(folders))
		for _, folder := range folders {
			// The pseudo-folder "No Folder" has no ID and cannot be bound.
			if folder.ID == nil {
				continue
			}
			items = append(items, types.SecretSourceBrowseItem{ID: *folder.ID, Name: folder.Name})
		}
		return sortedInternal(items), nil
	case types.SecretSourceBrowseBitwardenCollections:
		collections, err := p.client.ListCollections(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(collections))
		for _, collection := range collections {
			items = append(items, types.SecretSourceBrowseItem{ID: collection.ID, Name: collection.Name})
		}
		return sortedInternal(items), nil
	case types.SecretSourceBrowseBitwardenItems:
		vaultItems, err := p.client.ListItems(ctx, bitwarden.ItemFilter{})
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(vaultItems))
		for _, item := range vaultItems {
			items = append(items, types.SecretSourceBrowseItem{
				ID:     item.ID,
				Name:   item.Name,
				Detail: fmt.Sprintf("%d custom fields", len(item.Fields)),
			})
		}
		return sortedInternal(items), nil
	default:
		return nil, fmt.Errorf("unknown browse kind %q for Bitwarden", query.Kind)
	}
}

// Fetch syncs the vault and reads the target. Items or fields without a
// usable value are returned as skipped.
func (p *Provider) Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error) {
	if target.Bitwarden == nil {
		return nil, nil, errors.New("binding has no Bitwarden target")
	}
	// bw serve answers from its local cache; sync so a change made in the
	// vault is seen by this deploy or drift check.
	if err := p.client.Sync(ctx); err != nil {
		return nil, nil, fmt.Errorf("sync vault: %w", err)
	}

	switch target.Bitwarden.Scope {
	case types.SecretSourceBitwardenScopeFolder, types.SecretSourceBitwardenScopeCollection:
		filter := bitwarden.ItemFilter{FolderID: target.Bitwarden.ID}
		if target.Bitwarden.Scope == types.SecretSourceBitwardenScopeCollection {
			filter = bitwarden.ItemFilter{CollectionID: target.Bitwarden.ID}
		}
		items, err := p.client.ListItems(ctx, filter)
		if err != nil {
			return nil, nil, err
		}
		return ItemsToValues(items)
	case types.SecretSourceBitwardenScopeItem:
		item, err := p.client.GetItem(ctx, target.Bitwarden.ID)
		if err != nil {
			return nil, nil, err
		}
		if item.DeletedDate != nil {
			return nil, nil, fmt.Errorf("item %q is in the trash; restore it or bind another item", item.Name)
		}
		return FieldsToValues(item.Fields)
	default:
		return nil, nil, fmt.Errorf("unknown Bitwarden scope %q", target.Bitwarden.Scope)
	}
}

// ItemsToValues maps items to variables: the item name is the key, and the
// login password, or a secure note's text, is the value. Items with neither
// are skipped. Two items with the same name are an error, because which one
// wins would be arbitrary.
func ItemsToValues(items []bitwarden.Item) (map[string]string, []string, error) {
	values := make(map[string]string, len(items))
	var skipped, duplicates []string
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		value, ok := itemValueInternal(item)
		if name == "" || !ok {
			skipped = append(skipped, displayNameInternal(item.Name, "unnamed item"))
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
		return nil, nil, fmt.Errorf("more than one item is named %s; rename them so each variable is defined once", strings.Join(duplicates, ", "))
	}
	return values, skipped, nil
}

// FieldsToValues maps an item's custom fields to variables. Linked fields
// have no value of their own and are skipped.
func FieldsToValues(fields []bitwarden.Field) (map[string]string, []string, error) {
	values := make(map[string]string, len(fields))
	var skipped, duplicates []string
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || field.Type == bitwarden.FieldTypeLinked {
			skipped = append(skipped, displayNameInternal(field.Name, "unnamed field"))
			continue
		}
		if _, exists := values[name]; exists {
			duplicates = append(duplicates, name)
			continue
		}
		value := ""
		if field.Value != nil {
			value = *field.Value
		}
		values[name] = value
	}
	if len(duplicates) > 0 {
		slices.Sort(duplicates)
		return nil, nil, fmt.Errorf("more than one custom field is named %s; rename them so each variable is defined once", strings.Join(duplicates, ", "))
	}
	return values, skipped, nil
}

func itemValueInternal(item bitwarden.Item) (string, bool) {
	switch item.Type {
	case bitwarden.ItemTypeLogin:
		if item.Login != nil && item.Login.Password != nil && *item.Login.Password != "" {
			return *item.Login.Password, true
		}
	case bitwarden.ItemTypeSecureNote:
		if item.Notes != nil && *item.Notes != "" {
			return *item.Notes, true
		}
	}
	return "", false
}

func displayNameInternal(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return "(" + fallback + ")"
	}
	return name
}

func accountInternal(status bitwarden.Status) string {
	if status.UserEmail != "" {
		return status.UserEmail
	}
	return "the signed-in account"
}

func sortedInternal(items []types.SecretSourceBrowseItem) []types.SecretSourceBrowseItem {
	slices.SortFunc(items, func(a, b types.SecretSourceBrowseItem) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return items
}
