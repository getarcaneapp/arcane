package database

import (
	"context"
	"database/sql"
	"errors"
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
		preparing map[string]struct{}
		mu        sync.Mutex
	}

	stmtCacheTx struct {
		*sql.Tx
		pool  *stmtCachePool
		stmts map[string]*sql.Stmt
		mu    sync.Mutex
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
	if stmt == nil {
		return
	}
	go func() {
		time.Sleep(evictedStmtCloseDelay)
		_ = stmt.Close()
	}()
}

func (p *stmtCachePool) stmt(ctx context.Context, query string) (*sql.Stmt, error) {
	if stmt, ok := p.stmts.Get(query); ok && stmt != nil {
		return stmt, nil
	}
	stmt, err := p.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	cached, exists, _ := p.stmts.PeekOrAdd(query, stmt)
	if !exists {
		return stmt, nil
	}
	if cached == nil {
		p.mu.Lock()
		if cached, _ = p.stmts.Peek(query); cached == nil {
			p.stmts.Add(query, stmt)
		}
		p.mu.Unlock()
	}
	if cached != nil {
		_ = stmt.Close()
		return cached, nil
	}
	return stmt, nil
}

func (p *stmtCachePool) warm(ctx context.Context, query string) {
	p.mu.Lock()
	if _, busy := p.preparing[query]; busy {
		p.mu.Unlock()
		return
	}
	p.preparing[query] = struct{}{}
	p.mu.Unlock()
	go func() {
		warmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stmtWarmTimeout)
		defer cancel()
		if _, err := p.stmt(warmCtx, query); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			slog.DebugContext(warmCtx, "Statement cannot be cached outside a transaction", "error", err)
			p.stmts.PeekOrAdd(query, nil)
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
	return &stmtCacheTx{
		Tx:    tx,
		pool:  p,
		stmts: make(map[string]*sql.Stmt),
	}, nil
}

func (p *stmtCachePool) GetDBConn() (*sql.DB, error) {
	return p.DB, nil
}

func (t *stmtCacheTx) stmt(ctx context.Context, query string) (*sql.Stmt, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if stmt, ok := t.stmts[query]; ok {
		return stmt, true
	}
	parent, ok := t.pool.stmts.Get(query)
	if !ok {
		t.pool.warm(ctx, query)
		return nil, false
	}
	if parent == nil {
		return nil, false
	}
	stmt := t.StmtContext(ctx, parent)
	t.stmts[query] = stmt
	return stmt, true
}

func (t *stmtCacheTx) forget(query string) {
	t.mu.Lock()
	delete(t.stmts, query)
	t.mu.Unlock()
}

func (t *stmtCacheTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	stmt, ok := t.stmt(ctx, query)
	if !ok {
		return t.Tx.ExecContext(ctx, query, args...)
	}
	result, err := stmt.ExecContext(ctx, args...)
	if err != nil {
		t.forget(query)
	}
	return result, err
}

func (t *stmtCacheTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	stmt, ok := t.stmt(ctx, query)
	if !ok {
		return t.Tx.QueryContext(ctx, query, args...)
	}
	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		t.forget(query)
	}
	return rows, err
}

func (t *stmtCacheTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, ok := t.stmt(ctx, query)
	if !ok {
		return t.Tx.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

func (t *stmtCacheTx) GetDBConn() (*sql.DB, error) {
	return t.pool.DB, nil
}
