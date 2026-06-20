module github.com/zkrebbekx/pgparse/comparison

go 1.26.1

require (
	github.com/ajitpratap0/GoSQLX v1.14.0
	github.com/pganalyze/pg_query_go/v5 v5.1.0
	github.com/zkrebbekx/pgparse v0.0.0
)

require google.golang.org/protobuf v1.36.11 // indirect

replace github.com/zkrebbekx/pgparse => ../
