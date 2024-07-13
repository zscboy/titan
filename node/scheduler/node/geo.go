package node

import (
	"strings"
	"sync"

	"github.com/Filecoin-Titan/titan/api/types"
)

const (
	unknown = "unknown"
)

// GeoMgr node geo info manager
type GeoMgr struct {
	edgeGeoMap map[string]map[string]map[string]map[string][]*types.NodeInfo
	edgeLock   sync.Mutex

	candidateGeoMap map[string]map[string]map[string]map[string][]*types.NodeInfo
	candidateLock   sync.Mutex
}

func newMgr() *GeoMgr {
	return &GeoMgr{
		edgeGeoMap:      make(map[string]map[string]map[string]map[string][]*types.NodeInfo),
		candidateGeoMap: make(map[string]map[string]map[string]map[string][]*types.NodeInfo),
	}
}

// AddEdgeNode add edge to map
func (g *GeoMgr) AddEdgeNode(continent, country, province, city string, nodeInfo *types.NodeInfo) {
	g.edgeLock.Lock()
	defer g.edgeLock.Unlock()

	if g.edgeGeoMap[continent] == nil {
		g.edgeGeoMap[continent] = make(map[string]map[string]map[string][]*types.NodeInfo)
	}
	if g.edgeGeoMap[continent][country] == nil {
		g.edgeGeoMap[continent][country] = make(map[string]map[string][]*types.NodeInfo)
	}
	if g.edgeGeoMap[continent][country][province] == nil {
		g.edgeGeoMap[continent][country][province] = make(map[string][]*types.NodeInfo, 0)
	}
	g.edgeGeoMap[continent][country][province][city] = append(g.edgeGeoMap[continent][country][province][city], nodeInfo)
}

// RemoveEdgeNode remove edge from map
func (g *GeoMgr) RemoveEdgeNode(continent, country, province, city, nodeID string) {
	g.edgeLock.Lock()
	defer g.edgeLock.Unlock()

	nodes := g.edgeGeoMap[continent][country][province][city]
	for i, nodeInfo := range nodes {
		if nodeInfo.NodeID == nodeID {
			g.edgeGeoMap[continent][country][province][city] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}
}

// FindEdgeNodes find edge from map
func (g *GeoMgr) FindEdgeNodes(continent, country, province, city string) []*types.NodeInfo {
	g.edgeLock.Lock()
	defer g.edgeLock.Unlock()

	continent = strings.ToLower(continent)
	country = strings.ToLower(country)
	province = strings.ToLower(province)
	city = strings.ToLower(city)

	if continent != "" && country != "" && province != "" && city != "" {
		return g.edgeGeoMap[continent][country][province][city]
	} else if continent != "" && country != "" && province != "" {
		var result []*types.NodeInfo
		for _, cities := range g.edgeGeoMap[continent][country][province] {
			result = append(result, cities...)
		}
		return result
	} else if continent != "" && country != "" {
		var result []*types.NodeInfo
		for _, provinces := range g.edgeGeoMap[continent][country] {
			for _, cities := range provinces {
				result = append(result, cities...)
			}
		}
		return result
	} else if continent != "" {
		var result []*types.NodeInfo
		for _, countries := range g.edgeGeoMap[continent] {
			for _, provinces := range countries {
				for _, cities := range provinces {
					result = append(result, cities...)
				}
			}
		}
		return result
	}

	return nil
}

// GetEdgeGeoKey get edge geo key
func (g *GeoMgr) GetEdgeGeoKey(continent, country, province string) map[string]int {
	g.edgeLock.Lock()
	defer g.edgeLock.Unlock()

	continent = strings.ToLower(continent)
	country = strings.ToLower(country)
	province = strings.ToLower(province)

	result := make(map[string]int)
	if continent != "" && country != "" && province != "" {
		for city, list := range g.edgeGeoMap[continent][country][province] {
			result[city] = len(list)
		}
		return result
	} else if continent != "" && country != "" {
		for province, cities := range g.edgeGeoMap[continent][country] {
			for _, list := range cities {
				result[province] += len(list)
			}
		}
		return result
	} else if continent != "" {
		for country, provinces := range g.edgeGeoMap[continent] {
			for _, cities := range provinces {
				for _, list := range cities {
					result[country] += len(list)
				}
			}
		}
		return result
	}

	for continent := range g.edgeGeoMap {
		for _, provinces := range g.edgeGeoMap[continent] {
			for _, cities := range provinces {
				for _, list := range cities {
					result[continent] += len(list)
				}
			}
		}
	}

	return result
}

// AddCandidateNode add candidate to map
func (g *GeoMgr) AddCandidateNode(continent, country, province, city string, nodeInfo *types.NodeInfo) {
	g.candidateLock.Lock()
	defer g.candidateLock.Unlock()

	if g.candidateGeoMap[continent] == nil {
		g.candidateGeoMap[continent] = make(map[string]map[string]map[string][]*types.NodeInfo)
	}
	if g.candidateGeoMap[continent][country] == nil {
		g.candidateGeoMap[continent][country] = make(map[string]map[string][]*types.NodeInfo)
	}
	if g.candidateGeoMap[continent][country][province] == nil {
		g.candidateGeoMap[continent][country][province] = make(map[string][]*types.NodeInfo, 0)
	}
	g.candidateGeoMap[continent][country][province][city] = append(g.candidateGeoMap[continent][country][province][city], nodeInfo)
}

// RemoveCandidateNode remove candidate from map
func (g *GeoMgr) RemoveCandidateNode(continent, country, province, city, nodeID string) {
	g.candidateLock.Lock()
	defer g.candidateLock.Unlock()

	nodes := g.candidateGeoMap[continent][country][province][city]
	for i, nodeInfo := range nodes {
		if nodeInfo.NodeID == nodeID {
			g.candidateGeoMap[continent][country][province][city] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}
}

// FindCandidateNodes find candidate from map
func (g *GeoMgr) FindCandidateNodes(continent, country, province, city string) []*types.NodeInfo {
	g.candidateLock.Lock()
	defer g.candidateLock.Unlock()

	continent = strings.ToLower(continent)
	country = strings.ToLower(country)
	province = strings.ToLower(province)
	city = strings.ToLower(city)

	if continent != "" && country != "" && province != "" && city != "" {
		result := g.candidateGeoMap[continent][country][province][city]
		if len(result) > 0 {
			return result
		}
	}

	if continent != "" && country != "" && province != "" {
		var result []*types.NodeInfo
		for _, cities := range g.candidateGeoMap[continent][country][province] {
			result = append(result, cities...)
		}

		if len(result) > 0 {
			return result
		}
	}

	if continent != "" && country != "" {
		var result []*types.NodeInfo
		for _, provinces := range g.candidateGeoMap[continent][country] {
			for _, cities := range provinces {
				result = append(result, cities...)
			}
		}

		if len(result) > 0 {
			return result
		}
	}

	if continent != "" {
		var result []*types.NodeInfo
		for _, countries := range g.candidateGeoMap[continent] {
			for _, provinces := range countries {
				for _, cities := range provinces {
					result = append(result, cities...)
				}
			}
		}

		if len(result) > 0 {
			return result
		}
	}

	return nil
}
