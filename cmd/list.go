package cmd

import (
	"fmt"
	"github.com/gemalto/helm-image/internal/helm"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"io"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type imagesList struct {
	images map[string]struct{}
	mu     sync.Mutex
}

func newImagesList() *imagesList {
	return &imagesList{
		images: map[string]struct{}{},
	}
}

func (l *imagesList) add(image string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.images[image] = struct{}{}
}

func (l *imagesList) get() []string {
	var images []string
	for image, _ := range l.images {
		images = append(images, image)
	}
	return images
}

func (l *imagesList) contains(image string) bool {
	if _, ok := l.images[image]; ok {
		return true
	} else {
		return false
	}
}

type taskErrors struct {
	errors []error
	mu     sync.Mutex
}

func newTaskErrors() *taskErrors {
	return &taskErrors{
		errors: []error{},
	}
}

func (e *taskErrors) add(error error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errors = append(e.errors, error)
}

func (e *taskErrors) get() []error {
	return e.errors
}

type processChartInfo struct {
	images    *imagesList
	chartName string
	valuesSet []string
}

type listCmd struct {
	chartName  string
	namespace  string
	valuesOpts cliValues.Options
	helmPath   string
	verbose    bool
	debug      bool
}

func newListCmd(out io.Writer) *cobra.Command {
	l := &listCmd{}

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "list docker images referenced in a chart",
		Long:         "list docker images referenced in a chart",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
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
	flags.BoolVarP(&l.verbose, "verbose", "v", false, "enable verbose output")

	// When called through helm, helm path is transmitted through the HELM_BIN envvar
	l.helmPath = os.Getenv("HELM_BIN")

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

func addContainerImages(images *imagesList, path string, verbose bool, debug bool) error {
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
	deployment, ok := manifest.(*appsv1.Deployment)
	if ok {
		if debug {
			log.Printf("Searching for deployment images in %s...\n", path)
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
		for _, container := range deployment.Spec.Template.Spec.InitContainers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
	}
	statefulSet, ok := manifest.(*appsv1.StatefulSet)
	if ok {
		if debug {
			log.Printf("Searching for statefulset images in %s...\n", path)
		}
		for _, container := range statefulSet.Spec.Template.Spec.Containers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
		for _, container := range statefulSet.Spec.Template.Spec.InitContainers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
	}
	jobs, ok := manifest.(*batchv1.Job)
	if ok {
		if debug {
			log.Printf("Searching for job images in %s...\n", path)
		}
		for _, container := range jobs.Spec.Template.Spec.Containers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
		for _, container := range jobs.Spec.Template.Spec.InitContainers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
	}
	cronJobs, ok := manifest.(*batchv1.CronJob)
	if ok {
		if debug {
			log.Printf("Searching for cron job images in %s...\n", path)
		}
		for _, container := range cronJobs.Spec.JobTemplate.Spec.Template.Spec.Containers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
		for _, container := range cronJobs.Spec.JobTemplate.Spec.Template.Spec.InitContainers {
			if !images.contains(container.Image) {
				if verbose {
					fmt.Printf("Found %s\n", container.Image)
				}
				images.add(container.Image)
			} else if debug {
				fmt.Printf("Ignoring %s\n", container.Image)
			}
		}
	}
	return nil
}

func parseManifests(images *imagesList, path string, chartName string, verbose bool, debug bool) error {
	err := filepath.Walk(filepath.Join(path, chartName), func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, ".yaml") {
			err = addContainerImages(images, path, verbose, debug)
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

func (l *listCmd) processChart(images *imagesList, chartName string, valuesSet []string) error {
	tempDir, err := ioutil.TempDir("", "helm-image-")
	if err != nil {
		return fmt.Errorf("creating temporary directory to write rendered manifests: %w", err)
	}
	defer removeTempDir(tempDir)
	if l.verbose {
		fmt.Printf("Rendering %s with %v...\n", l.chartName, valuesSet)
	}
	err = helm.Template(l.helmPath, tempDir, l.namespace, l.chartName, l.valuesOpts.ValueFiles, valuesSet, l.valuesOpts.StringValues, l.valuesOpts.FileValues, l.debug)
	if err != nil {
		return err
	}
	err = parseManifests(images, tempDir, chartName, l.verbose, l.debug)
	if err != nil {
		return err
	}
	return nil
}

func (l *listCmd) list() ([]string, error) {
	images := newImagesList()

	if l.verbose {
		fmt.Printf("Loading chart %s...\n", l.chartName)
	}
	// TODO manage remote charts
	chart, err := loader.Load(l.chartName)
	if err != nil {
		return nil, err
	}
	if l.debug {
		log.Println("Merging values...")
	}
	mergedValues, err := helm.MergeValues(chart, &l.valuesOpts)
	if err != nil {
		return nil, err
	}
	if l.debug {
		log.Println("Loading dependencies...")
	}
	deps, err := helm.GetDependencies(chart, &mergedValues)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	if len(deps) > 0 {
		var wg sync.WaitGroup
		tasks := make(chan processChartInfo)
		taskErrors := newTaskErrors()
		for i := 0; i < runtime.NumCPU(); i++ {
			wg.Add(1)
			go func(tasks chan processChartInfo, wg *sync.WaitGroup) {
				defer wg.Done()
				for task := range tasks {
					if l.debug {
						log.Printf("Starting task for %s with %v\n", task.chartName, task.valuesSet)
					}
					err := l.processChart(task.images, task.chartName, task.valuesSet)
					if err != nil {
						taskErrors.add(err)
						if l.debug {
							log.Printf("End with error of task for %s with %v\n", task.chartName, task.valuesSet)
						}
						break
					}
					if l.debug {
						log.Printf("End without error of task for %s with %v\n", task.chartName, task.valuesSet)
					}
				}
			}(tasks, &wg)
		}
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
				tasks <- processChartInfo{
					images:    images,
					chartName: chart.Name(),
					valuesSet: valuesSet,
				}
			}
		}
		close(tasks)
		wg.Wait()
		errors := taskErrors.get()
		if len(errors) > 0 {
			for _, err := range errors {
				log.Printf("Error: %s\n", err)
			}
			return nil, fmt.Errorf("processing one of the sub-chart")
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

	return images.get(), nil
}
