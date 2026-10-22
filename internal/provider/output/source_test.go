package output

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// collapseIdiom joins the fields of a string with single spaces, which turns a
// listing, a table or a stack trace into one line of words.
const collapseIdiom = "strings.Join(strings.Fields("

// TestNoAdapterCollapsesWhitespace is PLAN 0208 P1 step 5 (MADR 0208 D11): no
// production file under internal/provider collapses whitespace. Agent text is
// bounded with Clip, and a one-line field is taken with Line.
func TestNoAdapterCollapsesWhitespace(t *testing.T) {
	root := filepath.Join("..")
	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, collapseIdiom) {
				hits = append(hits, filepath.ToSlash(path)+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		t.Errorf("collapses whitespace: %s", h)
	}
}
