package cmd

import (
	"context"
	"fmt"
	"github.com/containerd/containerd/namespaces"
	"github.com/gemalto/helm-image/internal/containerd"
	"github.com/spf13/cobra"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

type cacheCmd struct {
	debug   bool
	verbose bool
}

func newCacheListCmd(out io.Writer, c *cacheCmd) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "list docker images from local cache",
		Long:         "list docker images from local cache",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.list()
		},
	}
}

func newCacheCleanCmd(out io.Writer, c *cacheCmd) *cobra.Command {
	return &cobra.Command{
		Use:          "clean",
		Short:        "remove all docker images from local cache",
		Long:         "remove all docker images from local cache",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.clean()
		},
	}
}

func newCacheCmd(out io.Writer) *cobra.Command {
	c := &cacheCmd{}

	cmd := &cobra.Command{
		Use:          "cache",
		Short:        "manage local cache of docker images",
		Long:         "manage local cache of docker images",
		SilenceUsage: true,
	}

	cmd.AddCommand(
		newCacheListCmd(out, c),
		newCacheCleanCmd(out, c),
	)

	cmd.PersistentFlags().BoolVarP(&c.verbose, "verbose", "v", false, "enable verbose output")

	// When called through helm, debug mode is transmitted through the HELM_DEBUG envvar
	helmDebug := os.Getenv("HELM_DEBUG")
	if helmDebug == "1" || strings.EqualFold(helmDebug, "true") || strings.EqualFold(helmDebug, "on") {
		c.debug = true
	}

	return cmd
}

func (c *cacheCmd) list() error {
	serverStarted := make(chan bool)
	serverKill := make(chan bool)
	serverKilled := make(chan bool)
	go containerd.Server(serverStarted, serverKill, serverKilled, c.debug)
	if !<-serverStarted {
		return fmt.Errorf("cannot start containerd server")
	}
	interrupt := make(chan os.Signal)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupt
		if c.debug {
			log.Println("Sending interrupt signal to containerd server...")
		}
		serverKill <- true
		<-serverKilled
	}()
	client, err := containerd.Client(c.debug)
	if err != nil {
		if c.debug {
			log.Println("Sending interrupt signal to containerd server...")
		}
		serverKill <- true
		<-serverKilled
		return err
	}
	ctx := namespaces.WithNamespace(context.Background(), "default")
	err = containerd.ListImages(ctx, client)
	if err != nil {
		if c.debug {
			log.Println("Sending interrupt signal to containerd server...")
		}
		serverKill <- true
		<-serverKilled
		return err
	}
	if c.debug {
		log.Println("Sending interrupt signal to containerd server...")
	}
	serverKill <- true
	<-serverKilled
	return nil
}

func (c *cacheCmd) clean() error {
	return containerd.DeleteContainerdDirectories()
}
