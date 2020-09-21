DROP TABLE IF EXISTS numbers;

CREATE TABLE numbers AS
SELECT * FROM generate_series(1, 10000) i;
CREATE INDEX ON numbers(i);

VACUUM ANALYZE numbers;
