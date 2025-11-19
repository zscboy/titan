package node

import (
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
)

func (m *Manager) startNodePenaltyTimer() {
	time.Sleep(penaltyFreeTime)

	ticker := time.NewTicker(penaltyInterval)
	defer ticker.Stop()

	for {
		<-ticker.C

		// reset
		now := time.Now()
		if now.Hour() == 0 && now.Minute() < 1 {
			m.candidateOfflineTime = make(map[string]int)
		}

		m.penaltyNode()
	}
}

func (m *Manager) penaltyNode() {
	list, err := m.LoadCandidateInfos()
	if err != nil {
		log.Errorf("LoadCandidateInfos err:%s", err.Error())
		return
	}

	offlineNodes := make(map[string]float64)
	detailsList := make([]*types.ProfitDetails, 0)

	for _, info := range list {
		if m.GetNode(info.NodeID) != nil {
			continue
		}

		if info.DeactivateTime > 0 {
			continue
		}

		if info.Profit <= 0.0001 {
			continue
		}

		// No penalty for the first 30 minutes of each day
		count := m.candidateOfflineTime[info.NodeID]
		if count > 30 {
			dInfo := m.CalculatePenalty(info.NodeID, info.Profit, (max(info.OfflineDuration+1-info.FreeDeductionTime, 0)), info.OnlineDuration)
			if dInfo != nil {
				detailsList = append(detailsList, dInfo)
			}
		}

		offlineNodes[info.NodeID] = 0
		m.candidateOfflineTime[info.NodeID]++
	}

	if len(offlineNodes) > 0 {
		err := m.UpdateNodePenalty(offlineNodes)
		if err != nil {
			log.Errorf("UpdateNodePenalty err:%s", err.Error())
		}
	}

	err = m.AddNodeProfitDetails(detailsList)
	if err != nil {
		log.Errorf("AddNodeProfit err:%s", err.Error())
	}
}
