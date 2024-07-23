package locator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/region"
	"github.com/golang/geo/s2"
)

const unknown = "unknown"

func getLatLngOfIP(ip string, rg region.Region) (float64, float64, error) {
	geoInfo, err := rg.GetGeoInfo(ip)
	if err != nil {
		return 0, 0, err
	}
	return geoInfo.Latitude, geoInfo.Longitude, nil
}

func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	p1 := s2.PointFromLatLng(s2.LatLngFromDegrees(lat1, lon1))
	p2 := s2.PointFromLatLng(s2.LatLngFromDegrees(lat2, lon2))

	distance := s2.ChordAngleBetweenPoints(p1, p2).Angle().Radians()

	distanceKm := distance * 6371.0

	return distanceKm
}

func calculateTwoIPDistance(ip1, ip2 string, rg region.Region) (float64, error) {
	lat1, lon1, err := getLatLngOfIP(ip1, rg)
	if err != nil {
		return 0, err
	}

	lat2, lon2, err := getLatLngOfIP(ip2, rg)
	if err != nil {
		return 0, err
	}

	distance := calculateDistance(lat1, lon1, lat2, lon2)
	return distance, nil
}

func getUserNearestIP(userIP string, ipList []string, reg region.Region) string {
	ipDistanceMap := make(map[string]float64)
	for _, ip := range ipList {
		distance, err := calculateTwoIPDistance(userIP, ip, reg)
		if err != nil {
			log.Errorf("calculate tow ip distance error %s", err.Error())
			continue
		}
		ipDistanceMap[ip] = distance
	}

	minDistance := math.MaxFloat64
	var nearestIP string
	for ip, distance := range ipDistanceMap {
		if distance < minDistance {
			minDistance = distance
			nearestIP = ip
		}
	}

	return nearestIP
}

type titanRegion struct {
	webAPI string
}

func NewRegion(config *config.LocatorCfg) region.Region {
	return &titanRegion{webAPI: config.WebGeoAPI}
}

func (tr *titanRegion) GetGeoInfo(ip string) (*region.GeoInfo, error) {
	if len(ip) == 0 {
		return nil, fmt.Errorf("ip can not empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s?ip=%s", tr.webAPI, ip)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if rsp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(rsp.Body)
		return nil, fmt.Errorf("status code %d, error %s", rsp.StatusCode, string(buf))
	}

	type Data struct {
		Latitude  string `json:"latitude"`
		Longitude string `json:"longitude"`
		IP        string `json:"ip"`
		Geo       string `json:""`

		Continent string `json:"continent"`
		Country   string `json:"country"`
		Province  string `json:"province"`
		City      string `json:"city"`
	}

	result := struct {
		Code int    `json:"code"`
		Err  int    `json:"err"`
		Msg  string `json:"msg"`
		Data *Data  `json:"data"`
	}{}

	body, err := io.ReadAll(rsp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("code: %d err: %s msg: %s", result.Code, result.Err, result.Msg)
	}

	if result.Data == nil {
		return nil, fmt.Errorf("can not get geo info for %s", ip)
	}

	latitude, _ := strconv.ParseFloat(result.Data.Latitude, 64)
	longitude, _ := strconv.ParseFloat(result.Data.Longitude, 64)
	geoInfo := &region.GeoInfo{
		Latitude:  latitude,
		Longitude: longitude,
		IP:        result.Data.IP,

		Continent: unknown,
		Country:   unknown,
		Province:  unknown,
		City:      unknown,
	}

	if len(result.Data.Continent) != 0 {
		geoInfo.Continent = strings.ToLower(strings.Replace(result.Data.Continent, " ", "", -1))
	}

	if len(result.Data.Country) != 0 {
		geoInfo.Country = strings.ToLower(strings.Replace(result.Data.Country, " ", "", -1))
	}

	if len(result.Data.Province) != 0 {
		geoInfo.Province = strings.ToLower(strings.Replace(result.Data.Province, " ", "", -1))
	}

	if len(result.Data.City) != 0 {
		geoInfo.City = strings.ToLower(strings.Replace(result.Data.City, " ", "", -1))
	}

	geoInfo.Geo = fmt.Sprintf("%s-%s-%s-%s", geoInfo.Continent, geoInfo.Country, geoInfo.Province, geoInfo.City)
	return geoInfo, nil
}

func (tr *titanRegion) GetGeoInfoFromAreaID(areaID string) (*region.GeoInfo, error) {
	if areaID == "" {
		return nil, fmt.Errorf("areaID is nil")
	}

	areaID = strings.Replace(areaID, " ", "", -1)
	geoInfo := &region.GeoInfo{
		Latitude:  0,
		Longitude: 0,
		Geo:       areaID,

		Continent: unknown,
		Country:   unknown,
		Province:  unknown,
		City:      unknown,
	}

	continent, country, province, city := region.DecodeAreaID(areaID)

	geoInfo.Continent = strings.ToLower(strings.Replace(continent, " ", "", -1))
	geoInfo.Country = strings.ToLower(strings.Replace(country, " ", "", -1))
	geoInfo.Province = strings.ToLower(strings.Replace(province, " ", "", -1))
	geoInfo.City = strings.ToLower(strings.Replace(city, " ", "", -1))

	return geoInfo, nil
}
