package tun

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Filecoin-Titan/titan/api"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{} // use default options

type TunManger struct {
	l5API   api.L5
	tunnels map[int]*Tunnel
	tidx    int
}

func NewTunManager(l5API api.L5) *TunManger {
	tm := &TunManger{l5API: l5API, tunnels: make(map[int]*Tunnel), tidx: 0}
	go tm.keepalive()
	return tm
}

func (tm *TunManger) AcceptWebsocket(w http.ResponseWriter, r *http.Request) {
	if err := tm.authVerify(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Auth verify failed: %s", err.Error())))
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

	tun := newTunnel(idx, conn, 100)
	tm.tunnels[idx] = tun
	defer delete(tm.tunnels, idx)

	tun.serve()
}

func (tm *TunManger) keepalive() {
	for {
		time.Sleep(time.Second * 30)
		for _, t := range tm.tunnels {
			t.keepalive()
		}
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