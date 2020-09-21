SELECT i, (i * (i + 1)) / 2 AS sum
FROM generate_series(1, 1000) g(i);
