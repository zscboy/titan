package node

import (
	"strings"
	"sync"

	"github.com/Filecoin-Titan/titan/api/types"
)

const (
	unknown = "unknown"
)

type GeoMgr struct {
	geoMap map[string]map[string]map[string]map[string][]*types.NodeInfo
	mu     sync.Mutex
}

func newMgr() *GeoMgr {
	return &GeoMgr{
		geoMap: make(map[string]map[string]map[string]map[string][]*types.NodeInfo),
	}
}

func (g *GeoMgr) AddNode(continent, country, province, city string, nodeInfo *types.NodeInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.geoMap[continent] == nil {
		g.geoMap[continent] = make(map[string]map[string]map[string][]*types.NodeInfo)
	}
	if g.geoMap[continent][country] == nil {
		g.geoMap[continent][country] = make(map[string]map[string][]*types.NodeInfo)
	}
	if g.geoMap[continent][country][province] == nil {
		g.geoMap[continent][country][province] = make(map[string][]*types.NodeInfo, 0)
	}
	g.geoMap[continent][country][province][city] = append(g.geoMap[continent][country][province][city], nodeInfo)
}

func (g *GeoMgr) RemoveNode(continent, country, province, city, nodeID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	nodes := g.geoMap[continent][country][province][city]
	for i, nodeInfo := range nodes {
		if nodeInfo.NodeID == nodeID {
			g.geoMap[continent][country][province][city] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}
}

func (g *GeoMgr) FindNodes(continent, country, province, city string) []*types.NodeInfo {
	g.mu.Lock()
	defer g.mu.Unlock()

	continent = strings.ToLower(continent)
	country = strings.ToLower(country)
	province = strings.ToLower(province)
	city = strings.ToLower(city)

	if continent != "" && country != "" && province != "" && city != "" {
		return g.geoMap[continent][country][province][city]
	} else if continent != "" && country != "" && province != "" {
		var result []*types.NodeInfo
		for _, cities := range g.geoMap[continent][country][province] {
			result = append(result, cities...)
		}
		return result
	} else if continent != "" && country != "" {
		var result []*types.NodeInfo
		for _, provinces := range g.geoMap[continent][country] {
			for _, cities := range provinces {
				result = append(result, cities...)
			}
		}
		return result
	} else if continent != "" {
		var result []*types.NodeInfo
		for _, countries := range g.geoMap[continent] {
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

func (g *GeoMgr) GetGeoKey(continent, country, province string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	continent = strings.ToLower(continent)
	country = strings.ToLower(country)
	province = strings.ToLower(province)

	var result []string
	if continent != "" && country != "" && province != "" {
		for city := range g.geoMap[continent][country][province] {
			result = append(result, city)
		}
		return result
	} else if continent != "" && country != "" {
		for province := range g.geoMap[continent][country] {
			result = append(result, province)
		}
		return result
	} else if continent != "" {
		for country := range g.geoMap[continent] {
			result = append(result, country)
		}
		return result
	}

	for continent := range g.geoMap {
		result = append(result, continent)
	}

	return result
}
