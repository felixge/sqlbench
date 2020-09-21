SELECT i, sum(i) OVER (ORDER BY i) AS sum
FROM generate_series(1, 1000) g(i);
