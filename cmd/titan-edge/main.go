package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Filecoin-Titan/titan/node"
	"github.com/Filecoin-Titan/titan/node/asset"
	"github.com/Filecoin-Titan/titan/node/edge/clib"
	"github.com/Filecoin-Titan/titan/node/httpserver"
	"github.com/Filecoin-Titan/titan/node/modules"
	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/Filecoin-Titan/titan/node/tunnel"
	"github.com/Filecoin-Titan/titan/node/validation"
	"github.com/gbrlsnchs/jwt/v3"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/client"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/build"
	lcli "github.com/Filecoin-Titan/titan/cli"
	"github.com/Filecoin-Titan/titan/lib/titanlog"
	"github.com/Filecoin-Titan/titan/metrics"
	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/repo"
	titanrsa "github.com/Filecoin-Titan/titan/node/rsa"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"
)

var log = logging.Logger("main")

const (
	FlagEdgeRepo            = "edge-repo"
	FlagEdgeRepoDeprecation = "edgerepo"
	DefaultStorageDir       = "storage"
	WorkerdDir              = "workerd"
	HeartbeatInterval       = 10 * time.Second
)

func main() {
	types.RunningNodeType = types.NodeEdge
	titanlog.SetupLogLevels()
	local := []*cli.Command{
		daemonCmd,
	}

	local = append(local, lcli.CommonCommands...)

	app := &cli.App{
		Name:                 "titan-edge",
		Usage:                "Titan edge node",
		Version:              build.UserVersion(),
		EnableBashCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagEdgeRepo,
				Aliases: []string{FlagEdgeRepoDeprecation},
				EnvVars: []string{"TITAN_EDGE_PATH", "EDGE_PATH"},
				Value:   "~/.titanedge", // TODO: Consider XDG_DATA_HOME
				Usage:   fmt.Sprintf("Specify edge repo path. flag %s and env TITAN_EDGE_PATH are DEPRECATION, will REMOVE SOON", FlagEdgeRepoDeprecation),
			},
			&cli.StringFlag{
				Name:    "panic-reports",
				EnvVars: []string{"TITAN_PANIC_REPORT_PATH"},
				Hidden:  true,
				Value:   "~/.titanedge", // should follow --repo default
			},
		},

		After: func(c *cli.Context) error {
			if r := recover(); r != nil {
				// Generate report in TITAN_EDGE_PATH and re-raise panic
				build.GeneratePanicReport(c.String("panic-reports"), c.String(FlagEdgeRepo), c.App.Name)
				panic(r)
			}
			return nil
		},
		Commands: append(local, lcli.EdgeCmds...),
	}
	app.Setup()
	app.Metadata["repoType"] = repo.Edge

	if err := app.Run(os.Args); err != nil {
		log.Errorf("%+v", err)
		os.Exit(1)
	}
}

var daemonCmd = &cli.Command{
	Name:  "daemon",
	Usage: "Daemon commands",
	Subcommands: []*cli.Command{
		daemonStartCmd,
		daemonStopCmd,
		daemonRestartCmd,
		// daemonTestCmd,
	},
}

var daemonStopCmd = &cli.Command{
	Name:  "stop",
	Usage: "stop a running daemon",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		edgeAPI, close, err := lcli.GetEdgeAPI(cctx)
		if err != nil {
			return err
		}

		defer close()

		return edgeAPI.Shutdown(cctx.Context)
	},
}

var daemonRestartCmd = &cli.Command{
	Name:  "restart",
	Usage: "restart a running daemon from config",
	Action: func(cctx *cli.Context) error {
		edgeAPI, close, err := lcli.GetEdgeAPI(cctx)
		if err != nil {
			return err
		}

		defer close()

		return edgeAPI.Restart(context.Background())
	},
}

var daemonStartCmd = &cli.Command{
	Name:  "start",
	Usage: "Start titan edge node",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "init",
			Usage: "--init=true, initialize edge at first run",
			Value: false,
		},
		&cli.StringFlag{
			Name:  "url",
			Usage: "--url=https://titan-server-domain/rpc/v0",
			Value: "",
		},
	},

	Before: func(cctx *cli.Context) error {
		return nil
	},
	Action: func(cctx *cli.Context) error {
		repoPath := cctx.String(FlagEdgeRepo)
		locatorURL := cctx.String("url")
		ds := clib.DaemonSwitch{StopChan: make(chan bool)}
		return daemonStart(lcli.ReqContext(cctx), &ds, repoPath, locatorURL)
	},
}

func keepalive(api api.Scheduler, timeout time.Duration) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return api.NodeKeepaliveV2(ctx)
}

func getSchedulerVersion(api api.Scheduler, timeout time.Duration) (api.APIVersion, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return api.Version(ctx)
}

func newAuthTokenFromScheduler(schedulerURL, nodeID string, privateKey *rsa.PrivateKey) (string, error) {
	schedulerAPI, closer, err := client.NewScheduler(context.Background(), schedulerURL, nil, jsonrpc.WithHTTPClient(client.NewHTTP3Client()))
	if err != nil {
		return "", err
	}

	defer closer()

	rsa := titanrsa.New(crypto.SHA256, crypto.SHA256.New())
	sign, err := rsa.Sign(privateKey, []byte(nodeID))
	if err != nil {
		return "", err
	}

	return schedulerAPI.NodeLogin(context.Background(), nodeID, hex.EncodeToString(sign))
}

func getAccessPoint(locatorURL, nodeID, areaID string) (string, error) {
	locator, close, err := client.NewLocator(context.Background(), locatorURL, nil, jsonrpc.WithHTTPClient(client.NewHTTP3Client()))
	if err != nil {
		return "", err
	}
	defer close()

	schedulerURLs, err := locator.GetAccessPoints(context.Background(), nodeID, areaID)
	if err != nil {
		return "", err
	}

	if len(schedulerURLs) <= 0 {
		return "", fmt.Errorf("no access point in area %s for node %s", areaID, nodeID)
	}

	return schedulerURLs[0], nil
}

func newSchedulerAPI(transport *quic.Transport, schedulerURL, nodeID string, privateKey *rsa.PrivateKey) (api.Scheduler, jsonrpc.ClientCloser, error) {
	token, err := newAuthTokenFromScheduler(schedulerURL, nodeID, privateKey)
	if err != nil {
		return nil, nil, err
	}

	httpClient, err := client.NewHTTP3ClientWithPacketConn(transport)
	if err != nil {
		return nil, nil, err
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+token)
	headers.Add("Node-ID", nodeID)

	schedulerAPI, closer, err := client.NewScheduler(context.Background(), schedulerURL, headers, jsonrpc.WithHTTPClient(httpClient))
	if err != nil {
		return nil, nil, err
	}
	log.Debugf("scheduler url:%s, token:%s", schedulerURL, token)

	return schedulerAPI, closer, nil
}

func defaultTLSConfig() (*tls.Config, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		Certificates:       []tls.Certificate{tlsCert},
		NextProtos:         []string{"h2", "h3"},
		InsecureSkipVerify: true, //nolint:gosec // skip verify in default config
	}, nil
}

func loadPrivateKey(r repo.Repo) (*rsa.PrivateKey, error) {
	pem, err := r.PrivateKey()
	if err != nil {
		return nil, err
	}
	return titanrsa.Pem2PrivateKey(pem)
}

func setEndpointAPI(lr repo.LockedRepo, address string) error {
	a, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return xerrors.Errorf("parsing address: %w", err)
	}

	ma, err := manet.FromNetAddr(a)
	if err != nil {
		return xerrors.Errorf("creating api multiaddress: %w", err)
	}

	if err := lr.SetAPIEndpoint(ma); err != nil {
		return xerrors.Errorf("setting api endpoint: %w", err)
	}

	return nil
}

func openRepoOrNew(repoPath string) (repo.Repo, error) {
	r, err := repo.NewFS(repoPath)
	if err != nil {
		return nil, err
	}

	ok, err := r.Exists()
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := r.Init(repo.Edge); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func waitQuietCh(edgeAPI api.Edge) chan struct{} {
	out := make(chan struct{})
	go func() {
		ctx2 := context.Background()
		err := edgeAPI.WaitQuiet(ctx2)
		if err != nil {
			log.Errorf("wait quiet error %s", err.Error())
		}
		close(out)
	}()
	return out
}

var quitWg = &sync.WaitGroup{}

func daemonStart(ctx context.Context, daemonSwitch *clib.DaemonSwitch, repoPath, locatorURL string) error {
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
		return err
	}

	lr, err := r.Lock(repo.Edge)
	if err != nil {
		return err
	}

	_, nodeIDErr := r.NodeID()
	if nodeIDErr == repo.ErrNodeIDNotExist {
		if len(locatorURL) == 0 {
			return fmt.Errorf("Must set --url")
		}
		if err := lcli.RegisterEdgeNode(lr, locatorURL); err != nil {
			return err
		}
	}

	cfg, err := lr.Config()
	if err != nil {
		return err
	}

	edgeCfg := cfg.(*config.EdgeCfg)

	err = lr.Close()
	if err != nil {
		return err
	}

	privateKey, err := loadPrivateKey(r)
	if err != nil {
		return fmt.Errorf(`please initialize edge, example: 
		titan-edge daemon start --init --url https://titan-network-url/rpc/v0`)
	}

	nodeIDBuf, err := r.NodeID()
	if err != nil {
		return err
	}
	nodeID := string(nodeIDBuf)

	connectTimeout, err := time.ParseDuration(edgeCfg.Network.Timeout)
	if err != nil {
		return err
	}

	udpPacketConn, err := net.ListenPacket("udp", edgeCfg.Network.ListenAddress)
	if err != nil {
		return err
	}
	defer udpPacketConn.Close() //nolint:errcheck  // ignore error

	transport := &quic.Transport{
		Conn: udpPacketConn,
	}

	schedulerURL, err := getAccessPoint(edgeCfg.Network.LocatorURL, nodeID, edgeCfg.AreaID)
	if err != nil {
		return err
	}

	schedulerAPI, closer, err := newSchedulerAPI(transport, schedulerURL, nodeID, privateKey)
	if err != nil {
		return xerrors.Errorf("new scheduler api: %w", err)
	}
	defer closer()

	// ctx := lcli.ReqContext(cctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	v, err := getSchedulerVersion(schedulerAPI, connectTimeout)
	if err != nil {
		return err
	}

	if v.APIVersion != api.SchedulerAPIVersion0 {
		return xerrors.Errorf("titan-scheduler API version doesn't match: expected: %s", api.APIVersion{APIVersion: api.SchedulerAPIVersion0})
	}
	log.Infof("Remote version %s", v)

	var (
		shutdownChan    = make(chan struct{}) // shutdown chan
		restartChan     = make(chan struct{}) // cli restart
		restartDoneChan = make(chan struct{}) // make sure all modules are ready to start
	)

	var lockRepo repo.LockedRepo
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
			lockRepo = lr
			return setEndpointAPI(lr, edgeCfg.Network.ListenAddress)
		}),
		node.Override(new(api.Scheduler), func() api.Scheduler { return schedulerAPI }),

		node.Override(new(*tunnel.Services), func(scheduler api.Scheduler, nid dtypes.NodeID) *tunnel.Services {
			return tunnel.NewServices(ctx, scheduler, string(nid))
		}),
	)
	if err != nil {
		return xerrors.Errorf("creating node: %w", err)
	}

	registShutdownSignal(shutdownChan)

	handler, httpSrv := buildSrvHandler(httpServer, edgeAPI, edgeCfg, schedulerAPI, privateKey)

	go startHTTP3Server(ctx, transport, handler, edgeCfg)

	go startHTTPServer(ctx, httpSrv, edgeCfg.Network)

	go heartbeat(ctx, heartbeatParams{shutdownChan: shutdownChan, edgeAPI: edgeAPI, schedulerAPI: schedulerAPI, nodeID: nodeID, daemonSwitch: daemonSwitch})

	quitWg.Add(3)

	for {
		select {
		case <-shutdownChan:
			log.Warn("Shutting down...")
			cancel()

			if err := lockRepo.Close(); err != nil {
				log.Errorf("LockRepo close: %s", err.Error())
			}

			stop(ctx) //nolint:errcheck

			quitWg.Wait()

			log.Warn("Graceful shutdown successful")

			return nil

		case <-restartChan:
			log.Warn("Restarting ...")
			cancel()

			if err := lockRepo.Close(); err != nil {
				log.Errorf("LockRepo close: %s", err.Error())
			}

			stop(ctx)
			quitWg.Wait()

			if err := udpPacketConn.Close(); err != nil {
				log.Errorf("Close udpPacketConn: %s", err.Error())
			}

			restartDoneChan <- struct{}{} // node/edge/impl.go
			return daemonStart(context.Background(), daemonSwitch, repoPath, locatorURL)
		}
	}
}

func buildSrvHandler(httpServer *httpserver.HttpServer, edgeApi api.Edge, cfg *config.EdgeCfg, schedulerApi api.Scheduler, privateKey *rsa.PrivateKey) (http.Handler, *http.Server) {
	handler := EdgeHandler(edgeApi.AuthVerify, edgeApi, true)
	handler = httpServer.NewHandler(handler)
	handler = validation.AppendHandler(handler, schedulerApi, privateKey, time.Duration(cfg.ValidateDuration)*time.Second)
	handler = tunnel.NewTunserver(handler)

	httpSrv := &http.Server{
		ReadHeaderTimeout: 30 * time.Second,
		Handler:           handler,
		BaseContext: func(listener net.Listener) context.Context {
			ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "titan-edge"))
			return ctx
		},
	}

	return handler, httpSrv
}

func startHTTP3Server(ctx context.Context, transport *quic.Transport, handler http.Handler, config *config.EdgeCfg) error {
	var tlsConfig *tls.Config
	if len(config.CertificatePath) == 0 && len(config.PrivateKeyPath) == 0 {
		config, err := defaultTLSConfig()
		if err != nil {
			log.Errorf("startUDPServer, defaultTLSConfig error:%s", err.Error())
			return err
		}
		tlsConfig = config
	} else {
		cert, err := tls.LoadX509KeyPair(config.CertificatePath, config.PrivateKeyPath)
		if err != nil {
			log.Errorf("startUDPServer, LoadX509KeyPair error:%s", err.Error())
			return err
		}

		tlsConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			Certificates:       []tls.Certificate{cert},
			NextProtos:         []string{"h2", "h3"},
			InsecureSkipVerify: false,
		}
	}

	ln, err := transport.ListenEarly(tlsConfig, nil)
	if err != nil {
		return err
	}

	srv := http3.Server{
		TLSConfig: tlsConfig,
		Handler:   handler,
	}

	go func() {
		<-ctx.Done()
		log.Warn("http3 server graceful shutting down...")
		if err := srv.Close(); err != nil {
			log.Warn("graceful shutting down http3 server with error: ", err.Error())
		}
	}()

	if err = srv.ServeListener(ln); err != nil {
		quitWg.Done()
		log.Warn("http3 server start with error: ", err.Error())
	}
	return err
}

func startHTTPServer(ctx context.Context, srv *http.Server, cfg config.Network) error {
	nl, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return err
	}

	log.Infof("Edge listen on tcp/udp %s", cfg.ListenAddress)

	go func() {
		<-ctx.Done()
		log.Warn("http server graceful shutting down...")
		if err := srv.Close(); err != nil {
			log.Warn("graceful shutting down http server with error: ", err.Error())
		}
	}()

	if err = srv.Serve(nl); err != nil {
		quitWg.Done()
		log.Warn("http server start with error: ", err.Error())
	}

	return err
}

func registShutdownSignal(shutdown chan struct{}) {
	sigChan := make(chan os.Signal, 2)
	go func() {
		<-sigChan
		shutdown <- struct{}{}
	}()
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
}
