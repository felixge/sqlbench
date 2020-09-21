WITH RECURSIVE sums AS (
	SELECT 1 AS i, 1 AS sum
	UNION
	SELECT i+1, sum+i FROM sums WHERE i <= 1000
)

SELECT * FROM sums;
