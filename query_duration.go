package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type queryDurationFunc = func(context.Context, *sql.Conn, string, bool) func() (time.Duration, error)

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

func clientDuration(ctx context.Context, conn *sql.Conn, query string, includePlanning bool) func() (time.Duration, error) {
	var (
		queryContext func(context.Context, ...interface{}) (*sql.Rows, error)
		prepareErr   error
	)

	if !includePlanning {
		fmt.Printf("prepare\n")
		stmt, err := conn.PrepareContext(ctx, query)
		if err != nil {
			prepareErr = err
		} else {
			queryContext = stmt.QueryContext
		}
	} else {
		queryContext = func(ctx context.Context, args ...interface{}) (*sql.Rows, error) {
			return conn.QueryContext(ctx, query, args...)
		}
	}

	return func() (time.Duration, error) {
		if prepareErr != nil {
			return 0, prepareErr
		}

		start := time.Now()
		rows, err := queryContext(ctx)
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
}

func explainDuration(ctx context.Context, conn *sql.Conn, query string, includePlanning bool) func() (time.Duration, error) {
	type explainQuery struct {
		ExecutionTime float64 `json:"Execution Time"`
		PlanningTime  float64 `json:"Planning Time"`
	}

	query = "EXPLAIN (ANALYZE, FORMAT JSON) " + query
	return func() (time.Duration, error) {
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

		executionTime := queries[0].ExecutionTime
		planningTime := queries[0].PlanningTime
		// Seems to happen with Docker for Mac, let's not silently collect these
		// as valid samples.
		if executionTime < 0 {
			return 0, fmt.Errorf(`"Execution Time" %f is < 0`, executionTime)
		} else if planningTime < 0 {
			return 0, fmt.Errorf(`"Planning Time" %f is < 0`, planningTime)
		}

		totalTime := executionTime
		if includePlanning {
			totalTime += planningTime
		}

		d := time.Duration(float64(time.Millisecond) * totalTime)
		return d, nil
	}
}
