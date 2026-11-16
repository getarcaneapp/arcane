package database

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

type stmtCachePool struct {
	*gorm.PreparedStmtDB
	db *sql.DB
}

func (p *stmtCachePool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	return p.db.BeginTx(ctx, opts)
}
