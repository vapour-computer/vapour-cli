package main

import (
	"fmt"
	"os"

	"github.com/vapour-computer/vapour-cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, cmd.ErrorMessage(err))
		os.Exit(1)
	}
}
