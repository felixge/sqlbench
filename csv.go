package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"strconv"
)

//go:generate stringer -type=CSVColumn -linecomment

type CSVColumn int

const (
	ColumnIterations CSVColumn = iota // iterations
	ColumnQuery                       // query
	ColumnSeconds                     // seconds
	ColumnLast
)

// loadBaseline loads the query measurements contained in the csvPath file. The
// resulting Query structs don't have the Path or SQL field populated.
func loadBaseline(csvPath string) ([]*Query, error) {
	data, err := ioutil.ReadFile(csvPath)
	if err != nil {
		return nil, err
	}
	cr := csv.NewReader(bytes.NewBuffer(data))
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}

	var (
		queries []*Query
		lookup  = map[string]*Query{}
	)

	for _, record := range records[1:] {
		query := lookup[record[ColumnQuery]]
		if query == nil {
			query = &Query{Name: record[ColumnQuery]}
			lookup[record[ColumnQuery]] = query
			queries = append(queries, query)
		}

		seconds, err := strconv.ParseFloat(record[ColumnSeconds], 64)
		if err != nil {
			return nil, fmt.Errorf("bad seconds value: %w", err)
		}
		query.Seconds = append(query.Seconds, seconds)
	}
	for _, query := range queries {
		query.UpdateStats()
	}
	return queries, nil
}
