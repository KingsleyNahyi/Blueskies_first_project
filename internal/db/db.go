package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	//libsql - tursodbs forked version of sqllite
	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" driver used for local file: DSNs in dev
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// function for the connection of the database takes the urla nd the authentication string.
func Connect(databaseURL, authToken string) (*sql.DB, error) {

	//using the database URL directly if connecting locally gluing an authentication token if connecting remotely.
	dsn := databaseURL
	if authToken != "" {
		dsn = fmt.Sprintf("%s?authToken=%s", databaseURL, authToken)
	}

	//sql.Open to validate if libsq as a driver exists, uses the dsn t
	//pool is created pointer to sql.DB returned
	//we use an sql.DB pointer so every connection has a reference to it instead of the actual value so they are all in sync essentially it is more efficient that requiring to update copies if they have different values.
	conn, err := sql.Open("libsql", dsn)

	//error handling
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	//using a ping to actually check if a connection works.
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("pinging db: %w", err)
	}

	//limiting the number of open connections to the databse.
	conn.SetMaxOpenConns(1)

	// Close connections that sit idle for 30s so the next request opens a fresh
	// one. A quiet app (Render free services sleep when unused) can otherwise
	// hold a connection Turso has already dropped, and the first request after
	// the quiet period fails with a "stream"/connection error.
	conn.SetConnMaxIdleTime(30 * time.Second)

	return conn, nil
}

// RunMigrations applies every .sql file in migrationsDir, in filename
// order, tracking what's already been applied in a schema_migrations
// table. Good enough for a POC; swap for goose/atlas later if needed.
func RunMigrations(conn *sql.DB, migrationsDir string) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		var already string
		err := conn.QueryRow(`SELECT filename FROM schema_migrations WHERE filename = ?`, f).Scan(&already)
		if err == nil {
			continue // already applied
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("checking migration %s: %w", f, err)
		}

		contents, err := os.ReadFile(filepath.Join(migrationsDir, f))
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", f, err)
		}

		if _, err := conn.Exec(string(contents)); err != nil {
			return fmt.Errorf("applying migration %s: %w", f, err)
		}
		if _, err := conn.Exec(`INSERT INTO schema_migrations (filename) VALUES (?)`, f); err != nil {
			return fmt.Errorf("recording migration %s: %w", f, err)
		}
		fmt.Printf("applied migration: %s\n", f)
	}

	return nil
}