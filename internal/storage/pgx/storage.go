package pgx

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	pool      *pgxpool.Pool
	txManager *TxManager
}

func New(ctx context.Context, connString string) (*Storage, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	return &Storage{
		pool:      pool,
		txManager: NewTxManager(pool),
	}, nil
}

// interface and func ex --> to avoid duplicating code

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Storage) getExecutor(ctx context.Context) execer {
	if tx := TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.pool
}

func (s *Storage) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Storage) Close() {
	s.pool.Close()
}

func (s *Storage) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.txManager.WithTx(ctx, fn)
}
