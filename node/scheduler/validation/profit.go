package validation

import (
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
)

const profitInterval = 30 * time.Minute

func (m *Manager) startProfitTime() {
	nextTick := time.Now().Truncate(profitInterval)
	if nextTick.Before(time.Now()) {
		nextTick = nextTick.Add(profitInterval)
	}

	initialDuration := time.Until(nextTick)

	timer := time.NewTimer(initialDuration)
	for {
		<-timer.C
		timer.Reset(profitInterval)

		m.computeNodeProfits()
	}
}

func (m *Manager) computeNodeProfits() {
	nodes := m.nodeMgr.GetAllEdgeNode()
	for _, node := range nodes {
		rsp, err := m.nodeMgr.LoadValidationResultInfos(node.NodeID, 10, 0)
		if err != nil || len(rsp.ValidationResultInfos) == 0 {
			log.Warnf("%s LoadValidationResultInfos err:%v", node.NodeID, err)
			continue
		}

		l := len(rsp.ValidationResultInfos)

		size := 0.0
		for _, info := range rsp.ValidationResultInfos {
			if info.Status == types.ValidationStatusCreate {
				continue
			}

			size += info.Bandwidth * float64(info.Duration)
		}

		size = size / float64(l)

		dInfo := m.nodeMgr.GetNodeValidatableProfitDetails(node, size)
		if dInfo != nil {
			err := m.nodeMgr.AddNodeProfit(dInfo)
			if err != nil {
				log.Errorf("updateResultInfo AddNodeProfit %s,%d, %.4f err:%s", dInfo.NodeID, dInfo.PType, dInfo.Profit, err.Error())
			}
		}
	}
}
