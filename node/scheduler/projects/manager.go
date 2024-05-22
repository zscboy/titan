package projects

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/terrors"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/scheduler/db"
	"github.com/Filecoin-Titan/titan/node/scheduler/node"
	"github.com/filecoin-project/go-statemachine"
	"github.com/google/uuid"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("projects")

const (
	// If the node does not reply more than once, the project timeout is determined.
	projectTimeoutLimit = 3
	// Interval to get asset pull progress from node (Unit:Second)
	progressInterval = 60 * time.Second
)

// Manager manages project replicas
type Manager struct {
	*db.SQLDB
	nodeMgr              *node.Manager // node manager
	stateMachineWait     sync.WaitGroup
	projectStateMachines *statemachine.StateGroup
	deployingProjects    sync.Map // Assignments where projects are being pulled
}

// NewManager returns a new projectManager instance
func NewManager(nodeManager *node.Manager, sdb *db.SQLDB, ds datastore.Batching) *Manager {
	m := &Manager{
		SQLDB:   sdb,
		nodeMgr: nodeManager,
	}

	// state machine initialization
	m.stateMachineWait.Add(1)
	m.projectStateMachines = statemachine.New(ds, m, ProjectInfo{})

	return m
}

// Start initializes and starts the project state machine and associated tickers
func (m *Manager) StartTimer(ctx context.Context) {
	if err := m.initStateMachines(); err != nil {
		log.Errorf("restartStateMachines err: %s", err.Error())
	}

	go m.startCheckDeployProgressesTimer()
}

func (m *Manager) UpdateStatus(nodeID string, list []*types.Project) error {
	for _, info := range list {
		exist, _ := m.projectStateMachines.Has(ProjectID(info.ID))
		if !exist {
			continue
		}

		exist = m.isProjectTaskExist(info.ID)
		if !exist {
			continue
		}

		err := m.SaveProjectReplicasInfo(&types.ProjectReplicas{
			Id:     info.ID,
			NodeID: nodeID,
			Status: info.Status,
		})
		if err != nil {
			log.Errorf("UpdateStatus SaveProjectReplicasInfo %s,%s err:%s", nodeID, info.ID, err.Error())
		}
	}

	return nil
}

func (m *Manager) Deploy(req *types.DeployProjectReq) (string, error) {
	uid := uuid.NewString()

	// Waiting for state machine initialization
	m.stateMachineWait.Wait()
	log.Infof("project event: %s, add project ", req.Name)

	expiration := time.Now().Add(150 * 24 * time.Hour)

	info := &types.ProjectInfo{
		UUID:        uid,
		ServerID:    m.nodeMgr.ServerID,
		Expiration:  expiration,
		State:       Create.String(),
		CreatedTime: time.Now(),
		Name:        req.Name,
		BundleURL:   req.BundleURL,
		UserID:      req.UserID,
		Replicas:    req.Replicas,
	}

	err := m.SaveProjectInfo(info)
	if err != nil {
		return "", &api.ErrWeb{Code: terrors.DatabaseErr.Int(), Message: err.Error()}
	}

	rInfo := ProjectForceState{
		State: Create,
	}

	// create project task
	err = m.projectStateMachines.Send(ProjectID(info.UUID), rInfo)
	if err != nil {
		return "", &api.ErrWeb{Code: terrors.NotFound.Int(), Message: err.Error()}
	}

	return uid, nil
}

func (m *Manager) Start(req *types.ProjectReq) error {
	if req.UUID == "" {
		return xerrors.New("UUID is nil")
	}

	pInfo, err := m.GetProjectInfo(req.UUID)
	if err != nil {
		return xerrors.Errorf("GetProjectInfo err:%s", err.Error())
	}

	if req.NodeID != "" {
		node := m.nodeMgr.GetNode(req.NodeID)
		if node == nil {
			return xerrors.Errorf("node %s ont found", req.NodeID)
		}

		return node.Start(context.Background(), &types.Project{ID: req.UUID, BundleURL: pInfo.BundleURL, Name: pInfo.Name})
	}

	for _, info := range pInfo.DetailsList {
		// request nodes
		node := m.nodeMgr.GetNode(info.NodeID)
		if node == nil {
			continue
		}

		err := node.Start(context.Background(), &types.Project{ID: req.UUID, BundleURL: pInfo.BundleURL, Name: pInfo.Name})
		if err != nil {
			log.Errorf("StartProject %s err:%s", info.NodeID, err.Error())
		}
	}
	return nil
}

func (m *Manager) Update(req *types.ProjectReq) error {
	if req.UUID == "" {
		return xerrors.New("UUID is nil")
	}

	list, err := m.LoadProjectReplicasInfos(req.UUID)
	if err != nil {
		return err
	}

	for _, info := range list {
		// request nodes
		node := m.nodeMgr.GetNode(info.NodeID)
		if node == nil {
			continue
		}

		err := node.Update(context.Background(), &types.Project{ID: req.UUID, BundleURL: req.BundleURL, Name: req.Name})
		if err != nil {
			log.Errorf("Update %s err:%s", info.NodeID, err.Error())
		}
	}
	return nil
}

func (m *Manager) Delete(req *types.ProjectReq) error {
	if req.UUID == "" {
		// TODO test codes
		pList, err := m.LoadProjectInfos(m.nodeMgr.ServerID, "", 500, 0)
		if err != nil {
			return err
		}

		for _, pInfo := range pList {
			if exist, _ := m.projectStateMachines.Has(ProjectID(pInfo.UUID)); !exist {
				continue
			}

			err = m.projectStateMachines.Send(ProjectID(pInfo.UUID), ProjectForceState{State: Remove})
			if err != nil {
				log.Errorf("projectStateMachines send remove %s err:%s", pInfo.UUID, err.Error())
			}
		}

		return xerrors.New("UUID is nil")
	}

	if req.NodeID != "" {
		m.removeReplica(req.UUID, req.NodeID)

		return nil
	}

	if exist, _ := m.projectStateMachines.Has(ProjectID(req.UUID)); !exist {
		return xerrors.Errorf("not found project %s", req.UUID)
	}

	return m.projectStateMachines.Send(ProjectID(req.UUID), ProjectForceState{State: Remove})
}

func (m *Manager) GetProjectInfo(uuid string) (*types.ProjectInfo, error) {
	info, err := m.LoadProjectInfo(uuid)
	if err != nil {
		return nil, err
	}

	list, err := m.LoadProjectReplicasInfos(uuid)
	if err != nil {
		return nil, err
	}

	for _, dInfo := range list {
		node := m.nodeMgr.GetNode(dInfo.NodeID)
		if node == nil {
			continue
		}

		vNode := m.nodeMgr.GetNode(node.WSServerID)
		if vNode == nil {
			continue
		}

		wsURL, err := transformURL(vNode.ExternalURL)
		if err != nil {
			wsURL = fmt.Sprintf("ws://%s", vNode.RemoteAddr)
		}

		dInfo.WsURL = wsURL
	}

	info.DetailsList = list

	return info, nil
}

func transformURL(inputURL string) (string, error) {
	// Parse the URL from the string
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", err
	}

	switch parsedURL.Scheme {
	case "https":
		parsedURL.Scheme = "wss"
	case "http":
		parsedURL.Scheme = "ws"
	default:
		return "", xerrors.New("Scheme not http or https")
	}

	// Remove the path to clear '/rpc/v0'
	parsedURL.Path = ""

	// Return the modified URL as a string
	return parsedURL.String(), nil
}

type deployingProjectsInfo struct {
	count      int
	expiration time.Time
}

// Reset the count of no response project tasks
func (m *Manager) startProjectTimeoutCounting(hash string, count int) {
	info := &deployingProjectsInfo{count: 0}

	infoI, _ := m.deployingProjects.Load(hash)
	if infoI != nil {
		info = infoI.(*deployingProjectsInfo)
	} else {
		needTime := int64(60 * 60)
		info.expiration = time.Now().Add(time.Second * time.Duration(needTime))
	}
	info.count = count

	m.deployingProjects.Store(hash, info)
}

func (m *Manager) stopProjectTimeoutCounting(hash string) {
	m.deployingProjects.Delete(hash)
}

func (m *Manager) isProjectTaskExist(hash string) bool {
	_, exist := m.deployingProjects.Load(hash)

	return exist
}

// removeReplica remove a replica for node
func (m *Manager) removeReplica(id, nodeID string) error {
	err := m.DeleteProjectReplica(id, nodeID)
	if err != nil {
		return err
	}

	node := m.nodeMgr.GetNode(nodeID)
	if node != nil {
		go node.Delete(context.Background(), id)
	}

	return nil
}

// Terminate stops the project state machine
func (m *Manager) Terminate(ctx context.Context) error {
	log.Infof("Terminate stop")
	return m.projectStateMachines.Stop(ctx)
}

func (m *Manager) retrieveNodeDeployProgresses() {
	m.deployingProjects.Range(func(key, value interface{}) bool {
		id := key.(string)
		info := value.(*deployingProjectsInfo)

		if info.expiration.Before(time.Now()) {
			haveError := false
			// checkout node state
			nodes, err := m.LoadNodesOfStartingReplica(id)
			if err != nil {
				log.Errorf("retrieveNodeDeployProgresses %s LoadReplicas err:%s", id, err.Error())
				haveError = true
			} else {
				for _, nodeID := range nodes {
					result, err := m.requestNodeDeployProgresses(nodeID, []string{id})
					if err != nil {
						log.Errorf("retrieveNodeDeployProgresses %s %s requestNodeDeployProgresses err:%s", nodeID, id, err.Error())
						haveError = true
					} else {
						m.UpdateStatus(nodeID, result)
					}
				}
			}

			if haveError {
				m.setProjectTimeout(id, fmt.Sprintf("expiration:%s", info.expiration.String()))
				return true
			}
		}

		exist, _ := m.projectStateMachines.Has(ProjectID(id))
		if !exist {
			return true
		}

		err := m.projectStateMachines.Send(ProjectID(id), DeployResult{})
		if err != nil {
			log.Errorf("retrieveNodeDeployProgresses %s  statemachine send err:%s", id, err.Error())
			return true
		}

		return true
	})
}

// func (m *Manager) retrieveNodeDeployProgresses2() {
// 	deployingNodes := make(map[string][]string)
// 	toMap := make(map[string]*deployingProjectsInfo)

// 	m.deployingProjects.Range(func(key, value interface{}) bool {
// 		id := key.(string)
// 		info := value.(*deployingProjectsInfo)

// 		toMap[id] = info
// 		return true
// 	})

// 	for id, info := range toMap {
// 		stateInfo, err := m.LoadProjectStateInfo(id, m.nodeMgr.ServerID)
// 		if err != nil {
// 			continue
// 		}

// 		if stateInfo.State != Deploying.String() {
// 			continue
// 		}

// 		if info.count >= projectTimeoutLimit {
// 			m.setProjectTimeout(id, fmt.Sprintf("count:%d", info.count))
// 			continue
// 		}

// 		if info.expiration.Before(time.Now()) {
// 			m.setProjectTimeout(id, fmt.Sprintf("expiration:%s", info.expiration.String()))
// 			continue
// 		}

// 		m.startProjectTimeoutCounting(id, info.count+1)

// 		nodes, err := m.LoadNodesOfStartingReplica(id)
// 		if err != nil {
// 			log.Errorf("retrieveNodeDeployProgresses %s LoadReplicas err:%s", id, err.Error())
// 			continue
// 		}

// 		for _, nodeID := range nodes {
// 			list := deployingNodes[nodeID]
// 			deployingNodes[nodeID] = append(list, id)
// 		}
// 	}

// 	getCP := func(nodeID string, cids []string, delay int) {
// 		time.Sleep(time.Duration(delay) * time.Second)

// 		// request node
// 		result, err := m.requestNodeDeployProgresses(nodeID, cids)
// 		if err != nil {
// 			log.Errorf("retrieveNodeDeployProgresses %s requestNodeDeployProgresses err:%s", nodeID, err.Error())
// 			return
// 		}

// 		// update project info
// 		m.updateProjectDeployResults(nodeID, result)
// 	}

// 	duration := 1
// 	delay := 0
// 	for nodeID, ids := range deployingNodes {
// 		delay += duration
// 		if delay > 50 {
// 			delay = 0
// 		}

// 		go getCP(nodeID, ids, delay)
// 	}
// }

// // updateProjectDeployResults updates project results
// func (m *Manager) updateProjectDeployResults(nodeID string, result []*types.Project) {
// 	// doneCount := 0

// 	for _, progress := range result {
// 		log.Infof("updateProjectDeployResults node_id: %s, status: %d, id: %s ", nodeID, progress.Status, progress.ID)

// 		exist, _ := m.projectStateMachines.Has(ProjectID(progress.ID))
// 		if !exist {
// 			continue
// 		}

// 		exist = m.isProjectTaskExist(progress.ID)
// 		if !exist {
// 			continue
// 		}

// 		m.startProjectTimeoutCounting(progress.ID, 0)

// 		if progress.Status == types.ProjectReplicaStatusStarting {
// 			continue
// 		}

// 		err := m.SaveProjectReplicasInfo(&types.ProjectReplicas{
// 			Id:     progress.ID,
// 			NodeID: nodeID,
// 			Status: progress.Status,
// 		})
// 		if err != nil {
// 			log.Errorf("updateProjectDeployResults %s SaveProjectReplicasInfo err:%s", nodeID, err.Error())
// 			continue
// 		}

// 		// if progress.Status == types.ProjectReplicaStatusStarted {
// 		// 	doneCount++
// 		// }

// 		// if progress.Status == types.ProjectReplicaStatusError {
// 		// 	doneCount++
// 		// }

// 		err = m.projectStateMachines.Send(ProjectID(progress.ID), DeployResult{})
// 		if err != nil {
// 			log.Errorf("updateProjectDeployResults %s %s statemachine send err:%s", nodeID, progress.ID, err.Error())
// 			continue
// 		}
// 	}
// }

func (m *Manager) requestNodeDeployProgresses(nodeID string, ids []string) (result []*types.Project, err error) {
	node := m.nodeMgr.GetNode(nodeID)
	if node == nil {
		err = xerrors.Errorf("node %s not found", nodeID)
		return
	}

	result, err = node.Query(context.Background(), ids)
	return
}

func (m *Manager) setProjectTimeout(id, msg string) {
	nodes, err := m.LoadNodesOfStartingReplica(id)
	if err != nil {
		log.Errorf("setProjectTimeout %s LoadNodesOfStartingReplica err:%s", id, err.Error())
		return
	}

	// update replicas status
	err = m.UpdateProjectReplicasStatusToFailed(id)
	if err != nil {
		log.Errorf("setProjectTimeout %s UpdateProjectReplicasStatusToFailed err:%s", id, err.Error())
		return
	}

	for _, nodeID := range nodes {
		node := m.nodeMgr.GetNode(nodeID)
		if node != nil {
			go node.Delete(context.Background(), id)
		}
	}

	err = m.projectStateMachines.Send(ProjectID(id), DeployFailed{error: xerrors.Errorf("deploy timeout ; %s", msg)})
	if err != nil {
		log.Errorf("setProjectTimeout %s send time out err:%s", id, err.Error())
	}
}

// startCheckDeployProgressesTimer Periodically gets asset pull progress
func (m *Manager) startCheckDeployProgressesTimer() {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()

	for {
		<-ticker.C
		m.retrieveNodeDeployProgresses()
	}
}

// RestartDeployProjects restarts deploy projects
func (m *Manager) RestartDeployProjects(ids []string) error {
	for _, id := range ids {
		if exist, _ := m.projectStateMachines.Has(ProjectID(id)); !exist {
			continue
		}

		err := m.projectStateMachines.Send(ProjectID(id), ProjectRestart{})
		if err != nil {
			log.Errorf("RestartDeployProjects send err:%s", err.Error())
		}
	}

	return nil
}
