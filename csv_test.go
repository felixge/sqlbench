package main

import (
	"path/filepath"
	"testing"
)

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
