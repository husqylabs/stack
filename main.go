package main

//go:generate go run ./tools/docgen

import (
	"fmt"
	"os"

	"github.com/husqylabs/stack/internal/cmd"
)

func main() {
	if err := cmd.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
