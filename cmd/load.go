package cmd

import (
	"github.com/spf13/cobra"
	"io"
)

type loadCmd struct {
	chartName string
}

func newLoadCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "helm image [CHART]",
		Short: "searches for docker images referenced in a chart",
		Long:  globalUsage,
		Run:   runLoadCmd,
	}
	return cmd
}

func runLoadCmd(cmd *cobra.Command, args []string) {
	//	p := &loadCmd{}
}
