// Package audit records security audit events into the event log.
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
)

type AuditService struct {
	db             *database.DB
	denialsEnabled func(context.Context) bool
	denials        chan pendingDenial
	quit           chan struct{}
	done           chan struct{}
	dropped        atomic.Uint64
}

func NewAuditService(db *database.DB, denialsEnabled func(context.Context) bool) *AuditService {
	return &AuditService{
		db:             db,
		denialsEnabled: denialsEnabled,
		denials:        make(chan pendingDenial, denialQueueCapacity),
		quit:           make(chan struct{}),
		done:           make(chan struct{}),
	}
}

// RecordInTx writes an audit event inside the caller's transaction.
func RecordInTx(ctx context.Context, tx *gorm.DB, entry types.AuditEntry) error {
	if err := tx.Create(new(newEvent(ctx, entry, types.AuthzDenial{}))).Error; err != nil {
		return fmt.Errorf("failed to record audit entry %s: %w", entry.Action, err)
	}
	return nil
}

func (s *AuditService) Record(ctx context.Context, entry types.AuditEntry) error {
	return RecordInTx(ctx, s.db.WithContext(ctx), entry)
}

func (s *AuditService) RecordDenial(ctx context.Context, denial types.AuthzDenial) {
	if !s.denialsEnabled(ctx) {
		return
	}
	row := newEvent(ctx, types.AuditEntry{
		Action:        types.AuditActionAuthzDenied,
		Outcome:       types.AuditOutcomeDenied,
		EnvironmentID: kit.Ternary(denial.EnvironmentID == "", nil, new(denial.EnvironmentID)),
		Permission:    denial.Permission,
	}, denial)
	select {
	case s.denials <- pendingDenial{row: row, key: denialKey(ctx, denial)}:
	default:
		if dropped := s.dropped.Add(1); dropped == 1 || dropped%1000 == 0 {
			slog.WarnContext(ctx, "Audit denial queue is full; dropping denial records", "dropped", dropped)
		}
	}
}

func (s *AuditService) Start(ctx context.Context) error {
	go s.run(context.WithoutCancel(ctx))
	return nil
}

func (s *AuditService) Stop(ctx context.Context) error {
	close(s.quit)
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *AuditService) run(ctx context.Context) {
	defer close(s.done)
	cache := newDenialCache()
	for {
		select {
		case denial := <-s.denials:
			s.writeDenial(ctx, cache, denial)
		case <-s.quit:
			for {
				select {
				case denial := <-s.denials:
					s.writeDenial(ctx, cache, denial)
				default:
					return
				}
			}
		}
	}
}

func (s *AuditService) writeDenial(ctx context.Context, cache *denialCache, denial pendingDenial) {
	if cache.seenRecently(denial.key, denial.row.CreatedAt) {
		return
	}
	if err := s.db.WithContext(ctx).Create(new(denial.row)).Error; err != nil {
		slog.WarnContext(ctx, "Failed to record authorization denial", "permission", denial.row.Metadata["permission"], "resource", kit.FromPtr(denial.row.ResourceID), "error", err)
	}
}

// pendingDenial is a queued denial event with its deduplication key.
type pendingDenial struct {
	row event.Event
	key string
}
