select
	sum(l_extendedprice * l_discount) as revenue
from
	lineitem
where
	l_shipdate >= date '1'
	and l_shipdate < date '1' + interval '1' year
	and l_discount between 1 - 0.01 and 1 + 0.01
	and l_quantity < 1
LIMIT 1;
