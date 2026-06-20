module github.com/zkrebbekx/pgparse/comparison

go 1.20

require (
	github.com/pganalyze/pg_query_go/v5 v5.1.0
	github.com/zkrebbekx/pgparse v0.0.0
)

require google.golang.org/protobuf v1.31.0 // indirect

replace github.com/zkrebbekx/pgparse => ../
