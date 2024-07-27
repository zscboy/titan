package l5

import (
	"net/http"
	"strings"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/node/l5/tun"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("tunserver")

const (
	// tunnel path /tunnel/{tunnel-id}, tunnel-id is same as node-id
	tunPathPrefix = "/tun"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type HttpServer struct {
	handler    http.Handler
	scheduler  api.Scheduler
	nodeID     string
	tunManager *tun.TunManger
}

// ServeHTTP checks if the request path starts with the IPFS path prefix and delegates to the appropriate handler
func (s *HttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, tunPathPrefix):
		s.tunManager.AcceptWebsocket(w, r)
	default:
		s.handler.ServeHTTP(w, r)
	}
}

// NewTunserver creates a tunserver with the given HTTP handler
func NewHandler(handler http.Handler, scheduler api.Scheduler, nodeID string, l5API api.L5) http.Handler {
	return &HttpServer{
		handler:    handler,
		scheduler:  scheduler,
		nodeID:     nodeID,
		tunManager: tun.NewTunManager(l5API),
	}
}
