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
		go test -run='TestReport|TestCompleteness|TestRegressCompleteness' -v

# CPU + RAM comparison vs pg_query_go (cgo), GoSQLX, and go-pgquery (wasm).
# The cgo and wasm engines bundle conflicting libpg_query symbols, so they build
# as separate binaries.
CONC ?= 8
ITERS ?= 4
memcompare:
	cd comparison && go build -o /tmp/memprobe ./cmd/memprobe
	cd comparison && CGO_CFLAGS="-DHAVE_STRCHRNUL -Wno-error" \
		go build -o /tmp/memprobe-cgo ./cmd/memprobe-cgo
	@echo "== single-threaded =="
	@cd comparison && for e in pgparse gosqlx wasm; do \
		/tmp/memprobe -engine $$e -conc 1 -iters $(ITERS) -corpus testdata/regress; done
	@cd comparison && /tmp/memprobe-cgo -conc 1 -iters $(ITERS) -corpus testdata/regress
	@echo "== concurrent (conc=$(CONC)) =="
	@cd comparison && for e in pgparse gosqlx wasm; do \
		/tmp/memprobe -engine $$e -conc $(CONC) -iters $(ITERS) -corpus testdata/regress; done
	@cd comparison && /tmp/memprobe-cgo -conc $(CONC) -iters $(ITERS) -corpus testdata/regress

vet:
	go vet ./...

cover:
	go test -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -1

lint:
	gofmt -l .
