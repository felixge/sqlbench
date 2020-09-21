# sqlbench

sqlbench measures and compares the execution time of one or more SQL queries.

![screen recording](./recording/recording-min.gif)

The main use case is benchmarking simple CPU-bound query variants against each other during local development.

Only PostgreSQL is supported at this point, but pull requests for MySQL or other databases are welcome.

## Install

To install or update sqlbench, run:

```
$ go get -u github.com/felixge/sqlbench
```

## Usage

```
Usage of sqlbench:
  -c string
    	Connection URL or DSN for connecting to PostgreSQL as understood by pgx [1].
    	E.g.: postgres://user:secret@localhost:5432/my_db?sslmode=disable
    	
    	Alternatively you can use standard PostgreSQL environment variables [2] such as
    	PGHOST, PGPORT, PGPASSWORD, ... .
    	
    	[1] https://pkg.go.dev/github.com/jackc/pgx/v4/stdlib?tab=doc
    	[2] https://www.postgresql.org/docs/current/libpq-envars.html
    	(default "postgres://")
  -m string
    	Method for measuring the query time. One of: "client", "explain" (default "explain")
  -n int
    	Terminate after the given number of iterations. (default -1)
  -o string
    	Output path for writing individual measurements in CSV format.
  -p	Include the query planning time. For -m explain this is accomplished by adding
    	the "Planning Time" to the measurement. For -m client this is done by not using
    	prepared statements.
  -s	Silent mode for non-interactive use, only prints stats once after terminating.
  -t float
    	Terminate after the given number of seconds. (default -1)
  -v	Verbbose output. Print the content of all SQL queries, as well as the
    	PostgreSQL version.
  -version
    	Print version and exit.
```

### How It Works

sqlbench takes a list of SQL files and keeps executing them sequentially, measuring their execution times. By default the execution time is measured by prefixing the query with `EXPLAIN ANALYZE` and capturing the total `Execution Time` for it.

The query columns are ordered by mean execution time in ascending order, and the relative difference compared to the fastest query is shown in parentheses.

If the `-m client` flag is given, the time is measured using the wallclock time of sqlbench which includes network overhead.

Planning time is excluded by default, but can be included using the `-p` flag.

The filenames `init.sql` and `destroy.sql` are special, and are executed once before and after the benchmark respectively. They can be used to setup or teardown tables, indexes, etc..

## Tutorial

Let's say you want to compare three different queries for computing the running total of all numbers from 1 to 1000. Your first idea is to use a window function:

```sql
SELECT i, sum(i) OVER (ORDER BY i) AS sum
FROM generate_series(1, 1000) g(i);
```

Then you decide to get fancy and implement it as a recursive CTE:

```sql
WITH RECURSIVE sums AS (
	SELECT 1 AS i, 1 AS sum
	UNION
	SELECT i+1, sum+i FROM sums WHERE i <= 1000
)

SELECT * FROM sums;
```

And finally you become wise and remember that [9 year old Gauss](https://www.nctm.org/Publications/Teaching-Children-Mathematics/Blog/The-Story-of-Gauss/) could probably beat both approaches:

```sql
SELECT i, (i * (i + 1)) / 2 AS sum
FROM generate_series(1, 1000) g(i);
```

Now that you have your queries in `window.sql`, `recursive.sql`, `gauss.sql`, you want to summarize the performance differences for your colleagues. However, you know they're a pedantic bunch, and will ask you annoying questions such as:

- How many times did you run each query?
- Were you running other stuff on your laptop in the background?
- How can I reproduce this on my local machine?
- What version of PostgreSQL were you running on your local machine?
- Are you sure you're not just measuring the overhead of `EXPLAIN ANALYZE`?

This could normally be quite annoying to deal with, but luckily there is sqlbench. The command below lets you run your three queries 1000 times with `EXPLAIN ANALYZE` and report the statistics, the PostgreSQL version and even the SQL of your queries:

```
$ sqlbench -v -s -n 1000 examples/sum/*.sql | tee explain-bench.txt
```

```
         | gauss |    window     |   recursive    
---------+-------+---------------+----------------
  n      |  1000 |          1000 |          1000  
  min    |  0.35 | 1.31 (3.79x)  | 1.80 (5.22x)   
  max    |  4.18 | 23.76 (5.68x) | 11.41 (2.73x)  
  mean   |  0.50 | 1.94 (3.85x)  | 2.67 (5.30x)   
  stddev |  0.16 | 0.81 (4.93x)  | 0.63 (3.87x)   
  median |  0.53 | 2.02 (3.80x)  | 2.91 (5.49x)   
  p90    |  0.67 | 2.53 (3.80x)  | 3.41 (5.12x)   
  p95    |  0.68 | 2.57 (3.81x)  | 3.50 (5.18x)   

Stopping after 1000 iterations as requested.

postres version: PostgreSQL 11.6 on x86_64-apple-darwin16.7.0, compiled by Apple LLVM version 8.1.0 (clang-802.0.42), 64-bit
sqlbench -v -s -n 1000 examples/sum/gauss.sql examples/sum/recursive.sql examples/sum/window.sql

==> examples/sum/gauss.sql <==
SELECT i, (i * (i + 1)) / 2 AS sum
FROM generate_series(1, 1000) g(i);

==> examples/sum/window.sql <==
SELECT i, sum(i) OVER (ORDER BY i) AS sum
FROM generate_series(1, 1000) g(i);

==> examples/sum/recursive.sql <==
WITH RECURSIVE sums AS (
	SELECT 1 AS i, 1 AS sum
	UNION
	SELECT i+1, sum+i FROM sums WHERE i <= 1000
)

SELECT * FROM sums;
```

And finally, you can use the `-m client` flag to measure the query times without `EXPLAIN ANALYZE` to see if that had a significant overhead:

```
$ sqlbench -s -n 1000 -m client examples/sum/*.sql | tee client-bench.txt
```

```
         | gauss |    window    |  recursive    
---------+-------+--------------+---------------
  n      |  1000 |         1000 |         1000  
  min    |  0.66 | 1.44 (2.18x) | 2.03 (3.08x)  
  max    |  5.66 | 7.31 (1.29x) | 4.34 (0.77x)  
  mean   |  0.83 | 1.72 (2.08x) | 2.35 (2.83x)  
  stddev |  0.23 | 0.33 (1.41x) | 0.27 (1.18x)  
  median |  0.78 | 1.65 (2.11x) | 2.26 (2.89x)  
  p90    |  0.98 | 1.98 (2.03x) | 2.68 (2.75x)  
  p95    |  1.05 | 2.13 (2.03x) | 2.89 (2.76x)  

Stopping after 1000 iterations as requested.
```

Indeed, it appears that from the client's perspective the gauss query is a bit slower, while the others are a bit faster when measuring without `EXPLAIN ANALYZE`. Whether that's a rabbit hole worth exploring depends on you, but either way you now have a much better sense of the errors that might be contained in your measurements.

## Todos

Below are a few ideas for todos that I might implement at some point or would welcome as pull requests.

- [ ] Dynamically adjust unit between ms, s, etc.
- [ ] Support specifying benchmarks using a single YAML file.
- [ ] Support for other databases, e.g. MySQL.
- [ ] Capture query plans for each query, ideally one close to the median execution time.
- [ ] Provide an easy way to capture all inputs and outputs in a single tar.gz file or GitHub gist.
- [ ] Plot query times as a histogram (made a proof of concept for this, but didn't like it enough yet to release)
- [x] A flag to include planning time in `-m explain` mode.
- [x] A flag to use prepared queries in `-m client` mode.

## License

sqlbench is licensed under the MIT license.
