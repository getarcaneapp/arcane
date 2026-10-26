package database

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"runtime"
	"slices"
	"strings"

	"github.com/libtnb/sqlite"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/metrics"
)

const sqliteWriteConns = 2

type sqliteReadRouter struct {
	*gorm.PreparedStmtDB
	reader *gorm.PreparedStmtDB
}

func (r *sqliteReadRouter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.route(query).QueryContext(ctx, query, args...)
}

func (r *sqliteReadRouter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.route(query).QueryRowContext(ctx, query, args...)
}

func (r *sqliteReadRouter) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	if opts != nil && opts.ReadOnly {
		return r.reader.BeginTx(ctx, opts)
	}
	return r.PreparedStmtDB.BeginTx(ctx, opts)
}

func (r *sqliteReadRouter) route(query string) *gorm.PreparedStmtDB {
	trimmed := strings.TrimLeft(query, " \t\r\n(")
	if len(trimmed) >= len("select") && strings.EqualFold(trimmed[:len("select")], "select") {
		return r.reader
	}
	return r.PreparedStmtDB
}

func sqliteReadDSN(connString string) string {
	connStringURL, err := url.Parse(connString)
	if err != nil {
		return ""
	}
	qs := connStringURL.Query()
	walEnabled := slices.ContainsFunc(qs["_pragma"], func(pragma string) bool {
		return strings.EqualFold(strings.TrimSpace(pragma), "journal_mode(WAL)")
	})
	if !walEnabled ||
		slices.Contains([]string{"ro", "memory"}, qs.Get("mode")) ||
		qs.Get("cache") == "shared" ||
		strings.HasPrefix(cmp.Or(connStringURL.Opaque, connStringURL.Path), ":memory:") {
		return ""
	}
	qs.Set("_txlock", "deferred")
	qs.Add("_pragma", "query_only(1)")
	return (&url.URL{
		Scheme:   connStringURL.Scheme,
		Opaque:   connStringURL.Opaque,
		User:     connStringURL.User,
		Host:     connStringURL.Host,
		Path:     connStringURL.Path,
		RawPath:  connStringURL.RawPath,
		RawQuery: qs.Encode(),
		Fragment: connStringURL.Fragment,
	}).String()
}

func attachSQLiteReadPool(gormDB *gorm.DB, writer *sql.DB, readDSN string) (*sql.DB, error) {
	readerGorm, err := gorm.Open(sqlite.Open(readDSN), &gorm.Config{
		Logger: customGormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite read pool: %w", err)
	}
	reader, err := readerGorm.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get SQLite read pool: %w", err)
	}
	readConns := min(max(runtime.NumCPU(), 4), 8)
	reader.SetMaxIdleConns(readConns)
	reader.SetMaxOpenConns(readConns)
	reader.SetConnMaxLifetime(0)
	reader.SetConnMaxIdleTime(0)
	metrics.ReportDBStatsMetrics(reader, metric.WithAttributes(attribute.String("db.pool", "read")))
	router := &sqliteReadRouter{
		PreparedStmtDB: gorm.NewPreparedStmtDB(writer, preparedStmtMaxSize, preparedStmtTTL),
		reader:         gorm.NewPreparedStmtDB(reader, preparedStmtMaxSize, preparedStmtTTL),
	}
	gormDB.ConnPool = router
	gormDB.Statement.ConnPool = router
	return reader, nil
}
