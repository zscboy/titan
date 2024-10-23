package workload

import (
	"fmt"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/cidutil"
	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/Filecoin-Titan/titan/node/scheduler/db"
	"github.com/Filecoin-Titan/titan/node/scheduler/leadership"
	"github.com/Filecoin-Titan/titan/node/scheduler/node"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("workload")

// Manager node workload
type Manager struct {
	config        dtypes.GetSchedulerConfigFunc
	leadershipMgr *leadership.Manager
	nodeMgr       *node.Manager
	*db.SQLDB

	resultQueue []*Result
	resultLock  sync.Mutex
}

// NewManager return new node manager instance
func NewManager(sdb *db.SQLDB, configFunc dtypes.GetSchedulerConfigFunc, lmgr *leadership.Manager, nmgr *node.Manager) *Manager {
	manager := &Manager{
		config:        configFunc,
		leadershipMgr: lmgr,
		SQLDB:         sdb,
		nodeMgr:       nmgr,
		resultQueue:   make([]*Result, 0),
	}

	go manager.handleResults()

	return manager
}

// Result represents the result of a workload operation.
type Result struct {
	data   *types.WorkloadRecordReq
	nodeID string
}

func (m *Manager) addWorkloadResult(r *Result) {
	m.resultLock.Lock()
	defer m.resultLock.Unlock()

	m.resultQueue = append(m.resultQueue, r)
}

func (m *Manager) popWorkloadResult() *Result {
	m.resultLock.Lock()
	defer m.resultLock.Unlock()

	if len(m.resultQueue) > 0 {
		out := m.resultQueue[0]
		m.resultQueue = m.resultQueue[1:]

		return out
	}

	return nil
}

// PushResult sends the result of a workload to the specified node.
func (m *Manager) PushResult(data *types.WorkloadRecordReq, nodeID string) error {
	log.Infof("workload PushResult nodeID:[%s] , %s [%s]\n", nodeID, data.WorkloadID, data.AssetCID)

	m.addWorkloadResult(&Result{data: data, nodeID: nodeID})
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
func (m *Manager) handleClientWorkload(data *types.WorkloadRecordReq, downloadNode string) error {
	if data.WorkloadID == "" {
		hash, err := cidutil.CIDToHash(data.AssetCID)
		if err != nil {
			return err
		}

		// L1 download
		for _, dw := range data.Workloads {
			speed := int64((float64(dw.DownloadSize) / float64(dw.CostTime)) * 1000)
			if speed > 0 {
				m.nodeMgr.UpdateNodeBandwidths(downloadNode, speed, 0)
			}

			event := &types.RetrieveEvent{
				Hash:     hash,
				NodeID:   dw.SourceID,
				ClientID: downloadNode,
				Size:     dw.DownloadSize,
				Status:   types.EventStatusSucceed,
				Speed:    speed,
			}

			if err := m.SaveRetrieveEventInfo(event, 1, 0); err != nil {
				log.Errorf("handleClientWorkload SaveRetrieveEventInfo  error %s", err.Error())
			}
		}

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

	// update status
	record.Status = types.WorkloadStatusSucceeded
	err = m.UpdateWorkloadRecord(record, types.WorkloadStatusCreate)
	if err != nil {
		log.Errorf("handleClientWorkload UpdateWorkloadRecord %s error: %s", data.WorkloadID, err.Error())
		return err
	}

	hash, err := cidutil.CIDToHash(record.AssetCID)
	if err != nil {
		return err
	}

	eventList := make([]*types.RetrieveEvent, 0)
	detailsList := make([]*types.ProfitDetails, 0)

	limit := int64(float64(record.AssetSize))

	for _, dw := range data.Workloads {
		// update node bandwidths
		speed := int64((float64(dw.DownloadSize) / float64(dw.CostTime)) * 1000)
		if speed > 0 {
			m.nodeMgr.UpdateNodeBandwidths(dw.SourceID, 0, speed)
			m.nodeMgr.UpdateNodeBandwidths(record.ClientID, speed, 0)
		}

		// Only edge can get this reward
		node := m.nodeMgr.GetNode(dw.SourceID)
		if node == nil {
			continue
		}
		node.UploadTraffic += dw.DownloadSize

		if downloadNode != "" && node.Type == types.NodeEdge {
			if dw.DownloadSize > limit {
				dw.DownloadSize = limit
			}

			dInfo := m.nodeMgr.GetNodeBePullProfitDetails(node, float64(dw.DownloadSize), "")
			if dInfo != nil {
				dInfo.CID = record.AssetCID
				dInfo.Note = fmt.Sprintf("%s,%s", dInfo.Note, record.WorkloadID)

				detailsList = append(detailsList, dInfo)
			}
		}

		retrieveEvent := &types.RetrieveEvent{
			Hash:     hash,
			NodeID:   dw.SourceID,
			ClientID: record.ClientID,
			Size:     dw.DownloadSize,
			Status:   types.EventStatusSucceed,
			Speed:    speed,
		}
		eventList = append(eventList, retrieveEvent)
	}

	// Retrieve Event
	for _, data := range eventList {
		if err := m.SaveRetrieveEventInfo(data, 1, 0); err != nil {
			log.Errorf("handleClientWorkload SaveRetrieveEventInfo token:%s ,  error %s", record.WorkloadID, err.Error())
		}
	}

	err = m.nodeMgr.AddNodeProfitDetails(detailsList)
	if err != nil {
		log.Errorf("AddNodeProfit err:%s", err.Error())
	}

	return nil
}
