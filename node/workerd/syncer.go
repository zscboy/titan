package workerd

import "github.com/Filecoin-Titan/titan/api"

type syncer struct {
	api api.Scheduler
}

func newSyncer(api api.Scheduler) *syncer {
	return &syncer{
		api: api,
	}
}
