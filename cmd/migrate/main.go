// Command migrate applies db/migrations to a database, including managed MySQL hosts (Aiven...) that
// need a private CA for verified TLS. The stock `migrate` CLI can't be relied on for that: its
// x-tls-ca option depends on how the binary was built (several packaged builds reject it with
// "invalid DSN"). This command reuses the same golang-migrate library the tests use, plus the same
// verified-TLS opener (dbx.OpenWithCA) the API uses, so it behaves identically everywhere.
//
//	MIGRATE_DSN='avnadmin:PASSWORD@tcp(HOST:PORT)/brightbuy' DB_CA_CERT="$(cat ca.pem)" \
//	  go run ./cmd/migrate            # up (default)
//	go run ./cmd/migrate version | down 1 | force 12
//
// MIGRATE_DSN must be the MIGRATION user (DDL rights), never the application user.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"brightbuy-backend/internal/shared/dbx"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	dsn := os.Getenv("MIGRATE_DSN")
	if dsn == "" {
		log.Fatal("MIGRATE_DSN is required, e.g. user:password@tcp(host:port)/brightbuy")
	}
	// Stored procedures/triggers are multi-statement files.
	if !strings.Contains(dsn, "multiStatements=") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "multiStatements=true"
	}

	db, err := open(dsn, os.Getenv("DB_CA_CERT"))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "db/migrations"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		log.Fatal(err)
	}
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		log.Fatalf("migration driver: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+filepath.ToSlash(abs), "mysql", driver)
	if err != nil {
		log.Fatalf("create migrator: %v", err)
	}

	cmd, arg := "up", 0
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if len(os.Args) > 2 {
		if arg, err = strconv.Atoi(os.Args[2]); err != nil {
			log.Fatalf("bad number %q", os.Args[2])
		}
	}

	switch cmd {
	case "up":
		err = m.Up()
	case "down":
		if arg <= 0 {
			log.Fatal("down needs a step count, e.g. `down 1`")
		}
		err = m.Steps(-arg)
	case "force":
		err = m.Force(arg)
	case "version":
		v, dirty, verr := m.Version()
		if verr != nil {
			log.Fatal(verr)
		}
		fmt.Printf("version %d dirty=%v\n", v, dirty)
		return
	default:
		log.Fatalf("unknown command %q (up | down N | force N | version)", cmd)
	}
	switch err {
	case nil:
		fmt.Println("ok")
	case migrate.ErrNoChange:
		fmt.Println("no change: already up to date")
	default:
		log.Fatalf("%s failed: %v", cmd, err)
	}
	v, dirty, _ := m.Version()
	fmt.Printf("version %d dirty=%v\n", v, dirty)
}

// open uses verified TLS when a CA certificate is supplied (managed databases with a private CA).
func open(dsn, ca string) (*sql.DB, error) {
	if ca != "" {
		return dbx.OpenWithCA(dsn, ca)
	}
	return dbx.Open(dsn)
}
