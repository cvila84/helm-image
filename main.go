package main

import (
	"github.com/spf13/cobra"
	"os"
)

type imageCmd struct {
	chartName string
}

var globalUsage = `
This command searches for docker images referenced in a chart.
`

var version = "SNAPSHOT"

func newImageCmd(args []string) *cobra.Command {
	p := &imageCmd{}
	return nil
}

func main() {
	cmd := newImageCmd(os.Args[1:])
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
