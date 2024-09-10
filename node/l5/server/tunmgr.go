package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

var upgrader = websocket.Upgrader{} // use default options

type TunManger struct {
	l5API       api.L5
	tunnels     *sync.Map
	tidx        int
	rateLimit   uint // 0: no limit
	reverseServ *ReverseServ
}

func NewTunManager(l5API api.L5, reverseServ *ReverseServ) *TunManger {
	tm := &TunManger{l5API: l5API, tunnels: &sync.Map{}, tidx: 0, reverseServ: reverseServ}
	go tm.keepalive()
	return tm
}

func (tm *TunManger) AcceptWebsocket(w http.ResponseWriter, r *http.Request) {
	if err := tm.authVerify(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Auth verify failed: %s", err.Error())))
		return
	}

	var endpoint = r.URL.Query().Get("endpoint")
	if endpoint == "" {
		log.Println("need endpoint!")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Upgrade failed %s", err.Error())))
		return
	}
	defer conn.Close()

	log.Debugf("AcceptWebsocket %s", r.URL.Path)

	idx := tm.tidx
	tm.tidx++

	tun := newTunnel(idx, conn, 100, tm.rateLimit, endpoint, tm.reverseServ)
	tm.tunnels.Store(idx, tun)
	defer tm.tunnels.Delete(idx)

	tun.serve()
}

func (tm *TunManger) keepalive() {
	for {
		time.Sleep(time.Second * 30)
		tm.tunnels.Range(func(key, value any) bool {
			tun := value.(*Tunnel)
			tun.keepalive()
			return true
		})
	}
}

func (tm *TunManger) authVerify(r *http.Request) error {
	token := r.Header.Get("Authorization")
	if len(token) == 0 {
		return fmt.Errorf("No header Authorization exist")
	}

	token = strings.TrimPrefix(token, "Bearer ")

	_, err := tm.l5API.AuthVerify(context.TODO(), token)
	if err != nil {
		return err
	}

	return nil
}
