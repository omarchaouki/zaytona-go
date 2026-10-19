// Package db owns the connection pool and the embedded goose migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the embedded migration files.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err)
	}
	return sub
}

// Open creates a pgx pool using the extended protocol only, with decimal support
// and a per-statement timeout. The timeout is set with SET after connecting
// rather than as a startup parameter, which connection poolers (Supabase's
// session pooler) may not pass on.
func Open(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.ConnConfig.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	}
	cfg.MaxConns = maxConns
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.RuntimeParams["application_name"] = "zaytona"
	cfg.AfterConnect = afterConnect("5000")
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration and returns the resulting version.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	p, closeDB, err := provider(ctx, pool)
	if err != nil {
		return 0, err
	}
	defer closeDB()
	if _, err := p.Up(ctx); err != nil {
		return 0, fmt.Errorf("migrate up: %w", err)
	}
	return p.GetDBVersion(ctx)
}

// MigrateDownAll rolls every migration back (tests only).
func MigrateDownAll(ctx context.Context, pool *pgxpool.Pool) error {
	p, closeDB, err := provider(ctx, pool)
	if err != nil {
		return err
	}
	defer closeDB()
	_, err = p.DownTo(ctx, 0)
	return err
}

// provider runs migrations on a separate small pool without the 5 s
// statement timeout of the application pool (an index on a large table may
// take longer); it is closed with the provider.
func provider(ctx context.Context, pool *pgxpool.Pool) (*goose.Provider, func(), error) {
	cfg := pool.Config()
	cfg.AfterConnect = afterConnect("0")
	cfg.MaxConns = 2
	mpool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("migration pool: %w", err)
	}
	sqlDB := stdlib.OpenDBFromPool(mpool)
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, Migrations())
	if err != nil {
		_ = sqlDB.Close()
		mpool.Close()
		return nil, nil, fmt.Errorf("goose provider: %w", err)
	}
	return p, func() {
		closeQuietly(sqlDB)
		mpool.Close()
	}, nil
}

func closeQuietly(db *sql.DB) { _ = db.Close() }

// afterConnect registers the decimal type and sets the statement timeout
// (milliseconds, "0" for none) on every new connection.
func afterConnect(timeoutMS string) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, c *pgx.Conn) error {
		pgxdecimal.Register(c.TypeMap())
		_, err := c.Exec(ctx, "select set_config('statement_timeout', $1, false)", timeoutMS)
		return err
	}
}
