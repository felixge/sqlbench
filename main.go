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

const version = "1.1"

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
`)+"\n")
		inCsvF      = flag.String("i", "", "Input path for CSV file with baseline measurements.")
		outCsvF     = flag.String("o", "", "Output path for writing individual measurements in CSV format.")
		iterationsF = flag.Int64("n", -1, "Terminate after the given number of iterations.")
		secondsF    = flag.Float64("t", -1, "Terminate after the given number of seconds.")
		planF       = flag.Bool("p", false, strings.TrimSpace(`
Include the query planning time. For -m explain this is accomplished by adding
the "Planning Time" to the measurement. For -m client this is done by not using
prepared statements.
`))
		silentF  = flag.Bool("s", false, "Silent mode for non-interactive use, only prints stats once after terminating.")
		versionF = flag.Bool("version", false, "Print version and exit.")
		verboseF = flag.Bool("v", false, strings.TrimSpace(`
Verbose output. Print the content of all SQL queries, as well as the
PostgreSQL version.
`))
	)
	flag.Parse()

	if *versionF {
		fmt.Printf("%s\n", version)
		return nil
	}

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
		return err
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

	var baseline []*Query
	if *inCsvF != "" {
		baseline, err = loadBaseline(*inCsvF)
		if err != nil {
			return err
		}
	}

	var csvW *csv.Writer
	if *outCsvF != "" {
		csvFile, err := os.OpenFile(*outCsvF, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
		defer csvFile.Close()
		csvW = csv.NewWriter(csvFile)
		if err := csvW.Write(csvHeader()); err != nil {
			return err
		}
		defer csvW.Flush()
	}

	var exitMsg string

	preparedFns := map[string]func() (time.Duration, error){}

outerLoop:
	for i := int64(1); ; i++ {
		for _, query := range bench.Queries {
			preparedFn := preparedFns[query.Path]
			if preparedFn == nil {
				preparedFn = methodFn(ctx, conn, query.SQL, *planF)
				preparedFns[query.Path] = preparedFn
			}

			for {
				delta, err := preparedFn()
				if err != nil {
					return fmt.Errorf("%s: %w", query.Path, err)
					// Deal with PostgreSQL reporting negative execution times, probably
					// a docker for mac issue on my machine.
				} else if delta < 0 {
					continue
				}
				seconds := delta.Seconds()
				query.Seconds = append(query.Seconds, seconds)
				if csvW != nil {
					row := &CSVRow{
						Iteration: i,
						Query:     query.Name,
						Seconds:   seconds,
					}
					if record, err := row.MarshalRecord(); err != nil {
						return err
					} else if err := csvW.Write(record); err != nil {
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
			} else if err := render(bench.Queries, *silentF == false, baseline); err != nil {
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
	} else if err := render(bench.Queries, *silentF == false, baseline); err != nil {
		return err
	}
	fmt.Printf("\n%s\n", exitMsg)

	if err := execIndividually(ctx, conn, bench.Destroy); err != nil {
		return err
	}

	if *verboseF {
		var version string
		if err := db.QueryRow("SELECT version();").Scan(&version); err != nil {
			return fmt.Errorf("failed to determine PostgreSQL version: %w", err)
		}

		args := strings.Join(os.Args[1:], " ")
		fmt.Printf("\n")
		fmt.Printf("postgres version: %s\n", version)
		fmt.Printf("sqlbench %s\n\n", args)
		all := append(append([]*Query{bench.Init}, bench.Queries...), bench.Destroy)
		for _, q := range all {
			if q != nil {
				fmt.Printf("==> %s <==\n%s\n", q.Path, q.SQL)
			}
		}
	}

	return nil
}

func render(queries []*Query, clear bool, baseline []*Query) error {
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

	baselineLookup := map[string]*Query{}
	for _, query := range baseline {
		baselineLookup[query.Name] = query
	}

	tableFields := func(q *Query) []float64 {
		const scale = 1000
		return []float64{
			q.Min * scale,
			q.Max * scale,
			q.Mean * scale,
			q.StdDev * scale,
			q.Median * scale,
			q.P90 * scale,
			q.P95 * scale,
		}
	}

	var baselineQuery *Query
	var baselineFields []float64
	for i, query := range queries {
		headers = append(headers, query.Name)
		fields := tableFields(query)

		if len(baseline) > 0 {
			baselineQuery = baselineLookup[query.Name]
			baselineFields = tableFields(baselineQuery)
		} else if baselineFields == nil {
			baselineFields = fields
		}

		n := len(query.Seconds)
		nStr := fmt.Sprintf("%d", n)
		if baselineQuery != nil {
			baselineN := len(baselineQuery.Seconds)
			nStr += fmt.Sprintf(" (%.2fx)", float64(n)/float64(baselineN))
		}
		rows[0] = append(rows[0], nStr)

		for j, field := range fields {
			var xStr = ""
			if i > 0 || baselineQuery != nil {
				xStr = fmt.Sprintf(" (%.2fx)", field/baselineFields[j])
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
		// Our init or destroy SQL might contain non-transactional queries such as
		// `VACUUM`, so we'll try to execute them one by one. This will fail if a
		// ';' is contained in a string or similar, but that's probably rarely the
		// case. We could import a proper PostgreSQL query parser to solve this at
		// some point.
		if strings.HasSuffix(q.Name, "init") {
			b.Init = q
		} else if strings.HasSuffix(q.Name, "destroy") {
			b.Destroy = q
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
	// Init SQL statement to execute before starting the benchmark.
	Init *Query
	// Queries to execute during the benchmark.
	Queries []*Query
	// Destroy SQL query to execute after finishing the benchmark.
	Destroy *Query
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

func execIndividually(ctx context.Context, conn *sql.Conn, q *Query) error {
	if q == nil {
		return nil
	}

	for _, cmd := range strings.Split(q.SQL, ";") {
		if _, err := conn.ExecContext(ctx, cmd); err != nil {
			// TODO(fg) nice errors with line number, etc.
			return fmt.Errorf("%s: %w", q.Path, err)
		}
	}
	return nil
}

// loadBaseline loads the query measurements contained in the csvPath file. The
// resulting Query structs don't have the Path or SQL field populated.
func loadBaseline(csvPath string) ([]*Query, error) {
	rows, err := loadCSVRows(csvPath)
	if err != nil {
		return nil, err
	}

	var (
		queries []*Query
		lookup  = map[string]*Query{}
	)

	for _, row := range rows {
		query := lookup[row.Query]
		if query == nil {
			query = &Query{Name: row.Query}
			lookup[row.Query] = query
			queries = append(queries, query)
		}
		query.Seconds = append(query.Seconds, row.Seconds)
	}

	for _, query := range queries {
		query.UpdateStats()
	}
	return queries, nil
}
