package l5

import (
	"net/http"
	"strings"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/Filecoin-Titan/titan/node/config"
	"github.com/Filecoin-Titan/titan/node/l5/server"
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
	tunManager *server.TunManger
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
func NewHandler(handler http.Handler, scheduler api.Scheduler, nodeID string, l5API api.L5, l5Config *config.L5Cfg) http.Handler {
	tcpAddrssMaps := make(map[string]server.AddressMap)
	for _, endpoint := range l5Config.TCPAddressMaps {
		addressMap := tcpAddrssMaps[endpoint.Endpoint]
		if addressMap == nil {
			addressMap = make(server.AddressMap)
		}

		for k, v := range endpoint.Addressmap {
			addressMap[k] = v
		}

		tcpAddrssMaps[endpoint.Endpoint] = addressMap
	}

	udpAddrssMaps := make(map[string]server.AddressMap)
	for _, endpoint := range l5Config.UDPAddressMaps {
		addressMap := udpAddrssMaps[endpoint.Endpoint]
		if addressMap == nil {
			addressMap = make(server.AddressMap)
		}

		for k, v := range endpoint.Addressmap {
			addressMap[k] = v
		}

		udpAddrssMaps[endpoint.Endpoint] = addressMap

	}

	reverseServ, err := server.NewReverseServ(tcpAddrssMaps, udpAddrssMaps)
	if err != nil {
		log.Fatal(err)
	}

	return &HttpServer{
		handler:    handler,
		scheduler:  scheduler,
		nodeID:     nodeID,
		tunManager: server.NewTunManager(l5API, reverseServ),
	}
}
