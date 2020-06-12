package main

import (
	"github.com/gemalto/helm-image/cmd"
	"os"
)

func main() {
	rootCmd := cmd.NewRootCmd(os.Stdout, os.Args[1:])
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
