// Command docgen writes the CLI documentation generated from the live cobra
// command tree, so the docs can never drift from the code:
//
//	docs/commands.md   — full command reference
//	docs/cheatsheet.md — one-page quick reference
//
// Run via `go generate ./...` (see the directive in main.go). The rendering lives
// in internal/docs, shared with the drift test that fails if these are stale.
package main

import (
	"fmt"
	"os"

	"github.com/husqylabs/stack/internal/cmd"
	"github.com/husqylabs/stack/internal/docs"
)

func main() {
	root := cmd.NewRoot()
	if err := os.MkdirAll("docs", 0o755); err != nil {
		fail(err)
	}
	write("docs/index.md", docs.Index(root))
	write("docs/commands.md", docs.Reference(root))
	write("docs/cheatsheet.md", docs.Cheatsheet(root))
	write("mkdocs.yml", docs.MkdocsConfig())
	fmt.Println("generated docs/index.md, docs/commands.md, docs/cheatsheet.md, mkdocs.yml")
}

func write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "docgen:", err)
	os.Exit(1)
}
