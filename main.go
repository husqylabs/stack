package main

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
