package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2"
	"gorm.io/gorm"
)

type (
	stmtCachePool struct {
		*sql.DB
		stmts     *lru.Cache[string, *sql.Stmt]
		mu        sync.Mutex
		preparing map[string]struct{}
	}

	stmtCacheTx struct {
		*sql.Tx
		pool *stmtCachePool
	}
)

const (
	evictedStmtCloseDelay = 5 * time.Second
	stmtWarmTimeout       = 30 * time.Second
)

func newStmtCachePool(db *sql.DB) (*stmtCachePool, error) {
	stmts, err := lru.NewWithEvict(preparedStmtMaxSize, closeEvictedStmt)
	if err != nil {
		return nil, fmt.Errorf("failed to create statement cache: %w", err)
	}
	return &stmtCachePool{
		DB:        db,
		stmts:     stmts,
		preparing: make(map[string]struct{}),
	}, nil
}

func closeEvictedStmt(_ string, stmt *sql.Stmt) {
	go func() {
		time.Sleep(evictedStmtCloseDelay)
		_ = stmt.Close()
	}()
}

func (p *stmtCachePool) stmt(ctx context.Context, query string) (*sql.Stmt, error) {
	if stmt, ok := p.stmts.Get(query); ok {
		return stmt, nil
	}
	stmt, err := p.DB.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	if cached, ok, _ := p.stmts.PeekOrAdd(query, stmt); ok {
		_ = stmt.Close()
		return cached, nil
	}
	return stmt, nil
}

func (p *stmtCachePool) warm(query string) {
	p.mu.Lock()
	if _, busy := p.preparing[query]; busy {
		p.mu.Unlock()
		return
	}
	p.preparing[query] = struct{}{}
	p.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), stmtWarmTimeout)
		defer cancel()
		if _, err := p.stmt(ctx, query); err != nil {
			slog.DebugContext(ctx, "Failed to prepare cached statement", "error", err)
		}
		p.mu.Lock()
		delete(p.preparing, query)
		p.mu.Unlock()
	}()
}

func (p *stmtCachePool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	stmt, err := p.stmt(ctx, query)
	if err != nil {
		return nil, err
	}
	return stmt.ExecContext(ctx, args...)
}

func (p *stmtCachePool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	stmt, err := p.stmt(ctx, query)
	if err != nil {
		return nil, err
	}
	return stmt.QueryContext(ctx, args...)
}

func (p *stmtCachePool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, err := p.stmt(ctx, query)
	if err != nil {
		return p.DB.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

func (p *stmtCachePool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &stmtCacheTx{Tx: tx, pool: p}, nil
}

func (p *stmtCachePool) GetDBConn() (*sql.DB, error) {
	return p.DB, nil
}

func (t *stmtCacheTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	stmt, ok := t.pool.stmts.Get(query)
	if !ok {
		t.pool.warm(query)
		return t.Tx.ExecContext(ctx, query, args...)
	}
	return t.Tx.StmtContext(ctx, stmt).ExecContext(ctx, args...)
}

func (t *stmtCacheTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	stmt, ok := t.pool.stmts.Get(query)
	if !ok {
		t.pool.warm(query)
		return t.Tx.QueryContext(ctx, query, args...)
	}
	return t.Tx.StmtContext(ctx, stmt).QueryContext(ctx, args...)
}

func (t *stmtCacheTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, ok := t.pool.stmts.Get(query)
	if !ok {
		t.pool.warm(query)
		return t.Tx.QueryRowContext(ctx, query, args...)
	}
	return t.Tx.StmtContext(ctx, stmt).QueryRowContext(ctx, args...)
}

func (t *stmtCacheTx) GetDBConn() (*sql.DB, error) {
	return t.pool.DB, nil
}
