package main

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net"
	"path"
	"strings"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/types"
	lcli "github.com/Filecoin-Titan/titan/cli"
	"github.com/Filecoin-Titan/titan/metrics"
	"github.com/Filecoin-Titan/titan/node"
	"github.com/Filecoin-Titan/titan/node/asset"
	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/edge/clib"
	"github.com/Filecoin-Titan/titan/node/httpserver"
	"github.com/Filecoin-Titan/titan/node/modules"
	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/Filecoin-Titan/titan/node/repo"
	"github.com/Filecoin-Titan/titan/node/tunnel"
	"github.com/Filecoin-Titan/titan/node/validation"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/quic-go/quic-go"
	"go.opencensus.io/stats/view"
	"golang.org/x/xerrors"
)

type edgeNode struct {
	ID           string
	httpServer   *httpserver.HttpServer
	transport    *quic.Transport
	edgeAPI      api.Edge
	edgeConfig   *config.EdgeCfg
	schedulerAPI api.Scheduler
	privateKey   *rsa.PrivateKey

	repoPath   string
	locatorURL string

	stop           node.StopFunc
	closeScheduler jsonrpc.ClientCloser

	shutdownChan    chan struct{} // shutdown chan
	restartChan     chan struct{} // cli restart
	restartDoneChan chan struct{} // make sure all modules are ready to start
}

func newNode(ctx context.Context, repoPath, locatorURL string) (*edgeNode, error) {
	log.Info("Starting titan edge node")

	// Register all metric views
	if err := view.Register(
		metrics.DefaultViews...,
	); err != nil {
		log.Fatalf("Cannot register the view: %v", err)
	}

	// repoPath := cctx.String(FlagEdgeRepo)

	r, err := openRepoOrNew(repoPath)
	if err != nil {
		return nil, err
	}

	lr, err := r.Lock(repo.Edge)
	if err != nil {
		return nil, err
	}

	_, nodeIDErr := r.NodeID()
	if nodeIDErr == repo.ErrNodeIDNotExist {
		if len(locatorURL) == 0 {
			return nil, fmt.Errorf("Must set --url")
		}
		if err := lcli.RegisterEdgeNode(lr, locatorURL); err != nil {
			return nil, err
		}
	}

	cfg, err := lr.Config()
	if err != nil {
		return nil, err
	}

	edgeCfg := cfg.(*config.EdgeCfg)

	err = lr.Close()
	if err != nil {
		return nil, err
	}

	privateKey, err := loadPrivateKey(r)
	if err != nil {
		return nil, fmt.Errorf(`please initialize edge, example: 
		titan-edge daemon start --init --url https://titan-network-url/rpc/v0`)
	}

	nodeIDBuf, err := r.NodeID()
	if err != nil {
		return nil, err
	}
	nodeID := string(nodeIDBuf)

	connectTimeout, err := time.ParseDuration(edgeCfg.Network.Timeout)
	if err != nil {
		return nil, err
	}

	udpPacketConn, err := net.ListenPacket("udp", edgeCfg.Network.ListenAddress)
	if err != nil {
		return nil, err
	}

	transport := &quic.Transport{
		Conn: udpPacketConn,
	}

	schedulerURL, err := getAccessPoint(edgeCfg.Network.LocatorURL, nodeID, edgeCfg.AreaID)
	if err != nil {
		return nil, err
	}

	schedulerAPI, closeScheduler, err := newSchedulerAPI(transport, schedulerURL, nodeID, privateKey)
	if err != nil {
		return nil, xerrors.Errorf("new scheduler api: %w", err)
	}

	// ctx := lcli.ReqContext(cctx)
	// ctx, cancel := context.WithCancel(ctx)
	// defer cancel()

	v, err := getSchedulerVersion(schedulerAPI, connectTimeout)
	if err != nil {
		return nil, err
	}

	if v.APIVersion != api.SchedulerAPIVersion0 {
		return nil, xerrors.Errorf("titan-scheduler API version doesn't match: expected: %s", api.APIVersion{APIVersion: api.SchedulerAPIVersion0})
	}
	log.Infof("Remote version %s", v)

	var (
		shutdownChan    = make(chan struct{}) // shutdown chan
		restartChan     = make(chan struct{}) // cli restart
		restartDoneChan = make(chan struct{}) // make sure all modules are ready to start
	)

	var httpServer *httpserver.HttpServer
	var edgeAPI api.Edge

	stop, err := node.New(ctx,
		node.Edge(&edgeAPI),
		node.Base(),
		node.RepoCtx(ctx, r),
		node.Override(new(dtypes.NodeID), dtypes.NodeID(nodeID)),
		node.Override(new(api.Scheduler), schedulerAPI),
		node.Override(new(dtypes.ShutdownChan), shutdownChan),
		node.Override(new(dtypes.RestartChan), restartChan),
		node.Override(new(dtypes.RestartDoneChan), restartDoneChan),
		node.Override(new(*quic.Transport), transport),
		node.Override(new(*asset.Manager), modules.NewAssetsManager(ctx, &edgeCfg.Puller, edgeCfg.IPFSAPIURL)),

		node.Override(new(dtypes.NodeMetadataPath), func() dtypes.NodeMetadataPath {
			metadataPath := edgeCfg.Storage.Path
			if len(metadataPath) == 0 {
				metadataPath = path.Join(lr.Path(), DefaultStorageDir)
			}

			log.Debugf("metadataPath:%s", metadataPath)
			return dtypes.NodeMetadataPath(metadataPath)
		}),
		node.Override(new(dtypes.AssetsPaths), func() dtypes.AssetsPaths {
			assetsPaths := []string{path.Join(lr.Path(), DefaultStorageDir)}
			if len(edgeCfg.Storage.Path) > 0 {
				assetsPaths = []string{edgeCfg.Storage.Path}
			}

			log.Debugf("storage path:%#v", assetsPaths)
			return dtypes.AssetsPaths(assetsPaths)
		}),
		node.Override(new(dtypes.InternalIP), func() (dtypes.InternalIP, error) {
			schedulerAddr := strings.Split(schedulerURL, "/")
			conn, err := net.DialTimeout("tcp", schedulerAddr[2], connectTimeout)
			if err != nil {
				return "", err
			}

			defer conn.Close() //nolint:errcheck
			localAddr := conn.LocalAddr().(*net.TCPAddr)

			return dtypes.InternalIP(strings.Split(localAddr.IP.String(), ":")[0]), nil
		}),

		node.Override(node.RunGateway, func(assetMgr *asset.Manager, validation *validation.Validation, apiSecret *jwt.HMACSHA, limiter *types.RateLimiter) error {
			opts := &httpserver.HttpServerOptions{
				Asset: assetMgr, Scheduler: schedulerAPI,
				PrivateKey:          privateKey,
				Validation:          validation,
				APISecret:           apiSecret,
				MaxSizeOfUploadFile: edgeCfg.MaxSizeOfUploadFile,
				RateLimiter:         limiter,
			}
			httpServer = httpserver.NewHttpServer(opts)

			return err
		}),

		node.Override(node.SetApiEndpointKey, func(lr repo.LockedRepo) error {
			return setEndpointAPI(lr, edgeCfg.Network.ListenAddress)
		}),
		node.Override(new(api.Scheduler), func() api.Scheduler { return schedulerAPI }),

		node.Override(new(*tunnel.Services), func(scheduler api.Scheduler, nid dtypes.NodeID) *tunnel.Services {
			return tunnel.NewServices(ctx, scheduler, string(nid))
		}),
	)
	if err != nil {
		return nil, xerrors.Errorf("creating node: %w", err)
	}

	eNode := &edgeNode{
		ID:           nodeID,
		httpServer:   httpServer,
		transport:    transport,
		edgeAPI:      edgeAPI,
		schedulerAPI: schedulerAPI,
		edgeConfig:   edgeCfg,
		privateKey:   privateKey,

		repoPath:   repoPath,
		locatorURL: locatorURL,

		stop:           stop,
		closeScheduler: closeScheduler,

		shutdownChan:    shutdownChan,
		restartChan:     restartChan,
		restartDoneChan: restartDoneChan,
	}
	return eNode, nil
}

func (node *edgeNode) startServer(ctx context.Context, daemonSwitch *clib.DaemonSwitch) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	registShutdownSignal(node.shutdownChan)

	handler, httpSrv := buildSrvHandler(node.httpServer, node.edgeAPI, node.edgeConfig, node.schedulerAPI, node.privateKey)

	go startHTTP3Server(ctx, node.transport, handler, node.edgeConfig)

	go startHTTPServer(ctx, httpSrv, node.edgeConfig.Network)

	hbeatParams := heartbeatParams{
		shutdownChan: node.shutdownChan,
		edgeAPI:      node.edgeAPI,
		schedulerAPI: node.schedulerAPI,
		nodeID:       node.ID,
		daemonSwitch: daemonSwitch,
	}
	go heartbeat(ctx, hbeatParams)

	quitWg.Add(3)

	for {
		select {
		case <-node.shutdownChan:
			log.Warn("Shutting down...")
			cancel()

			err := node.stop(context.TODO()) //nolint:errcheck
			if err != nil {
				log.Errorf("stop err: %v", err)
			}

			quitWg.Wait()

			if err := node.transport.Conn.Close(); err != nil {
				log.Errorf("Close udpPacketConn: %s", err.Error())
			}

			log.Warn("Graceful shutdown successful")

			return nil

		case <-node.restartChan:
			log.Warn("Restarting ...")
			cancel()

			err := node.stop(context.TODO())
			if err != nil {
				log.Errorf("stop err: %v", err)
			}

			quitWg.Wait()

			if err := node.transport.Conn.Close(); err != nil {
				log.Errorf("Close udpPacketConn: %s", err.Error())
			}

			node.restartDoneChan <- struct{}{} // node/edge/impl.go
			ctx := context.Background()
			node, err = newNode(ctx, node.repoPath, node.locatorURL)
			if err != nil {
				return err
			}
			return node.startServer(ctx, daemonSwitch)
			// return daemonStart(context.Background(), daemonSwitch, node.repoPath, node.locatorURL)
		}
	}

}
