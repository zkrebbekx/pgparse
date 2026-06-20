.PHONY: test bench vet cover lint all

all: vet test

test:
	go test ./...

bench:
	go test -run=^$$ -bench=. -benchmem ./...

# Head-to-head vs pg_query_go (cgo). Lives in its own module so the root stays
# cgo-free. CGO_CFLAGS works around a strchrnul redefinition with recent macOS
# SDKs; it is harmless elsewhere.
compare:
	cd comparison && CGO_CFLAGS="-DHAVE_STRCHRNUL -Wno-error" \
		go test -run=^$$ -bench=Corpus -benchmem -benchtime=2s
	cd comparison && CGO_CFLAGS="-DHAVE_STRCHRNUL -Wno-error" \
		go test -run=TestReport -v

vet:
	go vet ./...

cover:
	go test -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -1

lint:
	gofmt -l .
