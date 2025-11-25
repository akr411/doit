package main

import (
	"os"

	"github.com/akr411/doit/internal/ui"
)

func main() {
	if err := Execute(); err != nil {
		ui.PrintError("Error: %v", err)
		os.Exit(1)
	}
}
