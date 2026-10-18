// Package dbtest starts a throwaway embedded Postgres for integration tests and
// for `task db:local`. No Docker needed; binaries are downloaded once and cached.
package dbtest

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omarchaouki/zaytona-go/internal/db"
)

// PGVersion is the Postgres major used locally and in tests (same as docker-compose).
const PGVersion = embeddedpostgres.V16

// Options for Start.
type Options struct {
	Port     uint32
	DataPath string // empty: temporary
	Logger   *os.File
}

// Start runs an embedded Postgres and returns its URL and a stop function.
func Start(opts Options) (string, func() error, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	runtime, err := os.MkdirTemp("", "zaytona-pg-runtime-*")
	if err != nil {
		return "", nil, err
	}
	cfg := embeddedpostgres.DefaultConfig().
		Version(PGVersion).
		Port(opts.Port).
		Username("postgres").Password("postgres").Database("postgres").
		CachePath(filepath.Join(cacheDir, "zaytona", "embedded-postgres")).
		RuntimePath(runtime).
		StartTimeout(60 * time.Second).
		Locale("C")
	if opts.DataPath != "" {
		cfg = cfg.DataPath(opts.DataPath)
	}
	if opts.Logger != nil {
		cfg = cfg.Logger(opts.Logger)
	} else {
		cfg = cfg.Logger(nil)
	}
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		_ = os.RemoveAll(runtime)
		return "", nil, fmt.Errorf("start embedded postgres: %w", err)
	}
	url := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", opts.Port)
	return url, func() error {
		err := pg.Stop()
		_ = os.RemoveAll(runtime)
		return err
	}, nil
}

// FreePort asks the OS for an unused TCP port.
func FreePort() (uint32, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil // #nosec G115 -- TCP ports fit in uint32
}

// RunMain starts one Postgres for a test package, sets URL, runs the tests and stops it.
// Use from TestMain: os.Exit(dbtest.RunMain(m, &url)).
func RunMain(m *testing.M, url *string) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	port, err := FreePort()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	u, stop, err := Start(Options{Port: port})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	*url = u
	code := m.Run()
	if err := stop(); err != nil {
		fmt.Fprintln(os.Stderr, "stop postgres:", err)
	}
	return code
}

// NewDatabase creates a fresh, migrated database on the server at baseURL and
// returns a pool to it. Each test gets its own database.
func NewDatabase(t testing.TB, baseURL string) *pgxpool.Pool {
	t.Helper()
	if baseURL == "" {
		t.Skip("integration test: embedded Postgres not started (-short)")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("t_%d", time.Now().UnixNano())
	// Identifier built from digits only; CREATE DATABASE cannot take a parameter.
	if _, err := admin.Exec(ctx, "create database "+name); err != nil { // #nosec G202 -- digits-only identifier
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, replaceDB(baseURL, name), 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func replaceDB(url, name string) string {
	const suffix = "/postgres?sslmode=disable"
	return url[:len(url)-len(suffix)] + "/" + name + "?sslmode=disable"
}
