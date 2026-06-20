package comparison

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v5"
	"github.com/zkrebbekx/pgparse"
)

var nearRe = regexp.MustCompile(` near .*$`)

// TestRegressFailures buckets the statements that pg_query_go accepts but
// pgparse rejects, by a normalised error reason, to prioritise grammar work.
func TestRegressFailures(t *testing.T) {
	files, _ := filepath.Glob("testdata/regress/*.sql")
	if len(files) == 0 {
		t.Skip("no corpus")
	}
	reason := map[string]int{}
	sample := map[string]string{}
	var fails int
	for _, file := range files {
		raw, _ := os.ReadFile(file)
		stmts, err := pgquery.SplitWithScanner(string(raw), true)
		if err != nil {
			continue
		}
		for _, stmt := range stmts {
			s := strings.TrimSpace(stmt)
			if s == "" || strings.HasPrefix(s, "\\") {
				continue
			}
			if !safeParse(pgParse, s) {
				continue
			}
			_, perr := pgparse.Parse(s)
			if perr == nil {
				continue
			}
			fails++
			key := nearRe.ReplaceAllString(perr.Error(), "")
			key = regexp.MustCompile(`offset \d+`).ReplaceAllString(key, "offset N")
			reason[key]++
			if sample[key] == "" {
				one := s
				if len(one) > 90 {
					one = one[:90]
				}
				sample[key] = strings.Join(strings.Fields(one), " ")
			}
		}
	}
	type kv struct {
		k string
		n int
	}
	var list []kv
	for k, n := range reason {
		list = append(list, kv{k, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	t.Logf("total failing statements: %d", fails)
	for i, e := range list {
		if i >= 30 {
			break
		}
		t.Logf("%4d  %s\n        e.g. %s", e.n, e.k, sample[e.k])
	}
	fmt.Printf("regress failures bucketed: %d total\n", fails)
}
