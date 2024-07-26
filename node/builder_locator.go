package node

import (
	"errors"
	"math/rand"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/locator"
	"github.com/Filecoin-Titan/titan/node/modules"
	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/Filecoin-Titan/titan/node/repo"
	"github.com/Filecoin-Titan/titan/region"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
)

func Locator(out *api.Locator) Option {
	return Options(
		ApplyIf(func(s *Settings) bool { return s.Config },
			Error(errors.New("the Locator option must be set before Config option")),
		),

		func(s *Settings) error {
			s.nodeType = repo.Locator
			return nil
		},

		func(s *Settings) error {
			resAPI := &locator.Locator{}
			s.invokes[ExtractAPIKey] = fx.Populate(resAPI)
			*out = resAPI
			return nil
		},
	)
}

func ConfigLocator(c interface{}) Option {
	cfg, ok := c.(*config.LocatorCfg)
	if !ok {
		return Error(xerrors.Errorf("invalid config from repo, got: %T", c))
	}
	log.Info("start to config locator")

	return Options(
		Override(new(*config.LocatorCfg), cfg),
		Override(new(dtypes.ServerID), modules.NewServerID),
		Override(new(region.Region), locator.NewRegion(cfg)),
		Override(new(locator.Storage), modules.NewLocatorStorage),
		Override(new(locator.SchedulerAPIMap), modules.NewSchedulerAPIMap),
		Override(new(dtypes.EtcdAddresses), func() dtypes.EtcdAddresses {
			return dtypes.EtcdAddresses(cfg.EtcdAddresses)
		}),
		Override(new(dtypes.GeoDBPath), func() dtypes.GeoDBPath {
			return dtypes.GeoDBPath(cfg.GeoDBPath)
		}),
		Override(new(*rand.Rand), rand.New(rand.NewSource(time.Now().Unix()))),
	)
}
