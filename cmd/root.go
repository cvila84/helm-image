package cmd

import (
	"errors"
	"github.com/spf13/cobra"
	"io"
)

var globalUsage = `
This command searches for docker images referenced in a chart.
`

func NewRootCmd(out io.Writer, args []string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "helm image [CHART]",
		Short: "searches for docker images referenced in a chart",
		Long:  globalUsage,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errors.New("no arguments accepted")
			}
			return nil
		},
	}
	cmd.AddCommand(newLoadCmd(out))
	return cmd
}
