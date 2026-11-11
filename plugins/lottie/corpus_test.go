package lottie

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// TestCorpus loads every Lottie document under the directory that
// LOTTIE_CORPUS names, and draws frames across the composition of each
// that loads: none may panic, in Load or in Draw. With -v it logs the
// share that loads, in all and per top-level folder, and what stops the
// rest: the first error of each file, like errors grouped.
//
//	LOTTIE_CORPUS=path/to/samples go test -v -run Corpus ./plugins/lottie
func TestCorpus(t *testing.T) {
	dir := os.Getenv("LOTTIE_CORPUS")
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		t.Skip("LOTTIE_CORPUS does not name a directory of Lottie files")
	}
	type tally struct{ loaded, total int }
	folders := map[string]*tally{}
	type failure struct {
		n       int
		example string
	}
	failures := map[string]*failure{}
	var all tally
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !lottieDocument(data) {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		folder, _, nested := strings.Cut(rel, "/")
		if !nested {
			folder = "."
		}
		if folders[folder] == nil {
			folders[folder] = &tally{}
		}
		folders[folder].total++
		all.total++
		loadErr, panicked := loadAndDraw(data)
		switch {
		case panicked != "":
			t.Errorf("%s: %s", rel, panicked)
		case loadErr != nil:
			key := normalizeError(loadErr.Error())
			if failures[key] == nil {
				failures[key] = &failure{example: rel}
			}
			failures[key].n++
		default:
			folders[folder].loaded++
			all.loaded++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	share := func(c tally) string {
		return fmt.Sprintf("%d of %d (%.1f%%)", c.loaded, c.total, 100*float64(c.loaded)/float64(max(c.total, 1)))
	}
	t.Logf("loaded %s", share(all))
	for _, name := range slices.Sorted(maps.Keys(folders)) {
		t.Logf("  %-14s %s", name, share(*folders[name]))
	}
	keys := slices.Collect(maps.Keys(failures))
	slices.SortFunc(keys, func(a, b string) int {
		if d := failures[b].n - failures[a].n; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	t.Logf("first errors of the %d files that fail to load:", all.total-all.loaded)
	for _, k := range keys {
		t.Logf("  %4d  %s  (e.g. %s)", failures[k].n, k, failures[k].example)
	}
}

// lottieDocument reports whether data is a Lottie document: a JSON object
// with layers.
func lottieDocument(data []byte) bool {
	var doc map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	_, ok := doc["layers"]
	return ok
}

// loadAndDraw loads data and, if it loads, draws frames from its first to
// its last; it returns Load's error, and what panicked, where.
func loadAndDraw(data []byte) (loadErr error, panicked string) {
	stage := "Load"
	defer func() {
		if r := recover(); r != nil {
			panicked = fmt.Sprintf("%s panicked: %v\n%s", stage, r, debug.Stack())
		}
	}()
	a, err := Load(data)
	if err != nil {
		return err, ""
	}
	const frames = 6
	last := math.Nextafter32(a.outPoint, float32(math.Inf(-1)))
	for i := range frames {
		frame := min(a.inPoint+(a.outPoint-a.inPoint)*float32(i)/(frames-1), last)
		stage = fmt.Sprintf("Draw at frame %v", frame)
		renderBox(a, 96, 96, frame)
	}
	return nil, ""
}

var (
	corpusSnippet  = regexp.MustCompile(`(want a number, got ).*`)
	corpusQuoted   = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	corpusNumber   = regexp.MustCompile(`-?\b\d+(?:\.\d+)?(?:[eE][-+]?\d+)?\b`)
	corpusLocation = regexp.MustCompile(`(?:layer|nested group|item) N: `)
)

// normalizeError makes like errors alike: their numbers N, their quoted
// strings "…", as the JSON that a number's error quotes, and without where
// in the file they are, which layer, group or item.
func normalizeError(msg string) string {
	msg = corpusSnippet.ReplaceAllString(msg, "${1}…")
	msg = corpusQuoted.ReplaceAllString(msg, `"…"`)
	msg = corpusNumber.ReplaceAllString(msg, "N")
	return corpusLocation.ReplaceAllString(msg, "")
}
