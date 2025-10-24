package pgx

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxTxKeyType struct{}

var pgxTxKey = pgxTxKeyType{}

type TxManager struct {
	db *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{db: pool}
}

func (m *TxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if tx := TxFromContext(ctx); tx != nil {
		return fn(ctx) // already in tx
	}
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ctx = contextWithTx(ctx, tx)
	if err := fn(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func contextWithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, pgxTxKey, tx)
}

func TxFromContext(ctx context.Context) pgx.Tx {
	if v := ctx.Value(pgxTxKey); v != nil {
		if tx, ok := v.(pgx.Tx); ok {
			return tx
		}
	}
	return nil
}
