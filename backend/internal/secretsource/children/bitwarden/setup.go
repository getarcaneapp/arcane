package bitwarden

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"getarcane.dev/arcane/v3/pkg/bitwarden"
	"getarcane.dev/types/v3"
)

type (
	// Setup writes project secrets through bw serve, as the account bw serve is
	// signed in to. It only writes to that account's own folders, so shared
	// collections can stay read-only for it.
	Setup struct {
		client *bitwarden.Client
	}

	// RemoteItem is one item of a setup folder, keyed by its name.
	RemoteItem struct {
		ID    string
		Value string
	}
)

func NewSetup(httpClient *http.Client, settings types.SecretSourceBitwardenSettings) (*Setup, error) {
	client, err := bitwarden.NewClient(httpClient, settings.ServeURL)
	if err != nil {
		return nil, err
	}
	return &Setup{client: client}, nil
}

// NormalizeSetupTarget validates a setup target and fills its defaults.
func NormalizeSetupTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error) {
	normalized := types.SecretSourceSetupTarget{Mode: strings.TrimSpace(target.Mode)}
	switch normalized.Mode {
	case types.SecretSourceSetupModeNewFolder:
		normalized.FolderName = strings.TrimSpace(target.FolderName)
		if normalized.FolderName == "" {
			normalized.FolderName = strings.TrimSpace(arcaneProjectName)
		}
		if normalized.FolderName == "" {
			return normalized, errors.New("a folder name is required")
		}
	case types.SecretSourceSetupModeExistingFolder:
		normalized.FolderID = strings.TrimSpace(target.FolderID)
		// The name is only kept for display in the binding.
		normalized.FolderName = strings.TrimSpace(target.FolderName)
		if normalized.FolderID == "" {
			return normalized, errors.New("choose a folder")
		}
	default:
		return normalized, fmt.Errorf("unknown setup mode %q for a Bitwarden source; use %s or %s",
			target.Mode, types.SecretSourceSetupModeNewFolder, types.SecretSourceSetupModeExistingFolder)
	}
	return normalized, nil
}

// FolderNameTaken reports whether a folder is already named name.
func (s *Setup) FolderNameTaken(ctx context.Context, name string) (bool, error) {
	if err := s.client.Sync(ctx); err != nil {
		return false, err
	}
	folders, err := s.client.ListFolders(ctx)
	if err != nil {
		return false, err
	}
	for _, folder := range folders {
		if folder.ID != nil && strings.EqualFold(folder.Name, strings.TrimSpace(name)) {
			return true, nil
		}
	}
	return false, nil
}

// CreateFolder creates a folder and returns its ID.
func (s *Setup) CreateFolder(ctx context.Context, name string) (string, error) {
	folder, err := s.client.CreateFolder(ctx, name)
	if err != nil {
		return "", err
	}
	return *folder.ID, nil
}

// RemoteItems returns the items of a folder by name, including items a
// binding would skip (an empty login password), so setup never creates a
// second item with the same name. Two items with the same name are an error:
// setup could not tell which one a deploy reads.
func (s *Setup) RemoteItems(ctx context.Context, folderID string) (map[string]RemoteItem, error) {
	if folderID == "" {
		return map[string]RemoteItem{}, nil
	}
	if err := s.client.Sync(ctx); err != nil {
		return nil, err
	}
	items, err := s.client.ListItems(ctx, bitwarden.ItemFilter{FolderID: folderID})
	if err != nil {
		return nil, err
	}
	remote := make(map[string]RemoteItem, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		if _, dup := remote[name]; dup {
			return nil, fmt.Errorf("the folder has more than one item named %q; rename or remove one first", name)
		}
		value := ""
		switch {
		case item.Type == bitwarden.ItemTypeLogin && item.Login != nil && item.Login.Password != nil:
			value = *item.Login.Password
		case item.Type == bitwarden.ItemTypeSecureNote && item.Notes != nil:
			value = *item.Notes
		}
		remote[name] = RemoteItem{ID: item.ID, Value: value}
	}
	return remote, nil
}

// CreateItems creates one secure note per key in folderID.
func (s *Setup) CreateItems(ctx context.Context, folderID string, values map[string]string, keys []string) error {
	for _, key := range keys {
		if _, err := s.client.CreateSecureNote(ctx, folderID, key, values[key]); err != nil {
			return fmt.Errorf("create %s: %w", key, err)
		}
	}
	return nil
}

// UpdateItems replaces the value of existing items, by item ID.
func (s *Setup) UpdateItems(ctx context.Context, values map[string]string, itemIDs map[string]string, keys []string) error {
	for _, key := range keys {
		if err := s.client.SetItemValue(ctx, itemIDs[key], values[key]); err != nil {
			return fmt.Errorf("update %s: %w", key, err)
		}
	}
	return nil
}
