// Package store wraps the sqlc queries with transactions and repository methods.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
)

// ErrNotFound is returned when a row does not exist (handlers map it to 404).
var ErrNotFound = errors.New("store: not found")

// Store gives access to the pool and the generated queries.
type Store struct {
	Pool *pgxpool.Pool
	Q    *dbq.Queries
}

// New wraps a pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool, Q: dbq.New(pool)}
}

// InTx runs fn in a transaction, committing when it returns nil.
func (s *Store) InTx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit
	if err := fn(s.Q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NotFound converts pgx.ErrNoRows to ErrNotFound.
func NotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
