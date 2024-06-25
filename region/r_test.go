package region

import (
	"testing"
)

func TestXxx(t *testing.T) {
	region, err := InitGeoLite("./city.mmdb")
	if err != nil {
		t.Errorf(" InitGeoLite: %s", err.Error())
		return
	}

	info, err := region.GetGeoInfo("47.242.3.255")
	if err != nil {
		t.Errorf(" GetGeoInfo: %s", err.Error())
		return
	}

	t.Logf("info: %s", info.Geo)
}
