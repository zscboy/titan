package node

import (
	"fmt"
	"time"

	"github.com/Filecoin-Titan/titan/api/types"
)

const (
	uploadTrafficProfit   = 220.0 // p/GB
	downloadTrafficProfit = 55.0  // p/GB

	mr = 1.0
	mo = 1.0

	phoneWeighting = 1.0
)

var (
	mx = 1.0

	year = time.Now().UTC().Year()

	week1Start = time.Date(year, time.April, 22, 8, 0, 0, 0, time.UTC)
	week1End   = time.Date(year, time.April, 28, 23, 59, 59, 0, time.UTC)
	week2Start = time.Date(year, time.April, 29, 0, 0, 0, 0, time.UTC)
	week2End   = time.Date(year, time.May, 5, 23, 59, 59, 0, time.UTC)
	week3Start = time.Date(year, time.May, 6, 0, 0, 0, 0, time.UTC)
	week3End   = time.Date(year, time.May, 12, 23, 59, 59, 0, time.UTC)
	week4Start = time.Date(year, time.May, 13, 0, 0, 0, 0, time.UTC)
	week4End   = time.Date(year, time.May, 19, 23, 59, 59, 0, time.UTC)
)

func updateMx() float64 {
	now := time.Now().UTC()

	log.Debugf("updateMx now : %s , %.2f", now.Format(time.DateTime), mx)

	switch {
	case now.After(week1Start) && now.Before(week1End):
		return 2
	case now.After(week2Start) && now.Before(week2End):
		return 1.7
	case now.After(week3Start) && now.Before(week3End):
		return 1.4
	case now.After(week4Start) && now.Before(week4End):
		return 1.2
	default:
		return 1
	}
}

func (m *Manager) startMxTimer() {
	ticker := time.NewTicker(time.Minute * 1)
	defer ticker.Stop()

	mx = updateMx()

	for {
		<-ticker.C
		mx = updateMx()
	}
}

// func (m *Manager) GetNodeBaseProfitDetails(node *Node, count float64) *types.ProfitDetails {
// 	p := count * m.NodeCalculateMCx()

// 	if p < 0.000001 {
// 		return nil
// 	}

// 	return &types.ProfitDetails{
// 		NodeID: node.NodeID,
// 		Profit: p,
// 		PType:  types.ProfitTypeBase,
// 	}
// }

func (m *Manager) GetNodePullProfitDetails(node *Node, size float64, note string) *types.ProfitDetails {
	w := 1.0
	if node.IsPhone {
		w = phoneWeighting
	}

	d := bToGB(size)
	mip := calculateMip(node.NATType)
	lip := len(m.GetNodeOfIP(node.ExternalIP))
	mn := calculateMn(lip)

	mbnd := mr * mx * mo * d * downloadTrafficProfit * mip * mn * w

	if mbnd < 0.000001 {
		return nil
	}

	return &types.ProfitDetails{
		NodeID: node.NodeID,
		Profit: mbnd,
		PType:  types.ProfitTypePull,
		Size:   int64(size),
		Note:   fmt.Sprintf("lip:[%d] ; mr:[%.4f], mx:[%.4f], mo:[%.4f], d:[%.6f]GB, [%.4f], mip:[%.4f], mn:[%.4f] ,w:[%.1f] ", lip, mr, mx, mo, d, downloadTrafficProfit, mip, mn, w),
	}
}

func (m *Manager) GetNodeBePullProfitDetails(node *Node, size float64, note string) *types.ProfitDetails {
	w := 1.0
	if node.IsPhone {
		w = phoneWeighting
	}

	u := bToGB(size)
	b := calculateB(node.BandwidthUp)
	mip := calculateMip(node.NATType)
	lip := len(m.GetNodeOfIP(node.ExternalIP))
	mn := calculateMn(lip)

	mbnu := mr * mx * mo * u * b * uploadTrafficProfit * mip * mn * w

	if mbnu < 0.000001 {
		return nil
	}

	return &types.ProfitDetails{
		NodeID: node.NodeID,
		Profit: mbnu,
		PType:  types.ProfitTypeBePull,
		Size:   int64(size),
		Note:   fmt.Sprintf("lip:[%d] BandwidthUp:[%d]; mr:[%.4f], mx:[%.4f], mo:[%.4f], u:[%.6f]GB, b:[%.4f], [%.4f], mip:[%.4f], mn:[%.4f],w:[%.1f]", lip, node.BandwidthUp, mr, mx, mo, u, b, uploadTrafficProfit, mip, mn, w),
	}
}

func (m *Manager) GetNodeValidatorProfitDetails(node *Node, size float64) *types.ProfitDetails {
	w := 1.0
	if node.IsPhone {
		w = phoneWeighting
	}

	d := bToGB(size)

	mip := calculateMip(node.NATType)
	lip := len(m.GetNodeOfIP(node.ExternalIP))
	mn := calculateMn(lip)

	mbnd := mr * mx * mo * d * downloadTrafficProfit * mip * mn * w

	if mbnd < 0.000001 {
		return nil
	}

	return &types.ProfitDetails{
		NodeID: node.NodeID,
		Profit: mbnd,
		PType:  types.ProfitTypeValidator,
		Size:   int64(size),
		Note:   fmt.Sprintf("lip:[%d] ; mr:[%.4f], mx:[%.4f], mo:[%.4f], d:[%.4f], [%.4f], mip:[%.4f], mn:[%.4f],w:[%.1f]", lip, mr, mx, mo, d, downloadTrafficProfit, mip, mn, w),
	}
}

func (m *Manager) GetNodeValidatableProfitDetails(node *Node, size float64) *types.ProfitDetails {
	w := 1.0
	if node.IsPhone {
		w = phoneWeighting
	}

	ds := float64(node.TitanDiskUsage)
	s := bToGB(ds)
	u := bToGB(size)
	b := calculateB(node.BandwidthUp)

	mt := 1.0

	mip := calculateMip(node.NATType)
	lip := len(m.GetNodeOfIP(node.ExternalIP))
	mn := calculateMn(lip)

	ms := mr * mx * mo * ((min(s, 2000) * 0.211148679 * mt) + (u * b * uploadTrafficProfit * mip * mn)) * w

	if ms < 0.000001 {
		return nil
	}

	return &types.ProfitDetails{
		NodeID: node.NodeID,
		Profit: ms,
		PType:  types.ProfitTypeValidatable,
		Size:   int64(size),
		Note:   fmt.Sprintf("lip:[%d] BandwidthUp:[%d]; mr:[%.4f], mx:[%.4f], mo:[%.4f], s:[%.4f], u:[%.6f]GB, b:[%.4f], [%.4f], mip:[%.4f], mn:[%.4f],w:[%.1f]", lip, node.BandwidthUp, mr, mx, mo, s, u, b, uploadTrafficProfit, mip, mn, w),
	}
}

// NodeCalculateMCx
func (m *Manager) NodeCalculateMCx(isPhone bool) float64 {
	w := 1.0
	if isPhone {
		w = phoneWeighting
	}

	b := 20.0 / 17280.0

	mcx := mr * mx * mo * b * w

	return mcx
}

func calculateMip(n string) float64 {
	switch n {
	case types.NatTypeNo.String():
		return 2
	case types.NatTypeFullCone.String():
		return 1.5
	case types.NatTypeRestricted.String():
		return 1.3
	case types.NatTypePortRestricted.String():
		return 1.1
	case types.NatTypeSymmetric.String():
		return 0.8
	}

	return 0.8
}

func calculateMn(ipNum int) float64 {
	switch ipNum {
	case 1:
		return 1.1
	case 2:
		return 0.5
	case 3:
		return 0.333333333
	case 4:
		return 0.25
	}

	return 0.2
}

func calculateB(upload int64) float64 {
	mb := bToMB(float64(upload))
	if mb >= 30 {
		return 1.2
	}

	if mb >= 5 {
		return 1
	}

	return 0.8
}
