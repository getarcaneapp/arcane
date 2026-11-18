package bitwarden

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The calls in this file write to the vault through `bw serve`. Arcane only
// makes them from the project setup wizard.

// CreateFolder creates a folder in the signed-in account's vault.
func (c *Client) CreateFolder(ctx context.Context, name string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Folder{}, errors.New("folder name is required")
	}
	var folder Folder
	if err := c.sendInternal(ctx, http.MethodPost, "/object/folder", nil, map[string]any{"name": name}, &folder); err != nil {
		return Folder{}, err
	}
	if folder.ID == nil || *folder.ID == "" {
		return Folder{}, errors.New("bw serve created the folder but returned no ID")
	}
	return folder, nil
}

// CreateSecureNote creates a secure note named name whose text is value, in
// folderID of the signed-in account's vault. That is the shape folder bindings
// read: the item name is the key and the note text is the value.
func (c *Client) CreateSecureNote(ctx context.Context, folderID, name, value string) (Item, error) {
	body := map[string]any{
		"type":       ItemTypeSecureNote,
		"name":       name,
		"notes":      value,
		"folderId":   folderID,
		"secureNote": map[string]any{"type": 0},
		"favorite":   false,
		"fields":     []any{},
		"reprompt":   0,
	}
	var item Item
	if err := c.sendInternal(ctx, http.MethodPost, "/object/item", nil, body, &item); err != nil {
		return Item{}, err
	}
	return item, nil
}

// SetItemValue replaces the value a folder binding reads from an item: the
// password of a login or the text of a secure note. bw serve replaces the
// whole item on edit, so the item is read and sent back with only that value
// changed.
func (c *Client) SetItemValue(ctx context.Context, id, value string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("item ID is required")
	}
	path := "/object/item/" + url.PathEscape(id)
	var item map[string]any
	if err := c.sendInternal(ctx, http.MethodGet, path, nil, nil, &item); err != nil {
		return err
	}
	itemType, _ := item["type"].(float64)
	switch int(itemType) {
	case ItemTypeLogin:
		login, _ := item["login"].(map[string]any)
		if login == nil {
			login = map[string]any{}
		}
		login["password"] = value
		item["login"] = login
	case ItemTypeSecureNote:
		item["notes"] = value
	default:
		return fmt.Errorf("item %s is neither a login nor a secure note", id)
	}
	return c.sendInternal(ctx, http.MethodPut, path, nil, item, nil)
}
