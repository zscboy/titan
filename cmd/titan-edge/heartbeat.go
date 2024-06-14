package main

import (
	"context"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/terrors"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/node/edge/clib"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"golang.org/x/xerrors"
)

type heartbeatParams struct {
	shutdownChan chan struct{}
	edgeAPI      api.Edge
	schedulerAPI api.Scheduler
	nodeID       string
	daemonSwitch *clib.DaemonSwitch
}

func heartbeat(ctx context.Context, hbp heartbeatParams) error {
	schedulerSession, err := hbp.schedulerAPI.Session(ctx)
	if err != nil {
		return xerrors.Errorf("getting scheduler session: %w", err)
	}

	token, err := hbp.edgeAPI.AuthNew(ctx, &types.JWTPayload{Allow: []auth.Permission{api.RoleAdmin}, ID: hbp.nodeID})
	if err != nil {
		return xerrors.Errorf("generate token for scheduler error: %w", err)
	}

	heartbeats := time.NewTicker(HeartbeatInterval)
	defer heartbeats.Stop()
	// var isStop = false
	var readyCh chan struct{}
	for {
		// TODO: we could get rid of this, but that requires tracking resources for restarted tasks correctly
		if readyCh == nil {
			log.Info("Making sure no local tasks are running")
			readyCh = waitQuietCh(hbp.edgeAPI)
		}

		for {
			select {
			case <-readyCh:
				opts := &types.ConnectOptions{Token: token}
				if err := hbp.schedulerAPI.EdgeConnect(ctx, opts); err != nil {
					log.Errorf("Registering edge failed: %s", err.Error())
					hbp.shutdownChan <- struct{}{}
					return err
				}
				hbp.daemonSwitch.IsOnline = true
				log.Info("Edge registered successfully, waiting for tasks")
				readyCh = nil
			case <-heartbeats.C:
			case <-ctx.Done():
				quitWg.Done()
				log.Warn("heartbeat stopped")
				hbp.daemonSwitch.IsOnline = false
				hbp.daemonSwitch.IsStop = true
				return nil // graceful shutdown
			case isStop := <-hbp.daemonSwitch.StopChan:
				hbp.daemonSwitch.IsOnline = !isStop
				hbp.daemonSwitch.IsStop = isStop
				if isStop {
					log.Info("stop daemon")
				} else {
					log.Info("start daemon")
				}
			}

			if hbp.daemonSwitch.IsStop {
				continue
			}

			curSession, err := keepalive(hbp.schedulerAPI, 10*time.Second)
			if err != nil {
				log.Errorf("heartbeat: keepalive failed: %+v", err)
				errNode, ok := err.(*api.ErrNode)
				if ok {
					if errNode.Code == int(terrors.NodeDeactivate) {
						hbp.shutdownChan <- struct{}{}
						return nil
					} else if errNode.Code == int(terrors.NodeIPInconsistent) {
						break
					} else if errNode.Code == int(terrors.NodeOffline) && readyCh == nil {
						break
					}
				} else {
					hbp.daemonSwitch.IsOnline = false
				}
			} else if curSession != schedulerSession {
				log.Warn("change session id")
				schedulerSession = curSession
				break
			} else {
				hbp.daemonSwitch.IsOnline = true
			}

			// log.Infof("cur session id %s", curSession.String())
		}

		log.Errorf("TITAN-EDGE CONNECTION LOST")
	}
}
