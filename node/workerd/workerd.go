package workerd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/tunnel"
	"github.com/Filecoin-Titan/titan/node/workerd/cgo"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

const (
	// specifies the file extension for ZIP archives.
	defaultZipSuffix      = ".zip"
	defaultSha256Suffix   = ".sha256"
	defaultConfigFilename = "config.capnp"
)

const (
	defaultQueueSize      = 10
	defaultSyncerInterval = time.Second * 60
)

var log = logging.Logger("workerd")

type Workerd struct {
	api      api.Scheduler
	nodeId   string
	basePath string
	ts       *tunnel.Services
	startCh  chan string

	projects map[string]*types.Project
	mu       sync.Mutex
}

func NewWorkerd(api api.Scheduler, ts *tunnel.Services, nodeId, path string) (*Workerd, error) {
	err := os.MkdirAll(path, 0o755)
	if err != nil {
		return nil, err
	}

	w := &Workerd{
		api:      api,
		nodeId:   nodeId,
		basePath: path,
		ts:       ts,
		projects: make(map[string]*types.Project),
		startCh:  make(chan string, defaultQueueSize),
	}

	go w.run(context.Background())

	return w, nil
}

func (w *Workerd) run(ctx context.Context) {
	syncerTicker := time.NewTicker(defaultSyncerInterval)

	for {
		select {
		case projectId := <-w.startCh:
			err := w.startProject(ctx, projectId)
			if err != nil {
				log.Errorf("starting project %s: %v", projectId, err)
			}

		case <-syncerTicker.C:
			go w.sync(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (w *Workerd) reportProjectStatus(ctx context.Context, project *types.Project) {
	w.mu.Lock()
	w.projects[project.ID].Status = project.Status
	w.mu.Unlock()

	err := w.api.UpdateProjectStatus(ctx, []*types.Project{project})
	if err != nil {
		log.Errorf("UpdateProjectStatus: %v", err)
		return
	}
}

func (w *Workerd) Deploy(ctx context.Context, project *types.Project) error {
	log.Infof("starting deploy project, id: %s", project.ID)

	w.mu.Lock()
	w.projects[project.ID] = project
	w.mu.Unlock()

	select {
	case w.startCh <- project.ID:
		log.Infof("Project ID %s sent to start channel", project.ID)
	case <-ctx.Done():
		log.Errorf("Deployment cancelled for project ID %s", project.ID)
		return ctx.Err()
	}

	return nil
}

func (w *Workerd) Update(ctx context.Context, project *types.Project) error {
	log.Infof("starting update project, id: %s", project.ID)

	path := w.getProjectPath(project.ID)

	if !Exists(path) {
		return xerrors.Errorf("project %s not exists", project.ID)
	}

	if err := w.destroyProject(ctx, project.ID); err != nil {
		return err
	}

	w.mu.Lock()
	w.projects[project.ID] = project
	w.mu.Unlock()

	select {
	case w.startCh <- project.ID:
		log.Infof("Project ID %s sent to start channel", project.ID)
	case <-ctx.Done():
		log.Errorf("Deployment cancelled for project ID %s", project.ID)
		return ctx.Err()
	}

	return nil
}

func (w *Workerd) Query(ctx context.Context, ids []string) ([]*types.Project, error) {
	var out []*types.Project

	for _, id := range ids {
		project := &types.Project{
			ID:     id,
			Status: types.ProjectReplicaStatusStarted,
		}

		running, err := w.queryProject(ctx, id)
		if err != nil && !running {
			project.Status = types.ProjectReplicaStatusError
			project.Msg = err.Error()
		}

		out = append(out, project)
	}

	return out, nil
}

func (w *Workerd) getProjectPath(projectId string) string {
	return filepath.Join(w.basePath, projectId)
}

func (w *Workerd) Delete(ctx context.Context, projectId string) error {
	log.Infof("starting delete project, id: %s", projectId)

	path := w.getProjectPath(projectId)

	if !Exists(path) {
		return xerrors.Errorf("project %s not exists", projectId)
	}

	err := w.destroyProject(ctx, projectId)
	if err != nil {
		log.Errorf("destroying project %s: %v", projectId, err)
	}

	return nil
}

func (w *Workerd) getProject(projectId string) (*types.Project, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	project, exists := w.projects[projectId]
	if !exists {
		return nil, xerrors.Errorf("project %s does not exist", projectId)
	}
	return project, nil
}

func (w *Workerd) ensureProjectInitialized(ctx context.Context, project *types.Project) error {
	if Exists(w.getProjectPath(project.ID)) {
		return nil
	}
	if err := w.createProject(ctx, project); err != nil {
		log.Errorf("error creating project %s: %v", project.ID, err)
		return err
	}
	return nil
}

func (w *Workerd) setupAndStartProject(ctx context.Context, project *types.Project) error {
	port, err := GetFreePort()
	if err != nil {
		log.Errorf("error getting free port for project %s: %v", project.ID, err)
		return err
	}

	project.Status = types.ProjectReplicaStatusStarted
	service := &tunnel.Service{ID: project.ID, Address: "127.0.0.1", Port: port}
	defer w.reportProjectStatus(ctx, project)
	defer w.ts.Regiseter(service)

	socketAddr := fmt.Sprintf("%s:%d", service.Address, service.Port)
	if err := cgo.CreateWorkerd(project.ID, w.getProjectPath(project.ID), defaultConfigFilename, socketAddr); err != nil {
		project.Status = types.ProjectReplicaStatusError
		project.Msg = err.Error()
		log.Errorf("error in CGo while creating project %s: %v", project.ID, err)
		return err
	}

	return nil
}

func (w *Workerd) startProject(ctx context.Context, projectId string) error {
	project, err := w.getProject(projectId)
	if err != nil {
		return err
	}

	if err := w.ensureProjectInitialized(ctx, project); err != nil {
		return err
	}

	if running, _ := w.queryProject(ctx, projectId); running {
		return xerrors.Errorf("project %s is already running", projectId)
	}

	if err := w.setupAndStartProject(ctx, project); err != nil {
		return err
	}

	return nil
}

func (w *Workerd) createProject(ctx context.Context, project *types.Project) error {
	projectPath := w.getProjectPath(project.ID)

	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		return xerrors.Errorf("project %s already initialized", project.ID)
	}

	if project.Name == "" {
		project.Name = project.ID
	}

	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		return xerrors.Errorf("failed to create project directory: %v", err)
	}

	zipFilePath := filepath.Join(projectPath, project.Name+defaultZipSuffix)
	if err := downloadBundle(ctx, project, zipFilePath); err != nil {
		return xerrors.Errorf("failed to download bundle: %v", err)
	}

	sha256FilePath := filepath.Join(projectPath, project.Name+defaultSha256Suffix)
	if err := writeSha256File(zipFilePath, sha256FilePath); err != nil {
		return xerrors.Errorf("failed to write SHA256 checksum: %v", err)
	}

	if err := unzip(zipFilePath, projectPath); err != nil {
		return err
	}

	return copySubDirectory(projectPath, projectPath)
}

func (w *Workerd) destroyProject(ctx context.Context, projectId string) error {
	defer w.ts.Remove(&tunnel.Service{ID: projectId})

	if err := cgo.DestroyWorkerd(projectId); err != nil {
		log.Errorf("cgo.DestroyWorkerd: %v", err)
		return err
	}

	if err := os.RemoveAll(w.getProjectPath(projectId)); err != nil {
		return err
	}

	return nil
}

func (w *Workerd) queryProject(ctx context.Context, projectId string) (bool, error) {
	return cgo.QueryWorkerd(projectId)
}

func (w *Workerd) initializing() error {
	return cgo.InitWorkerdRuntime()
}

func writeSha256File(sourcePath, destinationPath string) error {
	file, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer file.Close()

	hash, err := computeBundleHash(sourcePath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(file, strings.NewReader(hash)); err != nil {
		return err
	}

	return nil
}

func downloadBundle(ctx context.Context, project *types.Project, outPath string) error {
	// Create the file
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(project.BundleURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

// computeBundleHash calculates file hash
func computeBundleHash(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (w *Workerd) localProjectIds() ([]string, error) {
	var projectIds []string
	err := filepath.WalkDir(w.basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Check if the directory name is a valid project ID.
		if d.IsDir() {
			if _, pErr := uuid.Parse(d.Name()); pErr == nil {
				projectIds = append(projectIds, d.Name())
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return projectIds, nil
}

// RestartProjects reinitialized and synchronizes the worker's projects.
func (w *Workerd) RestartProjects(ctx context.Context) {
	if err := w.initializing(); err != nil {
		log.Errorf("Failed to initialize during project restart: %v", err)
		return
	}

	w.sync(ctx)
}

// GetFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

// sync synchronizes the local projects with the active projects from the API.
func (w *Workerd) sync(ctx context.Context) {
	// Retrieve local projects.
	localProjects, err := w.localProjectIds()
	if err != nil {
		log.Errorf("failed to get local projects: %v", err)
		return
	}

	// Fetch active projects from the API.
	projects, err := w.api.GetProjectsForNode(ctx, w.nodeId)
	if err != nil {
		log.Errorf("GetProjectsForNode: %v", err)
		return
	}

	// Use a temporary map to reduce lock contention.
	tempProjects := make(map[string]*types.Project)

	// Active projects tracking.
	activeProjects := make(map[string]struct{})

	for _, project := range projects {
		tempProjects[project.Id] = &types.Project{ID: project.Id}
		activeProjects[project.Id] = struct{}{}

		switch project.Status {
		case types.ProjectReplicaStatusStarted, types.ProjectReplicaStatusStarting:
			if running, _ := w.queryProject(ctx, project.Id); !running {
				w.startCh <- project.Id
			}
		}
	}

	w.mu.Lock()
	w.projects = tempProjects
	w.mu.Unlock()

	// Destroy inactive local projects.
	for _, projectId := range localProjects {
		if _, ok := activeProjects[projectId]; ok {
			continue
		}

		log.Infof("destroying inactive local project: %s", projectId)

		err := w.destroyProject(context.Background(), projectId)
		if err != nil {
			log.Errorf("failed to destroy project %s: %v", projectId, err)
		}
	}
}
