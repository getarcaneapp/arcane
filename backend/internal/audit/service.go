package audit

import (
	"context"
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
)

type AuditService struct {
	db             *database.DB
	denialsEnabled func(context.Context) bool
	denials        chan AuditLog
	quit           chan struct{}
	done           chan struct{}
	dropped        atomic.Uint64
}

func NewAuditService(db *database.DB, denialsEnabled func(context.Context) bool) *AuditService {
	return &AuditService{
		db:             db,
		denialsEnabled: denialsEnabled,
		denials:        make(chan AuditLog, denialQueueCapacity),
		quit:           make(chan struct{}),
		done:           make(chan struct{}),
	}
}

func RecordInTx(ctx context.Context, tx *gorm.DB, entry types.AuditEntry) error {
	if err := tx.Create(new(newLog(ctx, entry, types.AuthzDenial{}))).Error; err != nil {
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
	row := newLog(ctx, types.AuditEntry{
		Action:        types.AuditActionAuthzDenied,
		Outcome:       types.AuditOutcomeDenied,
		EnvironmentID: kit.Ternary(denial.EnvironmentID == "", nil, new(denial.EnvironmentID)),
		Permission:    denial.Permission,
	}, denial)
	select {
	case s.denials <- row:
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
		case row := <-s.denials:
			s.writeDenial(ctx, cache, row)
		case <-s.quit:
			for {
				select {
				case row := <-s.denials:
					s.writeDenial(ctx, cache, row)
				default:
					return
				}
			}
		}
	}
}

func (s *AuditService) writeDenial(ctx context.Context, cache *denialCache, row AuditLog) {
	if cache.seenRecently(denialKey(row), row.CreatedAt) {
		return
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		slog.WarnContext(ctx, "Failed to record authorization denial", "permission", row.Permission, "path", row.RequestPath, "error", err)
	}
}

func (s *AuditService) List(ctx context.Context, params pagination.QueryParams, filter types.AuditFilter) ([]types.AuditLog, pagination.Response, error) {
	query := applyFilter(s.db.WithContext(ctx).Model(&AuditLog{}), filter)
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("action LIKE ? OR actor_username LIKE ? OR target_name LIKE ? OR target_id LIKE ? OR permission LIKE ?", pattern, pattern, pattern, pattern, pattern)
	}
	var rows []AuditLog
	resp, err := pagination.PaginateAndSortDB(params, query, &rows)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate audit logs: %w", err)
	}
	out := make([]types.AuditLog, len(rows))
	for i, row := range rows {
		out[i] = toDTO(row)
	}
	return out, resp, nil
}

func (s *AuditService) Get(ctx context.Context, id string) (types.AuditLog, error) {
	row, err := dbutil.FirstWhere[AuditLog](ctx, s.db.DB, common.ErrAuditLogNotFound, "id = ?", id)
	if err != nil {
		return types.AuditLog{}, err
	}
	return toDTO(*row), nil
}

func (s *AuditService) Export(ctx context.Context, filter types.AuditFilter, format string, w io.Writer) error {
	if format != types.AuditExportFormatCSV && format != types.AuditExportFormatNDJSON {
		return common.Classify(common.ErrValidation, fmt.Errorf("unsupported audit export format %q", format))
	}
	rows, err := applyFilter(s.db.WithContext(ctx).Model(&AuditLog{}), filter).Order("created_at, id").Rows()
	if err != nil {
		return fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	writer := csv.NewWriter(w)
	if format == types.AuditExportFormatCSV {
		if writeErr := writer.Write(csvHeader); writeErr != nil {
			return writeErr
		}
	}
	for rows.Next() {
		var row AuditLog
		if scanErr := s.db.ScanRows(rows, &row); scanErr != nil {
			return fmt.Errorf("failed to read audit log: %w", scanErr)
		}
		if writeErr := writeExportRow(w, writer, format, row); writeErr != nil {
			return writeErr
		}
	}
	writer.Flush()
	return errors.Join(rows.Err(), writer.Error())
}

func writeExportRow(w io.Writer, writer *csv.Writer, format string, row AuditLog) error {
	if format == types.AuditExportFormatNDJSON {
		if err := json.MarshalWrite(w, toDTO(row)); err != nil {
			return fmt.Errorf("failed to encode audit log: %w", err)
		}
		_, err := io.WriteString(w, "\n")
		return err
	}
	record, err := csvRecord(row)
	if err != nil {
		return fmt.Errorf("failed to encode audit log: %w", err)
	}
	return writer.Write(record)
}

func (s *AuditService) Prune(ctx context.Context, olderThan time.Time) (int64, error) {
	result := s.db.WithContext(ctx).Where("created_at < ?", olderThan).Delete(&AuditLog{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to prune audit logs: %w", result.Error)
	}
	return result.RowsAffected, nil
}
