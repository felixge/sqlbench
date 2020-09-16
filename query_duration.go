package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type queryDurationFunc = func(context.Context, *sql.Conn, string) (time.Duration, error)

var queryDurationFuncs = map[string]queryDurationFunc{
	"client":  clientDuration,
	"explain": explainDuration,
}

var queryDurationMethods = func() string {
	var list []string
	for method := range queryDurationFuncs {
		list = append(list, fmt.Sprintf("%q", method))
	}
	return strings.Join(list, ", ")
}

func clientDuration(ctx context.Context, conn *sql.Conn, query string) (time.Duration, error) {
	start := time.Now()
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		// do nothing
	}
	if err := rows.Err(); err != nil {
		return 0, err
	} else if err := rows.Close(); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func explainDuration(ctx context.Context, conn *sql.Conn, query string) (time.Duration, error) {
	type explainQuery struct {
		ExecutionTime float64 `json:"Execution Time"`
	}

	query = "EXPLAIN (ANALYZE, FORMAT JSON) " + query
	var explainJSON []byte
	if err := conn.QueryRowContext(ctx, query).Scan(&explainJSON); err != nil {
		return 0, err
	}
	var queries []explainQuery
	if err := json.Unmarshal(explainJSON, &queries); err != nil {
		return 0, err
	} else if len(queries) != 1 {
		return 0, fmt.Errorf("bad json: %q", explainJSON)
	}
	d := time.Duration(float64(time.Millisecond) * queries[0].ExecutionTime)
	if d < 0 {
		return 0, fmt.Errorf("duration %s is < 0", d)
	}
	return d, nil
}
