package containerd

import (
	"bufio"
	"context"
	"fmt"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/images/archive"
	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/docker/distribution/reference"
	"github.com/gemalto/helm-image/internal/registry"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var containerdConfig = `
version = 2
plugin_dir = ""
disabled_plugins = [ "io.containerd.grpc.v1.cri" ]
required_plugins = []
oom_score = 0

[grpc]
  address = "\\\\.\\pipe\\containerd-containerd"
  tcp_address = ""
  tcp_tls_cert = ""
  tcp_tls_key = ""
  uid = 0
  gid = 0
  max_recv_message_size = 16777216
  max_send_message_size = 16777216

[ttrpc]
  address = ""
  uid = 0
  gid = 0

[debug]
  address = ""
  uid = 0
  gid = 0
  level = ""

[metrics]
  address = ""
  grpc_histogram = false

[cgroup]
  path = ""

[timeouts]
  "io.containerd.timeout.shim.cleanup" = "5s"
  "io.containerd.timeout.shim.load" = "5s"
  "io.containerd.timeout.shim.shutdown" = "3s"
  "io.containerd.timeout.task.state" = "2s"

[plugins]
  [plugins."io.containerd.gc.v1.scheduler"]
    pause_threshold = 0.02
    deletion_threshold = 0
    mutation_threshold = 100
    schedule_delay = "0s"
    startup_delay = "100ms"
  [plugins."io.containerd.internal.v1.opt"]
    path = "C:\\Users\\cvila\\.containerd\\root\\opt"
  [plugins."io.containerd.internal.v1.restart"]
    interval = "10s"
  [plugins."io.containerd.metadata.v1.bolt"]
    content_sharing_policy = "shared"
  [plugins."io.containerd.runtime.v2.task"]
    platforms = ["windows/amd64", "linux/amd64"]
  [plugins."io.containerd.service.v1.diff-service"]
    default = ["windows", "windows-lcow"]
`

var fileLog *log.Logger

type jobs struct {
	name     string
	added    map[digest.Digest]struct{}
	descs    []ocispec.Descriptor
	mu       sync.Mutex
	resolved bool
}

type imagePart struct {
	ref       string
	status    string
	offset    int64
	total     int64
	startedAt time.Time
	updatedAt time.Time
}

type imageParts struct {
	list          map[string]imagePart
	mu            sync.Mutex
	statusChanged func(string, imagePart)
	offsetChanged func(string, imagePart)
}

//type imageBars struct {
//	barManager *mpb.Progress
//	bar        map[string]*mpb.Bar
//	mu         sync.Mutex
//}

func (j *jobs) add(desc ocispec.Descriptor) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.resolved = true

	if _, ok := j.added[desc.Digest]; ok {
		return
	}
	j.descs = append(j.descs, desc)
	j.added[desc.Digest] = struct{}{}
}

func (j *jobs) jobs() []ocispec.Descriptor {
	j.mu.Lock()
	defer j.mu.Unlock()

	var descs []ocispec.Descriptor
	return append(descs, j.descs...)
}

func (j *jobs) isResolved() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.resolved
}

func (p *imageParts) get(name string) (imagePart, bool) {
	status, ok := p.list[name]
	return status, ok
}

func (p *imageParts) set(name string, status string, part imagePart) {
	p.mu.Lock()
	defer p.mu.Unlock()
	part.ref = name
	part.status = status
	if _, ok := p.list[name]; !ok {
		fileLog.Printf("imagePart created: %+v\n", part)
	} else {
		fileLog.Printf("imagePart updated: %+v\n", part)
		if p.list[name].status != part.status && p.statusChanged != nil {
			p.statusChanged(name, part)
		}
		if p.list[name].offset != part.offset && p.offsetChanged != nil {
			p.offsetChanged(name, part)
		}
	}
	p.list[name] = part
}

//func (b *imageBars) close() {
//	for name, bar := range b.bar {
//		fileLog.Printf("Bar %s: current()=%d, completed=%t\n", name, bar.Current(), bar.Completed())
//		bar.Abort(false)
//	}
//}

//func (b* imageBars) update(name string, part imagePart) {
//	b.mu.Lock()
//	defer b.mu.Unlock()
//	if _, ok := b.bar[name]; !ok {
//		if part.status == "downloading" {
//			fileLog.Printf("Bar %s created: total=%d\n", name, part.total)
//			nameParts := strings.Split(name, ":")
//			var displayName string
//			if len(nameParts) == 2 {
//				displayName = nameParts[1][:12]
//			} else {
//				displayName = name
//			}
//			b.bar[name] = b.barManager.AddBar(part.total,
//				mpb.PrependDecorators(
//					decor.Name(displayName, decor.WC{W: 12}), // , C: decor.DSyncWidthR
//					decor.CountersKiloByte( "% d / % d", decor.WC{W: 16}), // , C: decor.DSyncWidth
//				),
//				mpb.BarWidth(20),
//				mpb.AppendDecorators(decor.Percentage()),
//			)
//		}
//	} else {
//		switch part.status {
//		case "downloading", "done":
//			fileLog.Printf("Bar %s updated: setCurrent(%d)\n", name, part.offset)
//			b.bar[name].SetCurrent(part.offset)
//		}
//	}
//}

func newJobs(name string) *jobs {
	return &jobs{
		name:  name,
		added: map[digest.Digest]struct{}{},
	}
}

func newImageParts(statusChanged func(string, imagePart), offsetChanged func(string, imagePart)) *imageParts {
	return &imageParts{
		list:          map[string]imagePart{},
		statusChanged: statusChanged,
		offsetChanged: offsetChanged,
	}
}

//func newImageBars(barManager *mpb.Progress) *imageBars {
//	return &imageBars{
//		barManager: barManager,
//		bar:        map[string]*mpb.Bar{},
//	}
//}

func displayPart(name string, part imagePart) {
	nameParts := strings.Split(name, ":")
	var displayName string
	if len(nameParts) == 2 && len(nameParts[1]) == 64 {
		displayName = nameParts[1][:12]
	} else {
		displayName = name
	}
	if part.status == "downloading" {
		fmt.Printf("%s: Pulling fs layer\n", displayName)
	} else if part.status == "done" {
		fmt.Printf("%s: Download complete\n", displayName)
	} else if part.status == "waiting" {
		fmt.Printf("%s: Waiting\n", displayName)
	}
}

func manageActive(ctx context.Context, cs content.Store, parts *imageParts) map[string]struct{} {
	fileLog.Println("---> manageActive")
	activeSeen := map[string]struct{}{}
	active, err := cs.ListStatuses(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to get content statuses: %s\n", err)
		return activeSeen
	}
	// update status of active entries
	for _, active := range active {
		parts.set(active.Ref, "downloading", imagePart{
			offset:    active.Offset,
			total:     active.Total,
			startedAt: active.StartedAt,
			updatedAt: active.UpdatedAt,
		})
		activeSeen[active.Ref] = struct{}{}
	}
	return activeSeen
}

func manageInactive(ctx context.Context, cs content.Store, start time.Time, ongoing *jobs, activeSeen map[string]struct{}, parts *imageParts) error {
	fileLog.Println("---> manageInactive")
	for _, j := range ongoing.jobs() {
		key := remotes.MakeRefKey(ctx, j)
		if _, ok := activeSeen[key]; ok {
			continue
		}
		status, ok := parts.get(key)
		if !ok || status.status == "downloading" {
			info, err := cs.Info(ctx, j.Digest)
			if err != nil {
				if !errdefs.IsNotFound(err) {
					log.Printf("Warning: failed to get content info: %s\n", err)
					return err
				} else {
					parts.set(key, "waiting", imagePart{})
				}
			} else if info.CreatedAt.After(start) {
				parts.set(key, "done", imagePart{
					offset:    info.Size,
					total:     info.Size,
					updatedAt: info.CreatedAt,
				})
			} else {
				parts.set(key, "exists", imagePart{})
			}
		}
	}
	return nil
}

func completeInactive(ctx context.Context, ongoing *jobs, parts *imageParts) {
	fileLog.Println("---> completeInactive")
	for _, j := range ongoing.jobs() {
		key := remotes.MakeRefKey(ctx, j)
		status, ok := parts.get(key)
		if ok {
			if status.status != "done" && status.status != "exists" {
				fileLog.Printf("status: %+v\n", status)
				if status.total == 0 {
					status.offset = 1
					status.total = 1
				} else {
					status.offset = status.total
				}
				parts.set(key, "done", status)
			}
		} else {
			parts.set(key, "done", imagePart{
				offset: 1,
				total:  1,
			})
		}
	}
}

func showProgress(ctx context.Context, ongoing *jobs, cs content.Store) {
	var (
		//barManager = mpb.NewWithContext(ctx, mpb.WithRefreshRate(100 * time.Millisecond))
		ticker = time.NewTicker(100 * time.Millisecond)
		start  = time.Now()
		//bars       = newImageBars(barManager)
		//parts      = newImageParts(bars.update, bars.update)
		parts = newImageParts(displayPart, nil)
		last  bool
		stop  bool
	)
	defer ticker.Stop()

outer:
	for ok := true; ok; ok = !stop {
		select {
		case <-ticker.C:
			fileLog.Println("---> tick")
			if last {
				stop = true
				break
			}
			resolved := "resolved"
			if !ongoing.isResolved() {
				resolved = "resolving"
			}
			parts.set(ongoing.name, resolved, imagePart{})
			activeSeen := map[string]struct{}{}
			if !stop {
				activeSeen = manageActive(ctx, cs, parts)
			}
			err := manageInactive(ctx, cs, start, ongoing, activeSeen, parts)
			if err != nil {
				continue outer
			}
		case <-ctx.Done():
			completeInactive(ctx, ongoing, parts)
			last = true
		}
	}
	//fileLog.Println("---# bars.close")
	//bars.close()
	//fileLog.Println("---# barManager.Wait")
	//barManager.Wait()
}

func closeClient(client *containerd.Client) {
	if client != nil {
		err := client.Close()
		if err != nil {
			log.Println("WARNING: Cannot close containerd client")
		}
	}
}

func imageRef(imageName string) (reference.Named, error) {
	imageRef, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return nil, err
	}
	if reference.IsNameOnly(imageRef) {
		imageRef = reference.TagNameOnly(imageRef)
	}
	return imageRef, nil
}

func PullImage(ctx context.Context, client *containerd.Client, credentials registry.Credentials, imageName string, verbose bool) error {
	fmt.Printf("Pulling image %s...\n", imageName)

	imageRef, err := imageRef(imageName)
	if err != nil {
		return err
	}

	resolver := docker.NewResolver(docker.ResolverOptions{
		Tracker: docker.NewInMemoryTracker(),
		Hosts: func(host string) ([]docker.RegistryHost, error) {
			dockerHeaders := make(http.Header)
			dockerHeaders.Set("User-Agent", "containerd/1.3.4")
			dockerAuthorizer := docker.NewDockerAuthorizer(
				docker.WithAuthClient(http.DefaultClient),
				docker.WithAuthHeader(dockerHeaders),
				docker.WithAuthCreds(credentials(host)))
			if host == "docker.io" {
				host = "registry-1.docker.io"
			}
			config := docker.RegistryHost{
				Client:       http.DefaultClient,
				Authorizer:   dockerAuthorizer,
				Host:         host,
				Scheme:       "https",
				Path:         "/v2",
				Capabilities: docker.HostCapabilityPull | docker.HostCapabilityResolve | docker.HostCapabilityPush,
			}
			return []docker.RegistryHost{config}, nil
		},
	})

	if verbose {
		ongoing := newJobs(imageName)
		pctx, stopProgress := context.WithCancel(ctx)
		progress := make(chan struct{})
		go func() {
			showProgress(pctx, ongoing, client.ContentStore())
			close(progress)
		}()

		handler := images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
			if desc.MediaType != images.MediaTypeDockerSchema1Manifest {
				ongoing.add(desc)
			}
			return nil, nil
		})

		image, err := client.Pull(ctx, imageRef.String(), []containerd.RemoteOpt{
			containerd.WithPlatform("linux"),
			containerd.WithResolver(resolver),
			containerd.WithImageHandler(handler),
			containerd.WithSchema1Conversion,
		}...)
		stopProgress()
		<-progress
		if err != nil {
			return err
		}
		fmt.Printf("Successfully pulled %s image\n", image.Name())
	} else {
		image, err := client.Pull(ctx, imageRef.String(), []containerd.RemoteOpt{
			containerd.WithPlatform("linux"),
			containerd.WithResolver(resolver),
			containerd.WithSchema1Conversion,
		}...)
		if err != nil {
			return err
		}
		fmt.Printf("Successfully pulled %s image\n", image.Name())
	}

	return nil
}

func SaveImages(ctx context.Context, client *containerd.Client, images []string, fileName string) error {
	if len(images) == 0 {
		return fmt.Errorf("no images to save")
	}
	fmt.Printf("Saving images in %s...\n", fileName)
	var exportOpts []archive.ExportOpt
	p, err := platforms.Parse("linux")
	exportOpts = append(exportOpts, archive.WithPlatform(platforms.Ordered(p)))
	if err != nil {
		return err
	}
	is := client.ImageService()
	for _, img := range images {
		imageRef, err := imageRef(img)
		if err != nil {
			return err
		}
		exportOpts = append(exportOpts, archive.WithImage(is, imageRef.String()))
	}
	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer f.Close()
	err = client.Export(ctx, bufio.NewWriter(f), exportOpts...)
	if err != nil {
		return err
	}
	fmt.Printf("Successfully saved all images in %s\n", fileName)
	return nil
}

func ListImages(ctx context.Context, client *containerd.Client) error {
	imgs, err := client.ListImages(ctx, "")
	if err != nil {
		return err
	}
	for _, img := range imgs {
		fmt.Printf("%s\n", img.Name())
	}
	return nil
}

func Client(debug bool) (*containerd.Client, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	clientLogFile, err := os.OpenFile(filepath.Join(homeDir, ".containerd", "client.log"), os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer clientLogFile.Close()
	fileLog = log.New(clientLogFile, "container", log.LstdFlags)
	if debug {
		log.Println("Creating containerd client...")
	}
	var client *containerd.Client
	defer closeClient(client)
	for i := 0; i < 12; i++ {
		client, err = containerd.New("\\\\.\\pipe\\containerd-containerd", containerd.WithTimeout(1*time.Second))
		if client != nil {
			break
		} else if err != nil && i > 0 && debug {
			log.Println("containerd server unavailable, retrying...")
		}
	}
	if client == nil {
		return nil, fmt.Errorf("containerd server unavailable: %w", err)
	}
	return client, nil
}

func Server(serverStarted chan bool, serverKill chan bool, serverKilled chan bool, debug bool) {
	err := CreateContainerdDirectories()
	if err != nil {
		log.Printf("Error: cannot create containerd directories: %s\n", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Error: cannot create containerd.log file: %s\n", err)
		serverStarted <- false
	}
	serverLogFile, err := os.OpenFile(filepath.Join(homeDir, ".containerd", "containerd.log"), os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error: cannot create containerd.log file: %s\n", err)
		serverStarted <- false
	}
	defer serverLogFile.Close()
	if debug {
		log.Println("Running containerd server...")
	}
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Error: cannot start containerd: %s\n", err)
	}
	cmd := exec.Command(filepath.Join(filepath.Dir(execPath), "containerd"), "--config", filepath.Join(homeDir, ".containerd", "config.toml"))
	cmd.Stdout = serverLogFile
	cmd.Stderr = serverLogFile
	err = cmd.Start()
	if err != nil {
		log.Printf("Error: Cannot start containerd: %s\n", err)
		serverStarted <- false
	}
	if cmd.Process == nil {
		log.Println("Error: Cannot start containerd")
		serverStarted <- false
	}
	// Wait a bit to make sure client can connect to server
	time.Sleep(3 * time.Second)
	if debug {
		log.Println("Started containerd server")
	}
	serverStarted <- true
	<-serverKill
	if debug {
		log.Println("Stopping containerd server...")
	}
	_ = cmd.Process.Signal(syscall.SIGKILL)
	// Wait a bit to make sure signal is processed before client process is gone
	time.Sleep(3 * time.Second)
	serverKilled <- true
}

func DeleteContainerdDirectories() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(homeDir, ".containerd")
	err = os.RemoveAll(baseDir)
	return err
}

func CreateContainerdDirectories() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(homeDir, ".containerd")
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if err = os.Mkdir(baseDir, os.ModePerm); err != nil {
			return err
		}
	}
	rootDir := filepath.Join(baseDir, "root")
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		if err = os.Mkdir(rootDir, os.ModePerm); err != nil {
			return err
		}
	}
	stateDir := filepath.Join(baseDir, "state")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		if err = os.Mkdir(stateDir, os.ModePerm); err != nil {
			return err
		}
	}
	configFile := filepath.Join(baseDir, "config.toml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		config, err := os.OpenFile(configFile, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
		defer config.Close()
		if err != nil {
			return err
		}
		//root = "C:\\Users\\cvila\\.containerd\\root"
		//state = "C:\\Users\\cvila\\.containerd\\state"
		rootDir := strings.ReplaceAll(filepath.Join(baseDir, "root"), "\\", "\\\\")
		stateDir := strings.ReplaceAll(filepath.Join(baseDir, "state"), "\\", "\\\\")
		_, err = config.WriteString("root = \"" + rootDir + "\"\n")
		_, err = config.WriteString("state = \"" + stateDir + "\"")
		_, err = config.WriteString(containerdConfig)
		if err != nil {
			return err
		}
	}
	return nil
}
