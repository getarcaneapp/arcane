package transfer

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
)

// HoldStore is the durable key/value persistence holds live in (the KV table).
type HoldStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	ListByPrefix(ctx context.Context, prefix string) ([]kv.KVEntry, error)
}

// ErrHeldByOther means another transfer already reserves the resource.
var ErrHeldByOther = errors.New("resource is held by another transfer")

// Holds reserves resources on one node so lifecycle operations and background
// jobs leave them alone while a transfer owns them.
type Holds struct {
	store HoldStore
}

// NewHolds builds a hold registry over the store.
func NewHolds(store HoldStore) *Holds {
	return &Holds{store: store}
}

// HoldKey renders the KV key for one resource.
func HoldKey(kind transfertypes.Kind, resource string) string {
	return transfertypes.HoldKeyPrefix + string(kind) + ":" + strings.TrimSpace(resource)
}

// Get returns the hold on a resource, or nil.
func (h *Holds) Get(ctx context.Context, kind transfertypes.Kind, resource string) (*transfertypes.Hold, error) {
	if h == nil || h.store == nil {
		return nil, nil
	}
	raw, ok, err := h.store.Get(ctx, HoldKey(kind, resource))
	if err != nil || !ok {
		return nil, err
	}
	var hold transfertypes.Hold
	if err := json.Unmarshal([]byte(raw), &hold); err != nil {
		return nil, fmt.Errorf("decode hold %s: %w", resource, err)
	}
	return &hold, nil
}

// List returns every hold on this node.
func (h *Holds) List(ctx context.Context) ([]transfertypes.Hold, error) {
	if h == nil || h.store == nil {
		return nil, nil
	}
	entries, err := h.store.ListByPrefix(ctx, transfertypes.HoldKeyPrefix)
	if err != nil {
		return nil, err
	}
	holds := make([]transfertypes.Hold, 0, len(entries))
	for _, entry := range entries {
		var hold transfertypes.Hold
		if err := json.Unmarshal([]byte(entry.Value), &hold); err != nil {
			return nil, fmt.Errorf("decode hold %s: %w", entry.Key, err)
		}
		holds = append(holds, hold)
	}
	return holds, nil
}

// Acquire reserves the resource for transferID. Re-acquiring an own hold is
// idempotent; a hold by another transfer is ErrHeldByOther.
func (h *Holds) Acquire(ctx context.Context, transferID string, kind transfertypes.Kind, resource string) error {
	existing, err := h.Get(ctx, kind, resource)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.TransferID == transferID {
			return nil
		}
		return fmt.Errorf("%w: %s %s is held by transfer %s", ErrHeldByOther, kind, resource, existing.TransferID)
	}
	hold := transfertypes.Hold{TransferID: transferID, Kind: kind, Resource: strings.TrimSpace(resource), CreatedAt: time.Now().UTC()}
	encoded, err := json.Marshal(hold)
	if err != nil {
		return err
	}
	return h.store.Set(ctx, HoldKey(kind, resource), string(encoded))
}

// Release removes the hold when transferID owns it; foreign holds are left.
func (h *Holds) Release(ctx context.Context, transferID string, kind transfertypes.Kind, resource string) error {
	existing, err := h.Get(ctx, kind, resource)
	if err != nil || existing == nil {
		return err
	}
	if existing.TransferID != transferID {
		return fmt.Errorf("%w: %s %s is held by transfer %s", ErrHeldByOther, kind, resource, existing.TransferID)
	}
	return h.store.Delete(ctx, HoldKey(kind, resource))
}

// Check returns ErrHeldByOther-wrapped detail when a transfer other than
// allowedTransferID holds the resource; an empty allowedTransferID rejects any hold.
func (h *Holds) Check(ctx context.Context, kind transfertypes.Kind, resource, allowedTransferID string) error {
	existing, err := h.Get(ctx, kind, resource)
	if err != nil || existing == nil {
		return err
	}
	if allowedTransferID != "" && existing.TransferID == allowedTransferID {
		return nil
	}
	return fmt.Errorf("%w: %s %s is reserved by transfer %s", ErrHeldByOther, kind, resource, existing.TransferID)
}

type holdOwnerKeyInternal struct{}

// WithHoldOwner marks ctx as acting on behalf of transferID so mutation guards
// let the owning transfer through its own holds. A blank ID marks nothing.
func WithHoldOwner(ctx context.Context, transferID string) context.Context {
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		return ctx
	}
	return context.WithValue(ctx, holdOwnerKeyInternal{}, transferID)
}

// HoldOwnerFromContext returns the transfer the context acts for, if any.
func HoldOwnerFromContext(ctx context.Context) string {
	owner, _ := ctx.Value(holdOwnerKeyInternal{}).(string)
	return owner
}

// Guard is the mutation-path check: it passes when nothing holds the resource
// or the context acts for the holding transfer.
func (h *Holds) Guard(ctx context.Context, kind transfertypes.Kind, resource string) error {
	return h.Check(ctx, kind, resource, HoldOwnerFromContext(ctx))
}
