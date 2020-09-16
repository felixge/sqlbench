package main

import (
	"context"
	"database/sql"
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
