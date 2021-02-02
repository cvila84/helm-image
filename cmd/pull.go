package cmd

import (
	"fmt"
	"github.com/gemalto/helm-image/internal/docker"
	"github.com/spf13/cobra"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"io"
	"os"
	"strings"
)

type pullCmd struct {
	chartName  string
	namespace  string
	excludes   []string
	auths      []string
	valuesOpts cliValues.Options
	helmPath   string
	verbose    bool
	debug      bool
}

func newPullCmd(out io.Writer) *cobra.Command {
	p := &pullCmd{}

	cmd := &cobra.Command{
		Use:          "pull",
		Short:        "pull docker images referenced in a chart",
		Long:         "pull docker images referenced in a chart",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p.chartName = args[0]
			return p.pull()
		},
	}

	flags := cmd.Flags()

	flags.StringSliceVarP(&p.auths, "auth", "a", []string{}, "specify private registries which need authentication during pull")
	flags.StringSliceVarP(&p.excludes, "exclude", "x", []string{}, "specify docker images to be excluded from pulls")
	flags.StringSliceVarP(&p.valuesOpts.ValueFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	flags.StringArrayVar(&p.valuesOpts.Values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringArrayVar(&p.valuesOpts.StringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringArrayVar(&p.valuesOpts.FileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	flags.BoolVarP(&p.verbose, "verbose", "v", false, "enable verbose output")

	// When called through helm, helm path is transmitted through the HELM_BIN envvar
	p.helmPath = os.Getenv("HELM_BIN")

	// When called through helm, debug mode is transmitted through the HELM_DEBUG envvar
	helmDebug := os.Getenv("HELM_DEBUG")
	if helmDebug == "1" || strings.EqualFold(helmDebug, "true") || strings.EqualFold(helmDebug, "on") {
		p.debug = true
	}

	// When called through helm, namespace is transmitted through the HELM_NAMESPACE envvar
	namespace := os.Getenv("HELM_NAMESPACE")
	if len(namespace) > 0 {
		p.namespace = namespace
	} else {
		p.namespace = "default"
	}

	return cmd
}

func (p *pullCmd) pull() error {
	l := &listCmd{
		chartName:  p.chartName,
		namespace:  p.namespace,
		valuesOpts: p.valuesOpts,
		helmPath:   p.helmPath,
		debug:      p.debug,
		verbose:    p.verbose,
	}
	images, err := l.list()
	includedImagesMap := map[string]struct{}{}
	for _, image := range images {
		includedImagesMap[image] = struct{}{}
		for _, excludedImage := range p.excludes {
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
	//client, err := containerd.ClientWithAddress(os.Getenv("DOCKER_HOST"), l.debug)
	//if err != nil {
	//	return err
	//}
	//ctx := namespaces.WithNamespace(context.Background(), "default")
	//for _, auth := range p.auths {
	//	registry.AddAuthRegistry(auth)
	//}
	for _, image := range includedImages {
		if p.verbose {
			fmt.Printf("Pulling %s...\n", image)
		}
		err = docker.Pull(image, l.debug)
		//err = containerd.PullImage(ctx, client, registry.ConsoleCredentials, image, l.debug)
		if err != nil {
			return err
		}
	}
	return nil
}
