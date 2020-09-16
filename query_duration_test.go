package main

import (
	"testing"
)

func Test_queryDurationFuncs(t *testing.T) {
	ctx, conn, cleanup := setup(t)
	defer cleanup()

	for name, fn := range queryDurationFuncs {
		t.Run(name, func(t *testing.T) {
			d, err := fn(ctx, conn, "SELECT 1")
			if err != nil {
				t.Fatal(err)
			} else if d <= 0 {
				t.Fatalf("bad duration: %s", d)
			}
		})
	}
}
