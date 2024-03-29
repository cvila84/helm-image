package cmd

import (
	"context"
	"fmt"
	"github.com/containerd/containerd/namespaces"
	"github.com/gemalto/helm-image/internal/containerd"
	"github.com/gemalto/helm-image/internal/credentials"
	"github.com/gemalto/helm-image/internal/registry"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

type saveCmd struct {
	chartName  string
	outputFile string
	namespace  string
	excludes   []string
	auths      []string
	valuesOpts cliValues.Options
	helmPath   string
	verbose    bool
	debug      bool
}

func newSaveCmd(out io.Writer) *cobra.Command {
	s := &saveCmd{}

	cmd := &cobra.Command{
		Use:          "save",
		Short:        "save in a file docker images referenced in a chart",
		Long:         "save in a file docker images referenced in a chart",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s.chartName = args[0]
			return s.save()
		},
	}

	flags := cmd.Flags()

	flags.StringSliceVarP(&s.auths, "auth", "a", []string{}, "specify private registries which need authentication during pull")
	flags.StringSliceVarP(&s.excludes, "exclude", "x", []string{}, "specify docker images to be excluded from pulls")
	flags.StringSliceVarP(&s.valuesOpts.ValueFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	flags.StringArrayVar(&s.valuesOpts.Values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringArrayVar(&s.valuesOpts.StringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringArrayVar(&s.valuesOpts.FileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	flags.BoolVarP(&s.verbose, "verbose", "v", false, "enable verbose output")
	flags.StringVarP(&s.outputFile, "output", "o", "", "image file name")

	// When called through helm, helm path is transmitted through the HELM_BIN envvar
	s.helmPath = os.Getenv("HELM_BIN")

	// When called through helm, debug mode is transmitted through the HELM_DEBUG envvar
	helmDebug := os.Getenv("HELM_DEBUG")
	if helmDebug == "1" || strings.EqualFold(helmDebug, "true") || strings.EqualFold(helmDebug, "on") {
		s.debug = true
	}

	// When called through helm, namespace is transmitted through the HELM_NAMESPACE envvar
	namespace := os.Getenv("HELM_NAMESPACE")
	if len(namespace) > 0 {
		s.namespace = namespace
	} else {
		s.namespace = "default"
	}

	return cmd
}

func (s *saveCmd) save() error {
	l := &listCmd{
		chartName:  s.chartName,
		namespace:  s.namespace,
		valuesOpts: s.valuesOpts,
		helmPath:   s.helmPath,
		debug:      s.debug,
		verbose:    s.verbose,
	}
	images, err := l.list()
	includedImagesMap := map[string]struct{}{}
	for _, image := range images {
		includedImagesMap[image] = struct{}{}
		for _, excludedImage := range s.excludes {
			if image == excludedImage {
				delete(includedImagesMap, image)
			}
		}
	}
	var includedImages []string
	for image := range includedImagesMap {
		includedImages = append(includedImages, image)
	}
	if err != nil {
		return err
	}
	// TODO manage remote charts
	chart, err := loader.Load(l.chartName)
	if err != nil {
		return err
	}
	serverStarted := make(chan bool)
	serverKill := make(chan bool)
	serverKilled := make(chan bool)
	go containerd.Server(serverStarted, serverKill, serverKilled, l.debug)
	if !<-serverStarted {
		return fmt.Errorf("cannot start containerd server")
	}
	interrupt := make(chan os.Signal)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupt
		if l.debug {
			log.Println("Sending interrupt signal to containerd server...")
		}
		serverKill <- true
		<-serverKilled
	}()
	client, err := containerd.Client(l.debug)
	if err != nil {
		if l.debug {
			log.Println("Sending interrupt signal to containerd server...")
		}
		serverKill <- true
		<-serverKilled
		return err
	}
	ctx := namespaces.WithNamespace(context.Background(), "default")
	for _, auth := range s.auths {
		login, password, err := credentials.GetAuth(auth)
		if err != nil && l.debug {
			log.Printf("Warning: cannot get authentication information: %s\n", err)
		}
		registry.AddAuthRegistry(auth, login, password)
	}
	for _, image := range includedImages {
		err = containerd.PullImage(ctx, client, registry.ConsoleCredentials, image, l.debug)
		if err != nil {
			if l.debug {
				log.Println("Sending interrupt signal to containerd server...")
			}
			serverKill <- true
			<-serverKilled
			return err
		}
	}
	if len(s.outputFile) == 0 {
		s.outputFile = chart.Name() + ".tar"
	}
	err = containerd.SaveImages(ctx, client, includedImages, s.outputFile)
	if err != nil {
		if l.debug {
			log.Println("Sending interrupt signal to containerd server...")
		}
		serverKill <- true
		<-serverKilled
		return err
	}
	if l.debug {
		log.Println("Sending interrupt signal to containerd server...")
	}
	serverKill <- true
	<-serverKilled
	return nil
}
