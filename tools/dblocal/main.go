// Command dblocal runs Postgres on 127.0.0.1:5434 with data in .data/pg, no Docker.
// DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5434/postgres
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/omarchaouki/zaytona-go/internal/db/dbtest"
)

func main() {
	dataPath, err := filepath.Abs(filepath.Join(".data", "pg"))
	if err != nil {
		fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o750); err != nil {
		fail(err)
	}
	url, stop, err := dbtest.Start(dbtest.Options{Port: 5434, DataPath: dataPath, Logger: os.Stderr})
	if err != nil {
		fail(err)
	}
	fmt.Println("Postgres 16 running. DATABASE_URL=" + url)
	fmt.Println("Data in " + dataPath + ". Ctrl+C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	if err := stop(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "dblocal:", err)
	os.Exit(1)
}
