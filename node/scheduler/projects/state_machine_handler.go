package projects

import (
	"context"
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/filecoin-project/go-statemachine"
	xerrors "golang.org/x/xerrors"
)

var (
	// MinRetryTime defines the minimum time duration between retries
	MinRetryTime = 1 * time.Minute

	WaitTime = 5 * time.Second

	// MaxRetryCount defines the maximum number of retries allowed
	MaxRetryCount = 1
)

// failedCoolDown is called when a retry needs to be attempted and waits for the specified time duration
func failedCoolDown(ctx statemachine.Context, info ProjectInfo, t time.Duration) error {
	retryStart := time.Now().Add(t)
	if time.Now().Before(retryStart) {
		log.Debugf("%s(%s), waiting %s before retrying", info.State, info.UUID, time.Until(retryStart))
		select {
		case <-time.After(time.Until(retryStart)):
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		}
	}

	return nil
}

// handleCreate handles the selection nodes for project
func (m *Manager) handleCreate(ctx statemachine.Context, info ProjectInfo) error {
	log.Debugf("handle Create : %s", info.UUID)

	curCount := int64(0)

	// select nodes
	list := m.nodeMgr.GetAllEdgeNode()
	for _, node := range list {
		if curCount >= info.Replicas {
			break
		}

		if node == nil || !node.IsProjectNode {
			continue
		}

		status := types.ProjectReplicaStatusStarting
		// node.API.d
		err := node.Deploy(context.Background(), &types.Project{ID: info.UUID.String(), Name: info.Name, BundleURL: info.BundleURL})
		if err != nil {
			log.Errorf("DeployProject Deploy %s err:%s", node.NodeID, err.Error())
			status = types.ProjectReplicaStatusError
		}

		err = m.SaveProjectReplicasInfo(&types.ProjectReplicas{
			Id:     info.UUID.String(),
			NodeID: node.NodeID,
			Status: status,
		})
		if err != nil {
			log.Errorf("DeployProject SaveWorkerdDetailsInfo %s err:%s", node.NodeID, err.Error())
			continue
		}

		if status == types.ProjectReplicaStatusStarting {
			curCount++
		}
	}

	if curCount == 0 {
		return ctx.Send(CreateFailed{error: xerrors.New("node not found; ")})
	}

	m.startProjectTimeoutCounting(info.UUID.String(), 0)

	return ctx.Send(DeployRequestSent{})
}

// handleDeploying handles the project deploying process of seed nodes
func (m *Manager) handleDeploying(ctx statemachine.Context, info ProjectInfo) error {
	log.Debugf("handle deploying, %s", info.UUID)

	if info.EdgeWaitings > 0 {
		return nil
	}

	if int64(len(info.EdgeReplicaSucceeds)) >= info.Replicas {
		return ctx.Send(DeploySucceed{})
	}

	if info.EdgeWaitings == 0 {
		return ctx.Send(DeployFailed{error: xerrors.New("node deploying failed")})
	}

	return nil
}

// handleServicing project pull completed and in service status
func (m *Manager) handleServicing(ctx statemachine.Context, info ProjectInfo) error {
	log.Infof("handle servicing: %s", info.UUID)
	m.stopProjectTimeoutCounting(info.UUID.String())

	// remove fail replicas
	// return m.DeleteUnfinishedReplicas(info.Hash.String())
	return nil
}

// handleDeploysFailed handles the failed state of project pulling and retries if necessary
func (m *Manager) handleDeploysFailed(ctx statemachine.Context, info ProjectInfo) error {
	m.stopProjectTimeoutCounting(info.UUID.String())

	if info.RetryCount >= int64(MaxRetryCount) {
		log.Infof("handle pulls failed: %s, retry count: %d", info.UUID, info.RetryCount)

		return nil
	}

	log.Debugf(": %s, retries: %d", info.UUID, info.RetryCount)

	if err := failedCoolDown(ctx, info, MinRetryTime); err != nil {
		return err
	}

	return ctx.Send(ProjectRedeploy{})
}

func (m *Manager) handleRemove(ctx statemachine.Context, info ProjectInfo) error {
	// log.Infof("handle remove: %s", info.Hash)
	m.stopProjectTimeoutCounting(info.UUID.String())

	list, err := m.LoadProjectReplicasInfos(info.UUID.String())
	if err != nil {
		return err
	}

	for _, info := range list {
		// request nodes
		m.removeReplica(info.Id, info.NodeID)
	}

	return m.DeleteProjectInfo(m.nodeMgr.ServerID, info.UUID.String())
}
