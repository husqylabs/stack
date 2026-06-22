package docs_test

import (
	"os"
	"testing"

	"github.com/husqylabs/stack/internal/cmd"
	"github.com/husqylabs/stack/internal/docs"
)

// TestGeneratedDocsAreUpToDate fails if the committed docs differ from what the
// generator would produce now — i.e. someone changed a command but forgot to run
// `go generate ./...`. Test cwd is this package dir, so docs/ is two levels up.
func TestGeneratedDocsAreUpToDate(t *testing.T) {
	root := cmd.NewRoot()
	cases := []struct {
		path string
		want string
	}{
		{"../../docs/commands.md", docs.Reference(root)},
		{"../../docs/cheatsheet.md", docs.Cheatsheet(root)},
	}
	for _, tc := range cases {
		got, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s is stale — run `go generate ./...` and commit the result", tc.path)
		}
	}
}
