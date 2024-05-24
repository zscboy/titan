package projects

import (
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/terrors"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/google/uuid"
	xerrors "golang.org/x/xerrors"
)

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

		if info.Status == types.ProjectReplicaStatusError {
			log.Infof("UpdateStatus  %s,%s err:%s", nodeID, info.ID, info.Msg)
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
		State:       NodeSelect.String(),
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
		State: NodeSelect,
	}

	// create project task
	err = m.projectStateMachines.Send(ProjectID(info.UUID), rInfo)
	if err != nil {
		return "", &api.ErrWeb{Code: terrors.NotFound.Int(), Message: err.Error()}
	}

	return uid, nil
}

func (m *Manager) Update(req *types.ProjectReq) error {
	if req.UUID == "" {
		return xerrors.New("UUID is nil")
	}

	err := m.UpdateProjectInfo(&types.ProjectInfo{
		UUID:      req.UUID,
		Name:      req.Name,
		BundleURL: req.BundleURL,
		ServerID:  m.nodeMgr.ServerID,
		Replicas:  req.Replicas,
	})
	if err != nil {
		return err
	}

	rInfo := ProjectForceState{
		State: Update,
	}

	// create project task
	err = m.projectStateMachines.Send(ProjectID(req.UUID), rInfo)
	if err != nil {
		return &api.ErrWeb{Code: terrors.NotFound.Int(), Message: err.Error()}
	}

	return nil
}

func (m *Manager) Delete(req *types.ProjectReq) error {
	if req.UUID == "" {
		return xerrors.New("UUID is nil")
	}

	if req.NodeID != "" {
		return m.removeReplica(req.UUID, req.NodeID)
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

		dInfo.WsURL = vNode.WsURL()
	}

	info.DetailsList = list

	return info, nil
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
