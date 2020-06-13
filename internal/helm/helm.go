package helm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"log"
	"os"
	"os/exec"
	"reflect"
)

type Dependency struct {
	Name   string
	Weight int
}

var httpProvider = getter.Provider{
	Schemes: []string{"http", "https"},
	New:     getter.NewHTTPGetter,
}

func Template(manifestDir string, namespace string, chartPath string, valueFiles []string, valuesSet []string, valuesSetString []string, valuesSetFile []string, debug bool) error {
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

func MergeValues(chart *chart.Chart, valueOpts *values.Options) (chartutil.Values, error) {
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

func GetDependencies(chart *chart.Chart, values *chartutil.Values) ([]Dependency, error) {
	// Build the list of all dependencies, and their key attributes
	dependencies := make([]Dependency, len(chart.Metadata.Dependencies))
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
