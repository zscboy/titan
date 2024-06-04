package node

import (
	"strings"
	"sync"
)

type GeoMgr struct {
	Continent map[string]map[string]map[string]map[string][]string
	mu        sync.Mutex
}

func newMgr() *GeoMgr {
	return &GeoMgr{
		Continent: make(map[string]map[string]map[string]map[string][]string),
	}
}

func (g *GeoMgr) AddNode(continent, country, province, city, nodeID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Continent[continent] == nil {
		g.Continent[continent] = make(map[string]map[string]map[string][]string)
	}
	if g.Continent[continent][country] == nil {
		g.Continent[continent][country] = make(map[string]map[string][]string)
	}
	if g.Continent[continent][country][province] == nil {
		g.Continent[continent][country][province] = make(map[string][]string, 0)
	}
	g.Continent[continent][country][province][city] = append(g.Continent[continent][country][province][city], nodeID)
}

func (g *GeoMgr) RemoveNode(continent, country, province, city, nodeID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	nodes := g.Continent[continent][country][province][city]
	for i, id := range nodes {
		if id == nodeID {
			g.Continent[continent][country][province][city] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}
}

func (g *GeoMgr) FindNodes(continent, country, province, city string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	continent = strings.ToLower(continent)
	country = strings.ToLower(country)
	province = strings.ToLower(province)
	city = strings.ToLower(city)

	if continent != "" && country != "" && province != "" && city != "" {
		return g.Continent[continent][country][province][city]
	} else if continent != "" && country != "" && province != "" {
		var result []string
		for _, cities := range g.Continent[continent][country][province] {
			result = append(result, cities...)
		}
		return result
	} else if continent != "" && country != "" {
		var result []string
		for _, provinces := range g.Continent[continent][country] {
			for _, cities := range provinces {
				result = append(result, cities...)
			}
		}
		return result
	} else if continent != "" {
		var result []string
		for _, countries := range g.Continent[continent] {
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
