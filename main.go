package main

import (
	"os"

	"github.com/vaarde/mise/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(cmd.ExitCode(err))
	}
}
