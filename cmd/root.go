package cmd

import (
	"github.com/spf13/cobra"
	"io"
)

func NewRootCmd(out io.Writer, args []string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Short: "tools for docker images referenced in a chart",
		Long:  "tools for docker images referenced in a chart",
	}
	cmd.AddCommand(
		newListCmd(out),
		newSaveCmd(out),
		newPullCmd(out),
		newCacheCmd(out),
	)
	return cmd
}
