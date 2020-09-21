package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/montanaflynn/stats"
	"github.com/olekukonko/tablewriter"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		methodF = flag.String("m", "explain", "Method for measuring the query time. One of: "+queryDurationMethods())
		connF   = flag.String("c", "postgres://", strings.TrimSpace(`
Connection URL or DSN for connecting to PostgreSQL as understood by pgx [1].
E.g.: postgres://user:secret@localhost:5432/my_db?sslmode=disable

Alternatively you can use standard PostgreSQL environment variables [2] such as
PGHOST, PGPORT, PGPASSWORD, ... .

[1] https://pkg.go.dev/github.com/jackc/pgx/v4/stdlib?tab=doc
[2] https://www.postgresql.org/docs/current/libpq-envars.html
`))
		csvF        = flag.String("o", "", "Output path for writing individual measurements in CSV format.")
		iterationsF = flag.Int64("n", -1, "Terminate after the given number of iterations.")
		secondsF    = flag.Float64("t", -1, "Terminate after the given number of seconds.")
		silentF     = flag.Bool("s", false, "Silent mode for non-interactive use, only prints stats once after terminating.")
		verboseF    = flag.Bool("v", false, "Print the content of all SQL query files that were executed at the end.")
	)
	flag.Parse()

	methodFn, ok := queryDurationFuncs[*methodF]
	if !ok {
		return fmt.Errorf("-m: unknown method: %q: must be one of %s", *methodF, queryDurationMethods())
	}

	bench, err := LoadBenchmark(flag.Args()...)
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", *connF)
	if err != nil {
		return err
	}

	ctx := context.TODO()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}

	if err := execIndividually(ctx, conn, bench.Init); err != nil {
		return fmt.Errorf("failed to run init sql: %w", err)
	}

	drawTicker := &time.Ticker{}
	if *silentF == false {
		drawTicker = time.NewTicker(time.Second / 10)
		defer drawTicker.Stop()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	var secondsTimer = &time.Timer{}
	secondsD := time.Duration(float64(time.Second) * *secondsF)
	if secondsD > 0 {
		secondsTimer = time.NewTimer(secondsD)
		defer secondsTimer.Stop()
	}

	var csvW *csv.Writer
	if *csvF != "" {
		csvFile, err := os.OpenFile(*csvF, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
		defer csvFile.Close()
		csvW = csv.NewWriter(csvFile)
		if err := csvW.Write([]string{"iteration", "query", "seconds"}); err != nil {
			return err
		}
		defer csvW.Flush()
	}

	var exitMsg string

outerLoop:
	for i := int64(1); ; i++ {
		for _, query := range bench.Queries {
			for {
				delta, err := methodFn(ctx, conn, query.SQL)
				if err != nil {
					return err
					// Deal with PostgreSQL reporting negative execution times, probably
					// a docker for mac issue on my machine.
				} else if delta < 0 {
					continue
				}
				query.Seconds = append(query.Seconds, delta.Seconds()*1000)
				if csvW != nil {
					if err := csvW.Write([]string{
						fmt.Sprintf("%d", i),
						query.Name,
						fmt.Sprintf("%f", delta.Seconds()),
					}); err != nil {
						return err
					}
				}
				break
			}
		}

		if i >= *iterationsF && *iterationsF > 0 {
			exitMsg = fmt.Sprintf("Stopping after %d iterations as requested.", i)
			break
		}
		select {
		case <-drawTicker.C:
			if err := bench.Update(); err != nil {
				return err
			} else if err := render(bench.Queries, *silentF == false); err != nil {
				return err
			}
		case sig := <-sigCh:
			exitMsg = fmt.Sprintf("Stopping due to receiving %s signal.", sig)
			break outerLoop
		case <-secondsTimer.C:
			exitMsg = fmt.Sprintf("Stopping after %s as requested.", secondsD)
			break outerLoop
		default:
		}
	}

	if err := bench.Update(); err != nil {
		return err
	} else if err := render(bench.Queries, *silentF == false); err != nil {
		return err
	}
	fmt.Printf("\n%s\n", exitMsg)

	if err := execIndividually(ctx, conn, bench.Destroy); err != nil {
		return fmt.Errorf("failed to run destroy sql: %w", err)
	}

	if *verboseF {
		args := strings.Join(os.Args[1:], " ")
		fmt.Printf("\nsqlbench %s\n\n", args)
		for _, q := range bench.Queries {
			fmt.Printf("==> %s <==\n%s\n", q.Path, q.SQL)
		}
	}

	return nil
}

func render(queries []*Query, clear bool) error {
	screen := &bytes.Buffer{}

	if clear {
		// See https://en.wikipedia.org/wiki/ANSI_escape_code#Terminal_output_sequences
		// move cursor to 0, 0
		fmt.Fprintf(screen, "\033[%d;%dH", 0, 0)
		// reset screen
		fmt.Fprintf(screen, "\033[2J\033[3J")
	}

	headers := []string{""}
	rows := [][]string{
		{"n"},
		{"min"},
		{"max"},
		{"mean"},
		{"stddev"},
		{"median"},
		{"p90"},
		{"p95"},
	}

	var firstFields []float64
	for i, query := range queries {
		headers = append(headers, query.Name)
		fields := []float64{
			query.Min,
			query.Max,
			query.Mean,
			query.StdDev,
			query.Median,
			query.P90,
			query.P95,
		}
		if firstFields == nil {
			firstFields = fields
		}

		rows[0] = append(rows[0], fmt.Sprintf("%d", len(query.Seconds)))
		for j, field := range fields {
			var xStr = ""
			if i > 0 {
				xStr = fmt.Sprintf(" (%.2fx)", field/firstFields[j])
			}
			rows[j+1] = append(rows[j+1], fmt.Sprintf("%.2f%s", field, xStr))
		}
	}

	table := tablewriter.NewWriter(screen)
	table.SetAutoFormatHeaders(false)
	table.SetHeader(headers)
	table.SetBorder(false)
	table.AppendBulk(rows)
	table.Render()
	screen.WriteTo(os.Stdout)
	return nil
}

func LoadBenchmark(paths ...string) (*Benchmark, error) {
	queries, err := LoadQueries(paths...)
	if err != nil {
		return nil, err
	}
	b := &Benchmark{}
	for _, q := range queries {
		// Our init or destory SQL might contain non-transactional queries such as
		// `VACUUM`, so we'll try to execute them one by one. This will fail if a
		// ';' is contained in a string or similar, but that's probably rarely the
		// case. We could import a proper PostgreSQL query parser to solve this at
		// some point.
		if strings.HasSuffix(q.Name, "init") {
			b.Init = append(b.Init, strings.Split(q.SQL, ";")...)
		} else if strings.HasSuffix(q.Name, "destroy") {
			b.Destroy = append(b.Init, strings.Split(q.SQL, ";")...)
		} else {
			b.Queries = append(b.Queries, q)
		}
	}
	return b, nil
}

func LoadQueries(paths ...string) ([]*Query, error) {
	var queries []*Query
	for _, path := range paths {
		q, err := loadQuery(path)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, nil
}

func loadQuery(path string) (*Query, error) {
	sql, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &Query{
		Path: path,
		Name: name,
		SQL:  string(sql),
	}, nil
}

type Benchmark struct {
	// Init SQL statements to execute before starting the benchmark.
	Init []string
	// Queries to execute during the benchmark.
	Queries []*Query
	// Destroy SQL statements to execute after finishing the benchmark.
	Destroy []string
}

// Update updates the stats of all queries and sorts them by mean execution
// time in ascending order.
func (b *Benchmark) Update() error {
	for _, query := range b.Queries {
		if err := query.UpdateStats(); err != nil {
			return err
		}
	}

	sort.Slice(b.Queries, func(i, j int) bool {
		return b.Queries[i].Mean < b.Queries[j].Mean
	})
	return nil
}

type Query struct {
	Path string
	Name string
	SQL  string

	Errors  float64
	Seconds []float64
	Min     float64
	Max     float64
	Mean    float64
	Median  float64
	StdDev  float64
	P90     float64
	P95     float64
}

func (q *Query) UpdateStats() error {
	var err error
	q.Min, err = stats.Min(q.Seconds)
	if err != nil {
		return err
	}
	q.Max, err = stats.Max(q.Seconds)
	if err != nil {
		return err
	}
	q.Mean, err = stats.Mean(q.Seconds)
	if err != nil {
		return err
	}
	q.StdDev, err = stats.StdDevS(q.Seconds)
	if err != nil {
		return err
	}
	q.Median, err = stats.Median(q.Seconds)
	if err != nil {
		return err
	}
	q.P90, err = stats.Percentile(q.Seconds, 90)
	if err != nil {
		return err
	}
	q.P95, err = stats.Percentile(q.Seconds, 95)
	if err != nil {
		return err
	}
	return nil
}

func execIndividually(ctx context.Context, conn *sql.Conn, cmds []string) error {
	for _, cmd := range cmds {
		if _, err := conn.ExecContext(ctx, cmd); err != nil {
			// TODO(fg) nice errors with line number, etc.
			return fmt.Errorf("-i: %w", err)
		}
	}
	return nil
}
