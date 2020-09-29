package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"strconv"
)

type CSVRow struct {
	Iteration int64
	Query     string
	Seconds   float64
}

func (r *CSVRow) UnmarshalRecord(record []string) error {
	for i, val := range record {
		if err := csvColumns[i].UnmarshalColumn(val, r); err != nil {
			return err
		}
	}
	return nil
}

func (r *CSVRow) MarshalRecord() ([]string, error) {
	record := make([]string, len(csvColumns))
	for i, col := range csvColumns {
		val, err := col.MarshalColumn(r)
		if err != nil {
			return nil, err
		}
		record[i] = val
	}
	return record, nil
}

type csvColumn struct {
	Name            string
	UnmarshalColumn func(string, *CSVRow) error
	MarshalColumn   func(*CSVRow) (string, error)
}

var csvColumns = []csvColumn{
	{
		"iteration",
		func(val string, r *CSVRow) (err error) {
			r.Iteration, err = strconv.ParseInt(val, 10, 64)
			return
		},
		func(r *CSVRow) (string, error) {
			return fmt.Sprintf("%d", r.Iteration), nil
		},
	},
	{
		"query",
		func(val string, r *CSVRow) error {
			r.Query = val
			return nil
		},
		func(r *CSVRow) (string, error) {
			return r.Query, nil
		},
	},
	{
		"seconds",
		func(val string, r *CSVRow) (err error) {
			r.Seconds, err = strconv.ParseFloat(val, 64)
			return
		},
		func(r *CSVRow) (string, error) {
			return fmt.Sprintf("%f", r.Seconds), nil
		},
	},
}

// csvHeader returns the CSV header columns.
func csvHeader() []string {
	header := make([]string, len(csvColumns))
	for i, col := range csvColumns {
		header[i] = col.Name
	}
	return header
}

func loadCSVRows(csvPath string) ([]*CSVRow, error) {
	data, err := ioutil.ReadFile(csvPath)
	if err != nil {
		return nil, err
	}
	cr := csv.NewReader(bytes.NewBuffer(data))
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}

	var rows []*CSVRow
	for i, record := range records {
		if err := checkCSVColumns(record); err != nil {
			return nil, fmt.Errorf("row=%d: %w", i+1, err)
		}

		switch i {
		case 0:
			for i, got := range record {
				if want := csvColumns[i].Name; got != want {
					return nil, fmt.Errorf("unexpected header column %d: got=%q want=%q", i, got, want)
				}
			}
		default:
			row := &CSVRow{}
			if err := row.UnmarshalRecord(record); err != nil {
				return nil, fmt.Errorf("row=%d: %w", i+1, err)
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// checkCSVColumns returns an error if record doesn't have the right number
// of columns.
func checkCSVColumns(record []string) error {
	if got, want := len(record), len(csvColumns); got != want {
		return fmt.Errorf("bad number of columns: got=%d want=%d", got, want)
	}
	return nil
}
