package tunnel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/types"
)

const trafficStatIntervel = 10 * time.Minute

type TrafficStat struct {
	lock               sync.Mutex
	dataCountStartTime time.Time
	// upload data count of L2
	dataCountUp int64
	// download data count of L2
	dataCountDown int64
}

func (trafficStat *TrafficStat) countDataDown(count int) {
	trafficStat.lock.Lock()
	defer trafficStat.lock.Unlock()

	trafficStat.dataCountDown += int64(count)
}

func (trafficStat *TrafficStat) countDataUp(count int) {
	trafficStat.lock.Lock()
	defer trafficStat.lock.Unlock()

	trafficStat.dataCountUp += int64(count)
}

func (trafficStat *TrafficStat) setTimeToNow() {
	trafficStat.lock.Lock()
	defer trafficStat.lock.Unlock()

	trafficStat.dataCountStartTime = time.Now()
}

func (trafficStat *TrafficStat) clean() {
	trafficStat.lock.Lock()
	trafficStat.lock.Unlock()

	trafficStat.dataCountStartTime = time.Time{}
	trafficStat.dataCountDown = 0
	trafficStat.dataCountUp = 0
}

func (trafficStat *TrafficStat) total() int64 {
	trafficStat.lock.Lock()
	defer trafficStat.lock.Unlock()

	return trafficStat.dataCountUp + trafficStat.dataCountDown
}

func (trafficStat *TrafficStat) isTimeToSubmitProjectReport() bool {
	if !trafficStat.dataCountStartTime.IsZero() && time.Since(trafficStat.dataCountStartTime) > trafficStatIntervel && trafficStat.total() > 0 {
		return true
	}

	return false
}

func (trafficStat *TrafficStat) submitProjectReport(t *Tunnel, scheduler api.Scheduler, serviceID string) error {
	trafficStat.lock.Lock()
	req := &types.ProjectRecordReq{
		NodeID:            t.id,
		ProjectID:         serviceID,
		BandwidthUpSize:   float64(trafficStat.dataCountUp),
		BandwidthDownSize: float64(trafficStat.dataCountUp),
		StartTime:         trafficStat.dataCountStartTime,
		EndTime:           time.Now(),
	}

	trafficStat.dataCountUp = 0
	trafficStat.dataCountUp = 0
	trafficStat.dataCountStartTime = time.Now()

	trafficStat.lock.Unlock()

	if scheduler != nil {
		return scheduler.SubmitProjectReport(context.Background(), req)
	}
	return fmt.Errorf("scheduler == nil")
}
