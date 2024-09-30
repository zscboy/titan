package node

import (
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
)

// startNodeKeepaliveTimer periodically sends keepalive requests to all nodes and checks if any nodes have been offline for too long
func (m *Manager) startNodeKeepaliveTimer() {
	// start := time.Now()

	// offset := (time.Minute - time.Duration(start.Second())*time.Second - time.Duration(start.Nanosecond())) + (time.Minute * 10)
	// time.Sleep(offset)
	time.Sleep(penaltyFreeTime)

	ticker := time.NewTicker(keepaliveTime)
	defer ticker.Stop()

	minute := 10 // penalty free time

	for {
		<-ticker.C

		m.nodesKeepalive(minute)
		minute = 1
	}
}

// nodesKeepalive checks all nodes in the manager's lists for keepalive
func (m *Manager) nodesKeepalive(minute int) {
	now := time.Now()
	t := now.Add(-keepaliveTime)
	timeWindow := (minute * 60) / 5

	// date := now.Format("2006-01-02")
	// date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// nodes := []string{
	// 	string(m.ServerID),
	// }
	m.serverTodayOnlineTimeWindow += timeWindow

	eList := m.GetAllEdgeNode()
	for _, node := range eList {
		if m.checkNodeStatus(node, t) {
			node.OnlineDuration += minute
			node.TodayOnlineTimeWindow += timeWindow
			// nodes = append(nodes, node.NodeID)
		}
	}
	_, cList := m.GetAllCandidateNodes()
	for _, node := range cList {
		if m.checkNodeStatus(node, t) {
			node.OnlineDuration += minute
			node.TodayOnlineTimeWindow += timeWindow
			// nodes = append(nodes, node.NodeID)

		}
	}
	l3List := m.GetAllL3Node()
	for _, node := range l3List {
		if m.checkNodeStatus(node, t) {
			node.OnlineDuration += minute
			node.TodayOnlineTimeWindow += timeWindow
			// nodes = append(nodes, node.NodeID)
		}
	}
	l5List := m.GetAllL5Node()
	for _, node := range l5List {
		if m.checkNodeStatus(node, t) {
			node.OnlineDuration += minute
			node.TodayOnlineTimeWindow += timeWindow
			// nodes = append(nodes, node.NodeID)
		}
	}

	// if len(nodes) > 0 {
	// 	err := m.UpdateOnlineCount(nodes, timeWindow, date)
	// 	if err != nil {
	// 		log.Errorf("UpdateNodeInfos err:%s", err.Error())
	// 	}
	// }
}

func (m *Manager) SetNodeOffline(node *Node) {
	m.IPMgr.RemoveNodeIP(node.NodeID, node.ExternalIP)
	m.GeoMgr.RemoveNodeGeo(node.NodeID, node.Type, node.AreaID)

	if node.Type == types.NodeCandidate {
		m.deleteCandidateNode(node)
	} else if node.Type == types.NodeEdge {
		m.deleteEdgeNode(node)
	} else if node.Type == types.NodeL5 {
		m.deleteL5Node(node)
	} else if node.Type == types.NodeL3 {
		m.deleteL3Node(node)
	}

	log.Infof("node offline %s, %s", node.NodeID, node.ExternalIP)
}

// checkNodeStatus checks if a node has sent a keepalive recently and updates node status accordingly
func (m *Manager) checkNodeStatus(node *Node, t time.Time) bool {
	lastTime := node.LastRequestTime()

	if !lastTime.After(t) {
		m.SetNodeOffline(node)

		return false
	}

	return true
}
