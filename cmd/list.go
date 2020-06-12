package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"io"
	"io/ioutil"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type dependency struct {
	Name   string
	Weight int
}

type listCmd struct {
	chartName  string
	namespace  string
	valuesOpts cliValues.Options
	debug      bool
}

var httpProvider = getter.Provider{
	Schemes: []string{"http", "https"},
	New:     getter.NewHTTPGetter,
}

func newListCmd(out io.Writer) *cobra.Command {
	l := &listCmd{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list docker images referenced in a chart",
		Long:  "list docker images referenced in a chart",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l.chartName = args[0]
			images, err := l.list()
			if err != nil {
				return err
			}
			for _, image := range images {
				fmt.Println(image)
			}
			return nil
		},
	}

	flags := cmd.Flags()

	flags.StringSliceVarP(&l.valuesOpts.ValueFiles, "values", "f", []string{}, "specify values in a YAML file or a URL (can specify multiple)")
	flags.StringArrayVar(&l.valuesOpts.Values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringArrayVar(&l.valuesOpts.StringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringArrayVar(&l.valuesOpts.FileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")

	// When called through helm, debug mode is transmitted through the HELM_DEBUG envvar
	helmDebug := os.Getenv("HELM_DEBUG")
	if helmDebug == "1" || strings.EqualFold(helmDebug, "true") || strings.EqualFold(helmDebug, "on") {
		l.debug = true
	}

	// When called through helm, namespace is transmitted through the HELM_NAMESPACE envvar
	namespace := os.Getenv("HELM_NAMESPACE")
	if len(namespace) > 0 {
		l.namespace = namespace
	} else {
		l.namespace = "default"
	}

	return cmd
}

func duration(d time.Duration) string {
	d = d.Truncate(time.Second)
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

func removeTempDir(tempDir string) {
	if err := os.RemoveAll(tempDir); err != nil {
		log.Printf("Warning: removing temporary directory: %s\n", err)
	}
}

func template(manifestDir string, namespace string, chartPath string, valueFiles []string, valuesSet []string, valuesSetString []string, valuesSetFile []string, debug bool) error {
	// Prepare parameters...
	var myargs = []string{"template", chartPath, "--disable-openapi-validation", "--output-dir", manifestDir, "--namespace", namespace}

	for _, v := range valuesSet {
		myargs = append(myargs, "--set")
		myargs = append(myargs, v)
	}
	for _, v := range valuesSetString {
		myargs = append(myargs, "--set-string")
		myargs = append(myargs, v)
	}
	for _, v := range valuesSetFile {
		myargs = append(myargs, "--set-file")
		myargs = append(myargs, v)
	}
	for _, v := range valueFiles {
		myargs = append(myargs, "-f")
		myargs = append(myargs, v)
	}

	// Run the upgrade command
	if debug {
		log.Printf("Running helm %s\n", myargs)
	}
	cmd := exec.Command("helm", myargs...)
	cmdOutput := &bytes.Buffer{}
	cmd.Stderr = os.Stderr
	cmd.Stdout = cmdOutput
	err := cmd.Run()
	output := cmdOutput.Bytes()
	if debug {
		log.Printf("Helm returned: \n%s\n", string(output))
	}
	if err != nil {
		return err
	}
	return nil
}

func mergeValues(chart *chart.Chart, valueOpts *values.Options) (chartutil.Values, error) {
	chartValues, err := chartutil.CoalesceValues(chart, chart.Values)
	if err != nil {
		return nil, fmt.Errorf("merging values with umbrella chart: %w", err)
	}
	providedValues, err := valueOpts.MergeValues(getter.Providers{httpProvider})
	if err != nil {
		return nil, fmt.Errorf("merging values from CLI flags: %w", err)
	}
	return mergeMaps(chartValues, providedValues), nil
}

func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

func getDependencies(chart *chart.Chart, values *chartutil.Values) ([]dependency, error) {
	// Build the list of all dependencies, and their key attributes
	dependencies := make([]dependency, len(chart.Metadata.Dependencies))
	for i, req := range chart.Metadata.Dependencies {
		// dependency name and alias
		if req.Alias == "" {
			dependencies[i].Name = req.Name
		} else {
			dependencies[i].Name = req.Alias
		}

		// Get weight of the dependency. If no weight is specified, setting it to 0
		weightJson, err := values.PathValue(dependencies[i].Name + ".weight")
		if err != nil {
			return nil, fmt.Errorf("computing weight value for sub-chart \"%s\": %w", dependencies[i].Name, err)
		}
		// Depending on the configuration of the json parser, integer can be returned either as Float64 or json.Number
		weight := 0
		if reflect.TypeOf(weightJson).String() == "json.Number" {
			w, err := weightJson.(json.Number).Int64()
			if err != nil {
				return nil, fmt.Errorf("computing weight value for sub-chart \"%s\": %w", dependencies[i].Name, err)
			}
			weight = int(w)
		} else if reflect.TypeOf(weightJson).String() == "float64" {
			weight = int(weightJson.(float64))
		} else {
			return nil, fmt.Errorf("computing weight value for sub-chart \"%s\", value shall be an integer", dependencies[i].Name)
		}
		if weight < 0 {
			return nil, fmt.Errorf("computing weight value for sub-chart \"%s\", value shall be positive or equal to zero", dependencies[i].Name)
		}
		dependencies[i].Weight = weight
	}
	return dependencies, nil
}

func addContainerImages(images map[string]struct{}, path string, debug bool) error {
	if debug {
		log.Printf("Parsing %s...\n", path)
	}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	manifest, _, err := scheme.Codecs.UniversalDeserializer().Decode(content, nil, nil)
	if err != nil {
		if debug {
			log.Printf("Warning: cannot parse %s: %s\n", path, err)
		}
		return nil
	}
	deployment, ok := manifest.(*v1.Deployment)
	if ok {
		if debug {
			log.Printf("Searching for images in deployment %s...\n", path)
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			images[container.Image] = struct{}{}
		}
		for _, container := range deployment.Spec.Template.Spec.InitContainers {
			images[container.Image] = struct{}{}
		}
	}
	statefulSet, ok := manifest.(*v1.StatefulSet)
	if ok {
		if debug {
			log.Printf("Searching for images in statefulset %s...\n", path)
		}
		for _, container := range statefulSet.Spec.Template.Spec.Containers {
			images[container.Image] = struct{}{}
		}
		for _, container := range statefulSet.Spec.Template.Spec.InitContainers {
			images[container.Image] = struct{}{}
		}
	}
	return nil
}

func parseManifests(images map[string]struct{}, path string, chartName string, debug bool) error {
	err := filepath.Walk(filepath.Join(path, chartName), func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, ".yaml") {
			err = addContainerImages(images, path, debug)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (l *listCmd) processChart(images map[string]struct{}, chartName string, valuesSet []string) error {
	tempDir, err := ioutil.TempDir("", "helm-image-")
	if err != nil {
		return fmt.Errorf("creating temporary directory to write rendered manifests: %w", err)
	}
	defer removeTempDir(tempDir)
	err = template(tempDir, l.namespace, l.chartName, l.valuesOpts.ValueFiles, valuesSet, l.valuesOpts.StringValues, l.valuesOpts.FileValues, l.debug)
	if err != nil {
		panic(err)
	}
	err = parseManifests(images, tempDir, chartName, l.debug)
	if err != nil {
		panic(err)
	}
	return nil
}

func (l *listCmd) list() ([]string, error) {
	images := map[string]struct{}{}

	// TODO manage remote charts
	chart, err := loader.Load(l.chartName)
	if err != nil {
		panic(err)
	}
	mergedValues, err := mergeValues(chart, &l.valuesOpts)
	if err != nil {
		panic(err)
	}
	deps, err := getDependencies(chart, &mergedValues)
	if err != nil {
		panic(err)
	}

	start := time.Now()

	if len(deps) > 0 {
		maxWeight := 0
		for _, dep := range deps {
			if dep.Weight > maxWeight {
				maxWeight = dep.Weight
			}
		}
		for w := 0; w <= maxWeight; w++ {
			depValuesSet := ""
			for _, dep := range deps {
				if dep.Weight == w {
					depValuesSet = depValuesSet + dep.Name + ".enabled=true,"
				}
			}
			if len(depValuesSet) > 0 {
				var valuesSet []string
				valuesSet = append(valuesSet, l.valuesOpts.Values...)
				valuesSet = append(valuesSet, depValuesSet)
				err = l.processChart(images, chart.Name(), valuesSet)
				if err != nil {
					return nil, err
				}
			}
		}
	} else {
		err = l.processChart(images, chart.Name(), l.valuesOpts.Values)
		if err != nil {
			return nil, err
		}
	}

	spent := duration(time.Since(start))
	if l.debug {
		log.Printf("Chart parsed in %s\n", spent)
	}

	var ret []string
	for image, _ := range images {
		ret = append(ret, image)
	}

	return ret, nil
}
