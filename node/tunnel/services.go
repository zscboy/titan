package tunnel

import (
	"context"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api"
)

type Service struct {
	ID      string
	Address string
	Port    int
}

type Services struct {
	scheduler api.Scheduler
	nodeID    string
	services  sync.Map
}

func NewServices(schedulerAPI api.Scheduler, nodeID string) *Services {
	s := &Services{scheduler: schedulerAPI, nodeID: nodeID, services: sync.Map{}}
	go s.start()
	return s
}

func (s *Services) start() {
	for {
		time.Sleep(3 * time.Second)

		rsp, err := s.scheduler.AssignTunserverURL(context.Background())
		if err != nil {
			log.Errorf("start service failed, can not get candidate for L2 %s", err.Error())
			continue
		}

		tunclient, err := newTunclient(rsp.URL, s.nodeID, s)
		if err != nil {
			log.Errorf("new tunclient failed, %s", err.Error())
			continue

		}

		if err := s.scheduler.UpdateTunserverURL(context.Background(), rsp.NodeID); err != nil {
			log.Errorf("UpdateTunserverURL failed %s", err.Error())
			continue
		}

		if err := tunclient.startService(); err != nil {
			log.Errorf("start service failed, can not get candidate for L2")
		}
	}
}

func (s *Services) Regiseter(service *Service) {
	s.services.Store(service.ID, service)
}

func (s *Services) Remove(service *Service) {
	s.services.Delete(service.ID)
}

func (s *Services) get(serviceID string) *Service {
	v, ok := s.services.Load(serviceID)
	if ok {
		return v.(*Service)
	}
	return nil
}
