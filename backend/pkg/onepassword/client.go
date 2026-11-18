// Package onepassword is a small read-only client for the 1Password Connect
// server API. Connect runs next to Arcane and holds the vault keys; Arcane
// only sends the Connect access token.
package onepassword

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"getarcane.dev/arcane/v3/pkg/secretapi"
)

type (
	// Vault is a vault the token can read.
	Vault struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	// Field is one field of an item.
	Field struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Purpose string `json:"purpose"`
		Label   string `json:"label"`
		Value   string `json:"value"`
	}

	// Item is an item; Fields is only filled by GetItem.
	Item struct {
		ID       string  `json:"id"`
		Title    string  `json:"title"`
		Category string  `json:"category"`
		Fields   []Field `json:"fields"`
	}

	// Client talks to one Connect server. It is safe for concurrent use.
	Client struct {
		api *secretapi.Requester
	}
)

const (
	// Field purposes and types as defined by 1Password.
	PurposePassword = "PASSWORD"
	PurposeNotes    = "NOTES"
	PurposeUsername = "USERNAME"

	TypeOTP = "OTP"
)

// ParseServerURL validates a Connect server address such as
// http://op-connect-api:8080.
func ParseServerURL(raw string) (*url.URL, error) {
	return secretapi.ParseBaseURL(raw, "the 1Password Connect URL", "http://op-connect-api:8080")
}

// NewClient returns a client for one Connect server. httpClient may be nil.
func NewClient(httpClient *http.Client, serverURL, token string) (*Client, error) {
	base, err := ParseServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("a 1Password Connect token is required")
	}
	api := secretapi.NewRequester(httpClient, base, "1Password Connect")
	api.Authorize = func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	return &Client{api: api}, nil
}

// Vaults lists the vaults the token can read.
func (c *Client) Vaults(ctx context.Context) ([]Vault, error) {
	var vaults []Vault
	if err := c.api.Do(ctx, http.MethodGet, "/v1/vaults", nil, nil, &vaults); err != nil {
		return nil, err
	}
	return vaults, nil
}

// Items lists the items of a vault, without their fields.
func (c *Client) Items(ctx context.Context, vaultID string) ([]Item, error) {
	var items []Item
	path := "/v1/vaults/" + url.PathEscape(vaultID) + "/items"
	if err := c.api.Do(ctx, http.MethodGet, path, nil, nil, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// GetItem returns one item with its fields.
func (c *Client) GetItem(ctx context.Context, vaultID, itemID string) (*Item, error) {
	var item Item
	path := "/v1/vaults/" + url.PathEscape(vaultID) + "/items/" + url.PathEscape(itemID)
	if err := c.api.Do(ctx, http.MethodGet, path, nil, nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// PrimaryValue is the value an item stands for when the whole item is one
// variable: its password, else its credential or other concealed field, else
// its notes. ok is false when the item has none of these.
func PrimaryValue(item *Item) (string, bool) {
	for _, field := range item.Fields {
		if field.Purpose == PurposePassword && field.Value != "" {
			return field.Value, true
		}
	}
	for _, field := range item.Fields {
		if field.Type == "CONCEALED" && field.Purpose == "" && field.Value != "" {
			return field.Value, true
		}
	}
	for _, field := range item.Fields {
		if field.Purpose == PurposeNotes && field.Value != "" {
			return field.Value, true
		}
	}
	return "", false
}
