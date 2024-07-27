package node

import (
	"errors"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/l5"
	"github.com/Filecoin-Titan/titan/node/repo"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
)

func L5(out *api.L5) Option {
	return Options(
		ApplyIf(func(s *Settings) bool { return s.Config },
			Error(errors.New("the L5 option must be set before Config option")),
		),

		func(s *Settings) error {
			s.nodeType = repo.L5
			return nil
		},

		func(s *Settings) error {
			resAPI := &l5.L5{}
			s.invokes[ExtractAPIKey] = fx.Populate(resAPI)
			*out = resAPI
			return nil
		},
	)
}

func ConfigL5(c interface{}) Option {
	cfg, ok := c.(*config.L5Cfg)
	if !ok {
		return Error(xerrors.Errorf("invalid config from repo, got: %T", c))
	}
	log.Info("start to config L5")

	return Options(
		Override(new(*config.L5Cfg), cfg),
	)
}
