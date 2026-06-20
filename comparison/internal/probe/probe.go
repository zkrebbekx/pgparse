// Package probe holds the shared corpus-loading, workload, and memory-reporting
// logic for the memprobe commands. The cgo (pg_query_go) and WebAssembly
// (go-pgquery) engines bundle conflicting libpg_query symbols, so each lives in
// its own binary; both call Run.
package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Run executes the measurement for one engine in the current process.
func Run(engine string, parse func(string) (any, error), conc, iters int, corpus string) {
	stmts := loadCorpus(corpus)

	rssBaseline := peakRSSMB()

	// Warm-up pass — captures startup/instantiation cost.
	for _, s := range stmts {
		_, _ = parse(s)
	}
	runtime.GC()
	rssAfterWarm := peakRSSMB()

	start := time.Now()
	var ops int64
	if conc <= 1 {
		for it := 0; it < iters; it++ {
			for _, s := range stmts {
				_, _ = parse(s)
			}
		}
		ops = int64(iters) * int64(len(stmts))
	} else {
		var wg sync.WaitGroup
		for w := 0; w < conc; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for it := 0; it < iters; it++ {
					for _, s := range stmts {
						_, _ = parse(s)
					}
				}
			}()
		}
		wg.Wait()
		ops = int64(conc) * int64(iters) * int64(len(stmts))
	}
	elapsed := time.Since(start)

	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rssPeak := peakRSSMB()

	nsPerStmt := float64(elapsed.Nanoseconds()) / float64(ops)
	fmt.Printf("%-9s conc=%-2d  %7.0f ns/stmt  goheap=%5.1fMB gosys=%5.1fMB  rss: startup=%4dMB peak=%4dMB\n",
		engine, conc, nsPerStmt,
		float64(m.HeapAlloc)/1e6, float64(m.Sys)/1e6,
		rssAfterWarm-rssBaseline, rssPeak)
}

func loadCorpus(dir string) []string {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no corpus at", dir)
		os.Exit(2)
	}
	sort.Strings(files)
	var out []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, s := range splitStatements(string(raw)) {
			s = strings.TrimSpace(s)
			if s == "" || strings.HasPrefix(s, "\\") {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

// splitStatements splits SQL on semicolons outside single-quoted strings and
// line comments — sufficient for the regression corpus.
func splitStatements(sql string) []string {
	var stmts []string
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch c {
		case '\'':
			b.WriteByte(c)
			for i++; i < len(sql); i++ {
				b.WriteByte(sql[i])
				if sql[i] == '\'' {
					break
				}
			}
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				for i < len(sql) && sql[i] != '\n' {
					b.WriteByte(sql[i])
					i++
				}
				if i < len(sql) {
					b.WriteByte(sql[i])
				}
			} else {
				b.WriteByte(c)
			}
		case ';':
			stmts = append(stmts, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		stmts = append(stmts, b.String())
	}
	return stmts
}

// peakRSSMB returns peak resident set size in MB. ru_maxrss is bytes on macOS,
// kilobytes on Linux.
func peakRSSMB() int {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	max := int64(ru.Maxrss)
	if runtime.GOOS == "linux" {
		return int(max / 1024)
	}
	return int(max / (1024 * 1024))
}
