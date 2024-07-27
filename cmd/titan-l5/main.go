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
	"time"

	"github.com/Filecoin-Titan/titan/node"
	"github.com/Filecoin-Titan/titan/node/l5"
	"github.com/Filecoin-Titan/titan/node/modules/dtypes"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/api/client"
	"github.com/Filecoin-Titan/titan/api/terrors"
	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/build"
	"github.com/Filecoin-Titan/titan/lib/titanlog"
	"github.com/Filecoin-Titan/titan/metrics"
	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/repo"
	titanrsa "github.com/Filecoin-Titan/titan/node/rsa"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"

	lcli "github.com/Filecoin-Titan/titan/cli"
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
	FlagL5Repo = "l5-repo"

	FlagL5RepoDeprecation = "l5repo"
	HeartbeatInterval     = 10 * time.Second
)

func main() {
	types.RunningNodeType = types.NodeL5
	titanlog.SetupLogLevels()

	local := []*cli.Command{
		runCmd,
		authCmd,
	}

	app := &cli.App{
		Name:                 "titan-l5",
		Usage:                "Titan l5 node",
		Version:              build.UserVersion(),
		EnableBashCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagL5Repo,
				Aliases: []string{FlagL5RepoDeprecation},
				EnvVars: []string{"TITAN_l5_PATH", "l5_PATH"},
				Value:   "~/.titanl5", // TODO: Consider XDG_DATA_HOME
				Usage:   fmt.Sprintf("Specify candidate repo path. flag %s and env TITAN_l5_PATH are DEPRECATION, will REMOVE SOON", FlagL5RepoDeprecation),
			},
			&cli.StringFlag{
				Name:    "panic-reports",
				EnvVars: []string{"TITAN_PANIC_REPORT_PATH"},
				Hidden:  true,
				Value:   "~/.titanl5", // should follow --repo default
			},
		},

		After: func(c *cli.Context) error {
			if r := recover(); r != nil {
				// Generate report in TITAN_PATH and re-raise panic
				build.GeneratePanicReport(c.String("panic-reports"), c.String(FlagL5Repo), c.App.Name)
				panic(r)
			}
			return nil
		},
		Commands: local,
	}
	app.Setup()
	app.Metadata["repoType"] = repo.L5

	if err := app.Run(os.Args); err != nil {
		log.Errorf("%+v", err)
		return
	}
}

var authCmd = &cli.Command{
	Name:  "auth",
	Usage: "generate auth key",
	Action: func(cctx *cli.Context) error {
		addr, headers, err := lcli.GetRawAPI(cctx, repo.L5, "v0")
		if err != nil {
			return err
		}

		l5API, close, err := client.NewL5(cctx.Context, addr, headers)
		if err != nil {
			return err
		}
		defer close()

		key, err := l5API.AuthNew(cctx.Context, &types.JWTPayload{Allow: []auth.Permission{api.RoleL5}})
		if err != nil {
			return err
		}

		fmt.Println("Key: ", key)
		return nil
	},
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "run l5 node",
	Flags: []cli.Flag{
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
		log.Info("Starting titan l5 node")

		// Register all metric views
		if err := view.Register(
			metrics.DefaultViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}

		repoPath := cctx.String(FlagL5Repo)
		r, err := repo.NewFS(repoPath)
		if err != nil {
			return err
		}

		ok, err := r.Exists()
		if err != nil {
			return err
		}
		if !ok {
			if err := r.Init(repo.L5); err != nil {
				return err
			}
		}

		lr, err := r.Lock(repo.L5)
		if err != nil {
			return err
		}

		_, err = r.NodeID()
		if err != nil {
			if err == repo.ErrNodeIDNotExist {
				url := cctx.String("url")
				if len(url) == 0 {
					return fmt.Errorf(`please initialize l5, example: titan-l5 run --url https://titan-network-url/rpc/v0`)
				}
				err = registerL5Node(lr, url)
			}

			if err != nil {
				return err
			}
		}

		cfg, err := lr.Config()
		if err != nil {
			return err
		}

		l5Cfg := cfg.(*config.L5Cfg)

		err = lr.Close()
		if err != nil {
			return err
		}

		privateKey, err := loadPrivateKey(r)
		if err != nil {
			return fmt.Errorf(`please initialize l5, example: titan-l5 run --url https://titan-network-url/rpc/v0`)
		}

		nodeIDBuf, err := r.NodeID()
		if err != nil {
			return err
		}
		nodeID := string(nodeIDBuf)

		connectTimeout, err := time.ParseDuration(l5Cfg.Timeout)
		if err != nil {
			return err
		}

		packetConn, err := net.ListenPacket("udp", l5Cfg.ListenAddress)
		if err != nil {
			return err
		}
		defer packetConn.Close() //nolint:errcheck // ignore error

		transport := &quic.Transport{Conn: packetConn}

		// Connect to scheduler
		schedulerAPI, closer, err := newSchedulerAPI(cctx, transport, l5Cfg.SchedulerURL, nodeID, privateKey)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)
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

		shutdownChan := make(chan struct{})
		// var httpServer *httpserver.HttpServer
		var l5API api.L5
		stop, err := node.New(cctx.Context,
			node.L5(&l5API),
			node.Base(),
			node.Repo(r),
			node.Override(new(dtypes.ShutdownChan), shutdownChan),
			node.Override(new(*quic.Transport), transport),
			node.Override(node.SetApiEndpointKey, func(lr repo.LockedRepo) error {
				return setEndpointAPI(lr, l5Cfg.ListenAddress)
			}),
		)
		if err != nil {
			return xerrors.Errorf("creating node: %w", err)
		}

		tlsConfig, err := getTLSConfig(l5Cfg)
		if err != nil {
			return xerrors.Errorf("get tls config: %w", err)
		}

		handler := l5Handler(l5API.AuthVerify, l5API, true)
		handler = l5.NewHandler(handler, schedulerAPI, nodeID, l5API)

		httpSrv := &http.Server{
			ReadHeaderTimeout: 30 * time.Second,
			Handler:           handler,
			BaseContext: func(listener net.Listener) context.Context {
				ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "titan-l5"))
				return ctx
			},
			TLSConfig: tlsConfig,
		}

		go startHTTP3Server(transport, handler, tlsConfig)

		go func() {
			<-ctx.Done()
			log.Warn("Shutting down...")

			if err := transport.Close(); err != nil {
				log.Errorf("shutting down http3Srv failed: %s", err)
			}

			if err := httpSrv.Shutdown(context.TODO()); err != nil {
				log.Errorf("shutting down httpSrv failed: %s", err)
			}

			stop(ctx) //nolint:errcheck
			log.Warn("Graceful shutdown successful")
		}()

		nl, err := net.Listen("tcp", l5Cfg.ListenAddress)
		if err != nil {
			return err
		}

		log.Infof("L5 listen on %s", l5Cfg.ListenAddress)

		schedulerSession, err := schedulerAPI.Session(ctx)
		if err != nil {
			return xerrors.Errorf("getting scheduler session: %w", err)
		}

		token, err := l5API.AuthNew(cctx.Context, &types.JWTPayload{Allow: []auth.Permission{api.RoleAdmin}, ID: nodeID})
		if err != nil {
			return xerrors.Errorf("generate token for scheduler error: %w", err)
		}

		waitQuietCh := func() chan struct{} {
			out := make(chan struct{})
			go func() {
				ctx2 := context.Background()
				err = l5API.WaitQuiet(ctx2)
				if err != nil {
					log.Errorf("wait quiet error: %s", err.Error())
				}
				close(out)
			}()
			return out
		}

		go func() {
			heartbeats := time.NewTicker(HeartbeatInterval)
			defer heartbeats.Stop()

			var readyCh chan struct{}
			for {
				// TODO: we could get rid of this, but that requires tracking resources for restarted tasks correctly
				if readyCh == nil {
					log.Info("Making sure no local tasks are running")
					readyCh = waitQuietCh()
				}

				for {
					select {
					case <-readyCh:
						opts := &types.ConnectOptions{Token: token}
						err := schedulerAPI.L5Connect(ctx, opts)
						if err != nil {
							log.Errorf("L5 connect failed: %s", err.Error())
							cancel()
							return
						}

						log.Info("L5 registered successfully, waiting for tasks")
						readyCh = nil
					case <-heartbeats.C:
					case <-ctx.Done():
						return
					case <-shutdownChan:
						cancel()
						return
					}

					curSession, err := keepalive(schedulerAPI, connectTimeout)
					if err != nil {
						log.Errorf("heartbeat: keepalive failed: %+v", err)
						errNode, ok := err.(*api.ErrNode)
						if ok {
							if errNode.Code == int(terrors.NodeDeactivate) {
								cancel()
								return
							} else if errNode.Code == int(terrors.NodeIPInconsistent) {
								break
							} else if errNode.Code == int(terrors.NodeOffline) && readyCh == nil {
								break
							}
						}
					} else if curSession != schedulerSession {
						log.Warn("change session id")
						schedulerSession = curSession
						break
					}
				}

				log.Errorf("TITAN-EDGE CONNECTION LOST")
			}
		}()

		return httpSrv.Serve(nl)
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

func newSchedulerAPI(cctx *cli.Context, tansport *quic.Transport, schedulerURL, nodeID string, privateKey *rsa.PrivateKey) (api.Scheduler, jsonrpc.ClientCloser, error) {
	token, err := newAuthTokenFromScheduler(schedulerURL, nodeID, privateKey)
	if err != nil {
		return nil, nil, err
	}

	httpClient, err := client.NewHTTP3ClientWithPacketConn(tansport)
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

	if err := os.Setenv("SCHEDULER_API_INFO", token+":"+schedulerURL); err != nil {
		log.Errorf("set env error:%s", err.Error())
	}

	return schedulerAPI, closer, nil
}

func isEnableTLS(l5Cfg *config.L5Cfg) bool {
	if len(l5Cfg.CertificatePath) > 0 && len(l5Cfg.PrivateKeyPath) > 0 {
		return true
	}
	return false
}

func getTLSConfig(l5Cfg *config.L5Cfg) (*tls.Config, error) {
	if isEnableTLS(l5Cfg) {
		cert, err := tls.LoadX509KeyPair(l5Cfg.CertificatePath, l5Cfg.PrivateKeyPath)
		if err != nil {
			log.Errorf("LoadX509KeyPair error:%s", err.Error())
			return nil, err
		}

		return &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}, nil
	}

	return defaultTLSConfig()
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

func loadPrivateKey(r *repo.FsRepo) (*rsa.PrivateKey, error) {
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

func startHTTP3Server(transport *quic.Transport, handler http.Handler, config *tls.Config) error {
	var err error
	if config == nil {
		config, err = defaultTLSConfig()
		if err != nil {
			log.Errorf("startUDPServer, defaultTLSConfig error:%s", err.Error())
			return err
		}
	}

	ln, err := transport.ListenEarly(config, nil)
	if err != nil {
		log.Errorf("startUDPServer, ListenEarly error:%s", err.Error())
		return err
	}

	srv := http3.Server{
		TLSConfig: config,
		Handler:   handler,
	}
	return srv.ServeListener(ln)
}

func registerL5Node(lr repo.LockedRepo, schedulerURL string) error {
	schedulerAPI, closer, err := client.NewScheduler(context.Background(), schedulerURL, nil, jsonrpc.WithHTTPClient(client.NewHTTP3Client()))
	if err != nil {
		return err
	}
	defer closer()

	bits := 1024

	privateKey, err := titanrsa.GeneratePrivateKey(bits)
	if err != nil {
		return err
	}

	pem := titanrsa.PublicKey2Pem(&privateKey.PublicKey)
	nodeID := fmt.Sprintf("l5_%s", uuid.NewString())

	_, err = schedulerAPI.RegisterNode(context.Background(), nodeID, string(pem), types.NodeL5)
	if err != nil {
		return xerrors.Errorf("RegisterNode %w", err)
	}

	privatePem := titanrsa.PrivateKey2Pem(privateKey)
	if err := lr.SetPrivateKey(privatePem); err != nil {
		return err
	}

	if err := lr.SetNodeID([]byte(nodeID)); err != nil {
		return err
	}

	return lr.SetConfig(func(raw interface{}) {
		cfg, ok := raw.(*config.L5Cfg)
		if !ok {
			return
		}
		cfg.SchedulerURL = schedulerURL
	})
}
