package pgparse

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// loadTPCH returns the sanitized TPC-H corpus keyed by file name.
func loadTPCH(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("testdata/tpch/*.sql")
	if err != nil || len(files) == 0 {
		t.Skipf("no TPC-H corpus: %v", err)
	}
	sort.Strings(files)
	out := make(map[string]string, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(f)] = string(b)
	}
	return out
}

func TestTPCHCoverage(t *testing.T) {
	corpus := loadTPCH(t)
	var ok, fail int
	for _, name := range sortedKeys(corpus) {
		if _, err := Parse(corpus[name]); err != nil {
			fail++
			t.Logf("FAIL %s: %v", name, err)
		} else {
			ok++
		}
	}
	t.Logf("TPC-H coverage: %d/%d parsed", ok, ok+fail)
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
