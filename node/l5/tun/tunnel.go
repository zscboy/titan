package tun

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("tunnel")

const (
	// cMDNone              = 0
	// cMDReqData           = 1
	// cMDReqCreated        = 2
	// cMDReqClientClosed   = 3
	// cMDReqClientFinished = 4
	// cMDReqServerFinished = 5
	// cMDReqServerClosed   = 6
	// cMDDNSReq            = 7
	// cMDDNSRsp            = 8
	cMDNone              = 0
	cMDPing              = 1
	cMDPong              = 2
	cMReqBegin           = 3
	cMDReqData           = 3
	cMDReqCreated        = 4
	cMDReqClientClosed   = 5
	cMDReqClientFinished = 6
	cMDReqServerFinished = 7
	cMDReqServerClosed   = 8
	cMDReqRefreshQuota   = 9
	cMDReqEnd            = 10
)

// Tunnel tunnel
type Tunnel struct {
	id   int
	conn *websocket.Conn
	reqq *Reqq

	writeLock sync.Mutex
	waitping  int
}

func newTunnel(id int, conn *websocket.Conn, cap int) *Tunnel {

	t := &Tunnel{
		id:   id,
		conn: conn,
	}

	reqq := newReqq(cap, t)
	t.reqq = reqq

	// conn.SetPingHandler(func(data string) error {
	// 	t.writePong([]byte(data))
	// 	return nil
	// })

	// conn.SetPongHandler(func(data string) error {
	// 	t.onPong([]byte(data))
	// 	return nil
	// })

	return t
}

func (t *Tunnel) serve() {
	// loop read websocket message
	defer t.onClose()
	c := t.conn
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Infof("Tunnel read failed: %s", err.Error())
			break
		}

		// log.Println("Tunnel recv message, len:", len(message))
		err = t.onTunnelMessage(message)
		if err != nil {
			log.Infof("Tunnel onTunnelMessage failed:", err)
			break
		}
	}
}

func (t *Tunnel) keepalive() {
	if t.waitping > 3 {
		t.conn.Close()
		return
	}

	if err := t.sendPing(); err != nil {
		log.Errorf("keepalive send ping %s", err.Error())
	}
	t.waitping++
}

func (t *Tunnel) write(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()
	return t.conn.WriteMessage(websocket.BinaryMessage, msg)
}

func (t *Tunnel) sendPong(data []byte) error {
	buf := make([]byte, len(data))
	buf[0] = byte(cMDPong)
	copy(buf[1:], data[1:])
	return t.write(buf)
}

func (t *Tunnel) sendPing() error {
	now := time.Now().Unix()
	b := make([]byte, 9)
	b[0] = byte(cMDPing)
	binary.LittleEndian.PutUint64(b[1:], uint64(now))

	return t.write(b)
}

func (t *Tunnel) onPong(msg []byte) error {
	t.waitping = 0
	return nil
}

func (t *Tunnel) onClose() {
	t.reqq.cleanup()
}

func (t *Tunnel) onTunnelMessage(message []byte) error {
	if len(message) < 5 {
		return fmt.Errorf("invalid tunnel message")
	}

	cmd := uint8(message[0])
	// log.Debugf("onTunnelMsg messag len %d, cmd %d", len(message), cmd)
	if t.isRequestCmd(cmd) {
		t.onTunnelCmd(cmd, message)
		return nil
	}

	switch cmd {
	case cMDPing:
		return t.sendPong(message)
	case cMDPong:
		return t.onPong(message)

	default:
		log.Errorf("[Tunnel]unknown cmd:", cmd)
	}
	return nil
}

func (t *Tunnel) onTunnelCmd(cmd uint8, message []byte) {

	idx := binary.LittleEndian.Uint16(message[1:])
	tag := binary.LittleEndian.Uint16(message[3:])

	switch cmd {
	case cMDReqCreated:
		t.handleRequestCreate(idx, tag, message[5:])
	case cMDReqData:
		t.handleRequestData(idx, tag, message[5:])
	case cMDReqClientFinished:
		t.handleRequestFinished(idx, tag)
	case cMDReqClientClosed:
		t.handleRequestClosed(idx, tag)
	default:
		log.Errorf("[Tunnel]unknown cmd:", cmd)
	}

}

func (t *Tunnel) isRequestCmd(cmd uint8) bool {
	if cmd >= cMReqBegin && cmd < cMDReqEnd {
		return true
	}

	return false
}

func (t *Tunnel) handleRequestCreate(idx uint16, tag uint16, message []byte) {
	addressType := message[0]
	var port uint16
	var domain string
	switch addressType {
	case 0: // ipv4
		domain = fmt.Sprintf("%d.%d.%d.%d", message[4], message[3], message[2], message[1])
		port = binary.LittleEndian.Uint16(message[5:])
	case 1: // domain name
		domainLen := message[1]
		domain = string(message[2 : 2+domainLen])
		port = binary.LittleEndian.Uint16(message[(2 + domainLen):])
	case 2: // ipv6
		p1 := binary.LittleEndian.Uint16(message[1:])
		p2 := binary.LittleEndian.Uint16(message[3:])
		p3 := binary.LittleEndian.Uint16(message[5:])
		p4 := binary.LittleEndian.Uint16(message[7:])
		p5 := binary.LittleEndian.Uint16(message[9:])
		p6 := binary.LittleEndian.Uint16(message[11:])
		p7 := binary.LittleEndian.Uint16(message[13:])
		p8 := binary.LittleEndian.Uint16(message[15:])

		domain = fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", p8, p7, p6, p5, p4, p3, p2, p1)
		port = binary.LittleEndian.Uint16(message[17:])
	default:
		log.Error("handleRequestCreate, not support addressType:", addressType)
		return
	}

	req, err := t.reqq.alloc(idx, tag)
	if err != nil {
		log.Error("handleRequestCreate, alloc req failed:", err)
		return
	}

	addr := fmt.Sprintf("%s:%d", domain, port)
	log.Debug("proxy to:", addr)

	ts := time.Second * 2
	c, err := net.DialTimeout("tcp", addr, ts)
	if err != nil {
		log.Debug("proxy DialTCP failed: ", err)
		t.onRequestTerminate(req)
		return
	}

	req.conn = c.(*net.TCPConn)

	go req.proxy()
}

func (t *Tunnel) handleRequestData(idx uint16, tag uint16, message []byte) {
	log.Debugf("handleRequestData idx %d tag %d", idx, tag)
	req, err := t.reqq.get(idx, tag)
	if err != nil {
		log.Debug("handleRequestData, get req failed:", err)
		return
	}

	req.onClientData(message)
}

func (t *Tunnel) handleRequestFinished(idx uint16, tag uint16) {
	log.Debugf("handleRequestFinished idx %d tag %d", idx, tag)
	req, err := t.reqq.get(idx, tag)
	if err != nil {
		//log.Println("handleRequestData, get req failed:", err)
		return
	}

	req.onClientFinished()
}

func (t *Tunnel) handleRequestClosed(idx uint16, tag uint16) {
	log.Debugf("handleRequestClosed idx %d tag %d", idx, tag)

	err := t.reqq.free(idx, tag)
	if err != nil {
		//log.Println("handleRequestClosed, get req failed:", err)
		return
	}
}

func (t *Tunnel) onRequestTerminate(req *Request) {
	// send close to client
	buf := make([]byte, 5)
	buf[0] = cMDReqServerClosed
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	t.write(buf)

	t.handleRequestClosed(req.idx, req.tag)
}

func (t *Tunnel) onRequestHalfClosed(req *Request) {
	// send half-close to client
	buf := make([]byte, 5)
	buf[0] = cMDReqServerFinished
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	t.write(buf)
}

func (t *Tunnel) onRequestData(req *Request, data []byte) {
	buf := make([]byte, 5+len(data))
	buf[0] = cMDReqData
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)
	copy(buf[5:], data)

	t.write(buf)
}
