.PHONY: test bench vet cover lint all

all: vet test

test:
	go test ./...

bench:
	go test -run=^$$ -bench=. -benchmem ./...

vet:
	go vet ./...

cover:
	go test -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -1

lint:
	gofmt -l .
