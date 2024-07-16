package node

import "sync"

// IPMgr node ip info manager
type IPMgr struct {
	ipLimit   int
	nodeIPs   map[string][]string
	ipMapLock *sync.RWMutex
}

func newIPMgr(limit int) *IPMgr {
	return &IPMgr{
		ipLimit:   limit,
		ipMapLock: new(sync.RWMutex),
		nodeIPs:   make(map[string][]string),
	}
}

// StoreNodeIP store node
func (m *IPMgr) StoreNodeIP(nodeID, ip string) bool {
	m.ipMapLock.Lock()
	defer m.ipMapLock.Unlock()

	list, exist := m.nodeIPs[ip]
	if exist {
		for _, nID := range list {
			if nID == nodeID {
				return true
			}
		}

		if len(list) < m.ipLimit {
			list = append(list, nodeID)
			m.nodeIPs[ip] = list
			return true
		}

		return false
	}

	m.nodeIPs[ip] = []string{nodeID}
	return true
}

// RemoveNodeIP remove node
func (m *IPMgr) RemoveNodeIP(nodeID, ip string) {
	m.ipMapLock.Lock()
	defer m.ipMapLock.Unlock()

	list, exist := m.nodeIPs[ip]
	if exist {
		nList := []string{}

		for _, nID := range list {
			if nID != nodeID {
				nList = append(nList, nID)
			}
		}

		m.nodeIPs[ip] = nList
	}
}

// GetNodeOfIP get node
func (m *IPMgr) GetNodeOfIP(ip string) []string {
	return m.nodeIPs[ip]
}

// CheckIPExist check node
func (m *IPMgr) CheckIPExist(ip string) bool {
	_, exist := m.nodeIPs[ip]

	return exist
}
