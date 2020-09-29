package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) (context.Context, *sql.Conn, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	db, err := sql.Open("pgx", "postgres://")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		cancel()
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		} else if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}

	return ctx, conn, cleanup
}

func Test_loadBaseline(t *testing.T) {
	queries, err := loadBaseline(filepath.Join("test-fixtures", "sum_baseline.csv"))
	if err != nil {
		t.Fatal(err)
	} else if got, want := len(queries), 3; got != want {
		t.Fatalf("got=%d want=%d", got, want)
	} else if got, want := len(queries[0].Seconds), 1169; got != want {
		t.Fatalf("got=%d want=%d", got, want)
	} else if got, dontWant := queries[0].Mean, 0.0; got == dontWant {
		t.Fatalf("got=%f don't want=%f", got, dontWant)
	}
}
