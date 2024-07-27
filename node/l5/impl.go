package l5

import (
	"context"

	"github.com/Filecoin-Titan/titan/node/common"
	"go.uber.org/fx"
)

// Candidate represents the c node.
type L5 struct {
	fx.In
	*common.CommonAPI
}

// WaitQuiet does nothing and returns nil error.
func (l5 *L5) WaitQuiet(ctx context.Context) error {
	log.Debug("WaitQuiet")
	return nil
}
