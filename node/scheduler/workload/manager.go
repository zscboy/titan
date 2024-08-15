package workload

import (
	"fmt"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/Filecoin-Titan/titan/node/scheduler/db"
	"github.com/Filecoin-Titan/titan/node/scheduler/leadership"
	"github.com/Filecoin-Titan/titan/node/scheduler/node"
	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("workload")

// Manager node workload
type Manager struct {
	config        dtypes.GetSchedulerConfigFunc
	leadershipMgr *leadership.Manager
	nodeMgr       *node.Manager
	*db.SQLDB

	resultQueue []*WorkloadResult
	resultLock  sync.Mutex
}

// NewManager return new node manager instance
func NewManager(sdb *db.SQLDB, configFunc dtypes.GetSchedulerConfigFunc, lmgr *leadership.Manager, nmgr *node.Manager) *Manager {
	manager := &Manager{
		config:        configFunc,
		leadershipMgr: lmgr,
		SQLDB:         sdb,
		nodeMgr:       nmgr,
		resultQueue:   make([]*WorkloadResult, 0),
	}

	go manager.handleResults()

	return manager
}

type WorkloadResult struct {
	data   *types.WorkloadRecordReq
	nodeID string
}

func (m *Manager) addWorkloadResult(r *WorkloadResult) {
	m.resultLock.Lock()
	defer m.resultLock.Unlock()

	m.resultQueue = append(m.resultQueue, r)
}

func (m *Manager) popWorkloadResult() *WorkloadResult {
	m.resultLock.Lock()
	defer m.resultLock.Unlock()

	if len(m.resultQueue) > 0 {
		out := m.resultQueue[0]
		m.resultQueue = m.resultQueue[1:]

		return out
	}

	return nil
}

func (m *Manager) PushResult(data *types.WorkloadRecordReq, nodeID string) error {
	log.Infof("workload PushResult nodeID:[%s] , %s\n", nodeID, data.WorkloadID)

	if nodeID == "" {
		return nil
	}

	m.addWorkloadResult(&WorkloadResult{data: data, nodeID: nodeID})
	// m.resultQueue <- &WorkloadResult{data: data, nodeID: nodeID}

	return nil
}

func (m *Manager) handleResults() {
	for {
		// result := <-m.resultQueue
		result := m.popWorkloadResult()
		if result != nil {
			m.handleClientWorkload(result.data, result.nodeID)
		} else {
			time.Sleep(time.Minute)
		}
	}
}

// handleClientWorkload handle node workload
func (m *Manager) handleClientWorkload(data *types.WorkloadRecordReq, nodeID string) error {
	if data.WorkloadID == "" {
		return nil
	}

	record, err := m.LoadWorkloadRecordOfID(data.WorkloadID)
	if err != nil {
		log.Errorf("handleClientWorkload LoadWorkloadRecordOfID %s error: %s", data.WorkloadID, err.Error())
		return err
	}

	if record.Status != types.WorkloadStatusCreate {
		return nil
	}

	downloadTotalSize := int64(0)
	for _, dw := range data.Workloads {
		downloadTotalSize += dw.DownloadSize
	}

	if record == nil {
		log.Errorf("handleClientWorkload record is nil : %s, %s", data.AssetCID, nodeID)
		return nil
	}

	// update status
	record.Status = types.WorkloadStatusSucceeded
	err = m.UpdateWorkloadRecord(record, types.WorkloadStatusCreate)
	if err != nil {
		log.Errorf("handleClientWorkload UpdateWorkloadRecord error: %s", err.Error())
		return err
	}

	eventList := make([]*types.RetrieveEvent, 0)
	detailsList := make([]*types.ProfitDetails, 0)

	limit := int64(float64(record.AssetSize))

	for _, dw := range data.Workloads {
		// update node bandwidths
		speed := int64((float64(dw.DownloadSize) / float64(dw.CostTime)) * 1000)
		if speed > 0 {
			// m.nodeMgr.UpdateNodeBandwidths(dw.SourceID, 0, speed)
			m.nodeMgr.UpdateNodeBandwidths(record.ClientID, speed, 0)
		}

		// Only edge can get this reward
		node := m.nodeMgr.GetEdgeNode(dw.SourceID)
		if node == nil {
			continue
		}
		node.UploadTraffic += dw.DownloadSize

		dInfo := m.nodeMgr.GetNodeBePullProfitDetails(node, float64(dw.DownloadSize), "")
		if dInfo != nil {
			dInfo.CID = record.AssetCID
			dInfo.Note = fmt.Sprintf("%s,%s", dInfo.Note, record.WorkloadID)

			detailsList = append(detailsList, dInfo)
		}

		if dw.DownloadSize > limit {
			dw.DownloadSize = limit
		}

		retrieveEvent := &types.RetrieveEvent{
			CID:         record.AssetCID,
			TokenID:     uuid.NewString(),
			NodeID:      dw.SourceID,
			ClientID:    record.ClientID,
			Size:        dw.DownloadSize,
			CreatedTime: record.CreatedTime.Unix(),
			EndTime:     time.Now().Unix(),
		}
		eventList = append(eventList, retrieveEvent)
	}

	// Retrieve Event
	for _, data := range eventList {
		if err := m.SaveRetrieveEventInfo(data); err != nil {
			log.Errorf("handleClientWorkload SaveRetrieveEventInfo token:%s ,  error %s", record.WorkloadID, err.Error())
		}
	}

	err = m.nodeMgr.AddNodeProfits(detailsList)
	if err != nil {
		log.Errorf("AddNodeProfit err:%s", err.Error())
	}

	return nil
}
