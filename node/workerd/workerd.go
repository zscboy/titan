package workerd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/tunnel"
	"github.com/Filecoin-Titan/titan/node/workerd/cgo"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

const (
	zipSuffix    = ".zip"
	sha256Suffix = ".sha256"
)

const defaultConfigFilename = "config.capnp"

var log = logging.Logger("workerd")

type Workerd struct {
	api      api.Scheduler
	basePath string
	ts       *tunnel.Services
	startCh  chan string
	stopCh   chan string

	projects map[string]*types.Project
	mu       sync.Mutex
}

func NewWorkerd(api api.Scheduler, ts *tunnel.Services, path string) (*Workerd, error) {
	err := os.MkdirAll(path, 0o755)
	if err != nil {
		return nil, err
	}

	w := &Workerd{
		api:      api,
		basePath: path,
		ts:       ts,
		projects: make(map[string]*types.Project),
		startCh:  make(chan string, 1),
		stopCh:   make(chan string, 1),
	}

	go w.run(context.Background())

	return w, nil
}

func (w *Workerd) run(ctx context.Context) {
	for {
		select {
		case projectId := <-w.startCh:
			err := w.startProject(ctx, projectId)
			if err != nil {
				log.Errorf("starting project %s: %v", projectId, err)
			}

		case projectId := <-w.stopCh:
			err := w.destroyProject(ctx, projectId)
			if err != nil {
				log.Errorf("destroying project %s: %v", projectId, err)
			}

			err = w.cleanProject(ctx, projectId)
			if err != nil {
				log.Errorf("destroying project %s: %v", projectId, err)
			}

		case <-ctx.Done():
			return
		}
	}
}

func (w *Workerd) updateProject(ctx context.Context, project *types.Project) {
	err := w.api.UpdateProjectStatus(ctx, []*types.Project{{
		ID:     project.ID,
		Status: project.Status,
	}})
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

	w.startCh <- project.ID

	return nil
}

func (w *Workerd) Update(ctx context.Context, project *types.Project) error {
	log.Infof("starting update project, id: %s", project.ID)

	path := w.getProjectPath(project.ID)

	if !Exists(path) {
		return xerrors.Errorf("project %s not exists", project.ID)
	}

	if len(project.Name) == 0 {
		project.Name = project.ID
	}

	zipFilePath := filepath.Join(w.getProjectPath(project.ID), project.Name) + zipSuffix

	if err := downloadBundle(ctx, project, zipFilePath); err != nil {
		return err
	}

	sha256FilePath := filepath.Join(w.getProjectPath(project.ID), project.Name) + sha256Suffix

	hashBytes, err := os.ReadFile(sha256FilePath)
	if err != nil {
		return err
	}

	// Check hash.
	if f, err := os.Open(zipFilePath); err == nil {
		h := sha256.New()
		io.Copy(h, f)
		f.Close()

		if string(hashBytes) == hex.EncodeToString(h.Sum(nil)) {
			return os.RemoveAll(zipFilePath)
		}
	}
	// Hash did not match. Fall through and rewrite file.

	file, err := os.Create(sha256FilePath)
	if err != nil {
		return fmt.Errorf("create file: %s", err)
	}
	defer file.Close()

	_, err = io.Copy(file, bytes.NewReader(hashBytes))
	if err != nil {
		return fmt.Errorf("copy file: %s", err)
	}

	if err := Unzip(zipFilePath, path); err != nil {
		return err
	}

	w.startCh <- project.ID

	return nil
}

func (w *Workerd) Query(ctx context.Context, ids []string) ([]*types.Project, error) {
	var out []*types.Project

	for _, id := range ids {
		status := types.ProjectReplicaStatusStarted
		err := w.queryProject(ctx, id)
		if err != nil {
			status = types.ProjectReplicaStatusError
		}

		out = append(out, &types.Project{
			ID:     id,
			Status: status,
		})
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

	w.stopCh <- projectId

	return nil
}

func (w *Workerd) startProject(ctx context.Context, projectId string) error {
	if !Exists(w.getProjectPath(projectId)) {

		w.mu.Lock()
		project, ok := w.projects[projectId]
		w.mu.Unlock()

		if !ok {
			return xerrors.Errorf("unknown error")
		}

		if err := w.createProject(ctx, project); err != nil {
			log.Errorf("creating project %s: %v", project.ID, err)
			return err
		}
	}

	port, err := GetFreePort()
	if err != nil {
		log.Errorf("get free port %s: %v", projectId, err)
		return err
	}

	configFilePath := filepath.Join(w.getProjectPath(projectId), defaultConfigFilename)
	if err = ModifiedPort(configFilePath, port); err != nil {
		log.Errorf("modified %s: %v", projectId, err)
		return err
	}

	project := &types.Project{ID: projectId, Status: types.ProjectReplicaStatusStarted}
	service := &tunnel.Service{ID: projectId, Address: "localhost", Port: port}

	defer w.updateProject(ctx, project)
	defer w.ts.Regiseter(service)

	err = cgo.CreateWorkerd(projectId, w.getProjectPath(projectId), defaultConfigFilename)
	if err != nil {
		project.Status = types.ProjectReplicaStatusError
		log.Errorf("cgo creating project %s: %v", projectId, err)
		return err
	}

	return nil
}

func (w *Workerd) destroyProject(ctx context.Context, projectId string) error {
	defer w.ts.Remove(&tunnel.Service{ID: projectId})
	return cgo.DestroyWorkerd(projectId)
}

func (w *Workerd) queryProject(ctx context.Context, projectId string) error {
	return cgo.QueryWorkerd(projectId)
}

func (w *Workerd) cleanProject(ctx context.Context, projectId string) error {
	return os.RemoveAll(w.getProjectPath(projectId))
}

func (w *Workerd) initializing() error {
	return cgo.InitWorkerdRuntime()
}

func (w *Workerd) createProject(ctx context.Context, project *types.Project) error {
	path := w.getProjectPath(project.ID)

	_, err := os.Stat(path)
	if !os.IsNotExist(err) {
		if err == nil {
			return xerrors.Errorf("project %s already initialized", project.ID)
		}
		return err
	}

	if len(project.Name) == 0 {
		project.Name = project.ID
	}

	if err = os.MkdirAll(path, 0o755); err != nil {
		return err
	}

	zipFilePath := filepath.Join(path, project.Name) + zipSuffix
	if err = downloadBundle(ctx, project, zipFilePath); err != nil {
		return err
	}

	sha256FilePath := filepath.Join(w.getProjectPath(project.ID), project.Name) + sha256Suffix
	file, err := os.Create(sha256FilePath)
	if err != nil {
		return fmt.Errorf("create file: %s", err)
	}
	defer file.Close()

	_, err = io.Copy(file, strings.NewReader(computeBundleHash(zipFilePath)))
	if err != nil {
		return fmt.Errorf("copy file: %s", err)
	}

	if err = Unzip(zipFilePath, path); err != nil {
		return err
	}

	return CopySubDirectory(path, path)
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
func computeBundleHash(filename string) string {
	data, _ := os.ReadFile(filename)
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (w *Workerd) RestartProjects(ctx context.Context, nodeId string) {
	if err := w.initializing(); err != nil {
		log.Errorf("restarting project initializing failed: %v", err)
		return
	}

	projects, err := w.api.GetProjectsForNode(ctx, nodeId)
	if err != nil {
		log.Errorf("GetProjectsForNode: %v", err)
		return
	}

	for _, project := range projects {
		switch project.Status {
		case types.ProjectReplicaStatusStarted, types.ProjectReplicaStatusStarting, types.ProjectReplicaStatusUpdating:
			w.startCh <- project.Id
		case types.ProjectReplicaStatusStopped:
			w.stopCh <- project.Id
		}
	}

	return
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

func ModifiedPort(filePath string, port int) error {
	cmd := exec.Command("sed", "-i", fmt.Sprintf(`s/\(address = "\*\):\([0-9]*\)/\1:%d/`, port), filePath)
	return cmd.Run()
}
