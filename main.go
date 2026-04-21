package main

import (
	"fmt"
	"os"

	"github.com/anttti/j/cmd"
)

func main() {
	root := cmd.NewRootCmd(os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "jira:", err)
		os.Exit(1)
	}
}
