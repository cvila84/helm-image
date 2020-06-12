package containerd

import (
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

var fileLog *log.Logger

type credentials func(host string) func(string) (string, string, error)

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
		log.Printf("WARNING: failed to get content statuses: %s\n", err)
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
					log.Printf("WARNING: failed to get content info: %s\n", err)
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
				fileLog.Printf("--- %+v\n", status)
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

func PullImage(ctx context.Context, client *containerd.Client, credentials credentials, imageName string) error {
	log.Printf("Pulling image %s...\n", imageName)

	imageRef, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return fmt.Errorf("reference.ParseNormalizedNamed: %w", err)
	}
	if reference.IsNameOnly(imageRef) {
		imageRef = reference.TagNameOnly(imageRef)
	}

	ongoing := newJobs(imageName)
	pctx, stopProgress := context.WithCancel(ctx)
	progress := make(chan struct{})
	go func() {
		showProgress(pctx, ongoing, client.ContentStore())
		close(progress)
	}()

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
		return fmt.Errorf("client.Pull: %w", err)
	}
	log.Printf("Successfully pulled %s image\n", image.Name())
	return nil
}

func SaveImage(ctx context.Context, client *containerd.Client, imageName string, fileName string) error {
	if len(imageName) > 0 {
		log.Printf("Saving image %s in %s...\n", imageName, fileName)
	} else {
		log.Printf("Saving all images in %s...\n", fileName)
	}
	var exportOpts []archive.ExportOpt
	p, err := platforms.Parse("linux")
	exportOpts = append(exportOpts, archive.WithPlatform(platforms.Ordered(p)))
	var images []string
	if len(imageName) > 0 {
		images = append(images, imageName)
	} else {
		imgs, err := client.ListImages(ctx, "")
		if err != nil {
			return fmt.Errorf("Client.ListImages: %w", err)
		}
		for _, img := range imgs {
			images = append(images, img.Name())
		}
	}
	is := client.ImageService()
	for _, img := range images {
		exportOpts = append(exportOpts, archive.WithImage(is, img))
	}
	f, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("os.Create: %w", err)
	}
	defer f.Close()
	err = client.Export(ctx, f, exportOpts...)
	if err != nil {
		return fmt.Errorf("client.Export: %w", err)
	}
	if len(imageName) > 0 {
		log.Printf("Successfully saved image %s in %s\n", imageName, fileName)
	} else {
		log.Printf("Successfully saved all images in %s\n", fileName)
	}
	return nil
}

//func ListImages(ctx context.Context, client *containerd.Client) error {
//	imgs, err := client.ListImages(ctx, "")
//	if err != nil {
//		return fmt.Errorf("client.List: %w", err)
//	}
//	for _, img := range imgs {
//		fmt.Printf("%s\n", img.Name())
//	}
//	return nil
//}

func Client() (*containerd.Client, error) {
	clientLogFile, err := os.OpenFile("container.log", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("cannot create container.log file: %s\n", err)
		os.Exit(1)
	}
	defer clientLogFile.Close()
	fileLog = log.New(clientLogFile, "container", log.LstdFlags)
	log.Println("Creating containerd client...")
	var client *containerd.Client
	defer closeClient(client)
	for i := 0; i < 8; i++ {
		client, err = containerd.New("\\\\.\\pipe\\containerd-containerd", containerd.WithTimeout(1*time.Second))
		if client != nil {
			break
		} else if err != nil && i > 0 {
			log.Println("containerd.New: server unavailable, retrying...")
		}
	}
	if client == nil {
		return nil, fmt.Errorf("containerd.New: server unavailable: %w", err)
	}
	return client, nil
}

func Server(serverStarted chan bool, serverKill chan bool, serverKilled chan bool) {
	serverLogFile, err := os.OpenFile("containerd.log", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("cannot create containerd.log file: %s\n", err)
		serverStarted <- false
	}
	defer serverLogFile.Close()
	log.Println("Running containerd server...")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("ERROR: Cannot start containerd: %s\n", err)
		serverStarted <- false
	}
	configFile := filepath.Join(homeDir, ".containerd", "config.toml")
	cmd := exec.Command("containerd", "--config", configFile)
	cmd.Stdout = serverLogFile
	cmd.Stderr = serverLogFile
	err = cmd.Start()
	if err != nil {
		log.Printf("ERROR: Cannot start containerd: %s\n", err)
		serverStarted <- false
	}
	if cmd.Process == nil {
		log.Println("ERROR: Cannot start containerd")
		serverStarted <- false
	}
	// Wait a bit to make sure client can connect to server
	time.Sleep(3 * time.Second)
	log.Println("Started containerd server")
	serverStarted <- true
	<-serverKill
	log.Println("Stopping containerd server...")
	_ = cmd.Process.Signal(syscall.SIGKILL)
	// Wait a bit to make sure signal is processed before client process is gone
	time.Sleep(2 * time.Second)
	serverKilled <- true
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
	return nil
}
