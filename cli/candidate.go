package cli

import (
	"fmt"

	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/urfave/cli/v2"
)

var CandidateCmds = []*cli.Command{
	nodeInfoCmd,
	cacheStatCmd,
	progressCmd,
	keyCmds,
	configCmds,
	registerCmds,
}

var registerCmds = &cli.Command{
	Name:  "register",
	Usage: "register candidate to scheduler",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "locator-url",
			Usage: "--locator=https://titan-locator-domain/rpc/v0",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "scheduler-url",
			Usage: "--locator=https://titan-locator-domain/rpc/v0",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		locatorURL := cctx.String("locator-url")
		if len(locatorURL) == 0 {
			return fmt.Errorf("--locator-url can not empty")
		}

		schedulerURL := cctx.String("scheduler-url")
		if len(schedulerURL) == 0 {
			return fmt.Errorf("--scheduler-url can not empty")
		}

		_, lr, err := openRepoAndLock(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() //nolint:errcheck  // ignore error

		return RegisterNodeWithScheduler(lr, schedulerURL, locatorURL, types.NodeCandidate)
	},
}
