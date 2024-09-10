package server

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	cMDNone              = 0
	cMDReqData           = 1
	cMDReqCreated        = 2
	cMDReqClientClosed   = 3
	cMDReqClientFinished = 4
	cMDReqServerFinished = 5
	cMDReqServerClosed   = 6
	cMDDNSReq            = 7
	cMDDNSRsp            = 8
	cMDUDPReq            = 9
)

// Tunnel tunnel
type Tunnel struct {
	id   int
	conn *websocket.Conn
	reqq *Reqq

	writeLock sync.Mutex
	waitping  int

	rateLimit   uint
	rateQuota   uint
	rateChan    chan []byte
	rateWg      *sync.WaitGroup
	cache       *Cache
	endpoint    string
	reverseServ *ReverseServ
	// reverseServ *UDPReverseServers
}

func newTunnel(id int, conn *websocket.Conn, cap int, rateLimit uint, endpiont string, reverseServ *ReverseServ) *Tunnel {

	t := &Tunnel{
		id:          id,
		conn:        conn,
		rateLimit:   rateLimit,
		rateQuota:   rateLimit,
		cache:       newCache(),
		endpoint:    endpiont,
		reverseServ: reverseServ,
	}

	reqq := newReqq(cap, t)
	t.reqq = reqq

	conn.SetPingHandler(func(data string) error {
		t.writePong([]byte(data))
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		t.onPong([]byte(data))
		return nil
	})

	if rateLimit > 0 {
		// 100 item
		t.rateChan = make(chan []byte, 100)
	}

	reverseServ.onTunnelConnect(t)
	return t
}

func (t *Tunnel) rateLimitReset(quota uint) {
	t.rateQuota = quota

	if t.rateWg != nil {
		t.rateWg.Done()
		t.rateWg = nil
	}
}

func (t *Tunnel) waitQuota(q uint) {
	if t.rateQuota < q {
		t.rateQuota = 0

		// wait
		wg := sync.WaitGroup{}
		wg.Add(1)
		t.rateWg = &wg
		wg.Wait()
	} else {
		t.rateQuota = t.rateQuota - q
	}
}

func (t *Tunnel) drainRateChan() {
	for {
		buf := <-t.rateChan
		if buf != nil {
			leN := len(buf)
			t.write(buf)

			t.waitQuota(uint(leN))
		} else {
			break
		}
	}
}

func (t *Tunnel) serve() {
	// loop read websocket message
	if t.rateLimit > 0 {
		go t.drainRateChan()
	}

	c := t.conn
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("Tunnel read failed:", err)
			break
		}

		// log.Println("Tunnel recv message, len:", len(message))
		err = t.onTunnelMessage(message)
		if err != nil {
			log.Println("Tunnel onTunnelMessage failed:", err)
			break
		}
	}

	t.onClose()
}

func (t *Tunnel) keepalive() {
	if t.waitping > 3 {
		t.conn.Close()
		return
	}

	t.writeLock.Lock()
	now := time.Now().Unix()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(now))
	t.conn.WriteMessage(websocket.PingMessage, b)
	t.writeLock.Unlock()

	t.waitping++
}

func (t *Tunnel) writePong(msg []byte) {
	t.writeLock.Lock()
	t.conn.WriteMessage(websocket.PongMessage, msg)
	t.writeLock.Unlock()
}

func (t *Tunnel) write(msg []byte) {
	t.writeLock.Lock()
	t.conn.WriteMessage(websocket.BinaryMessage, msg)
	t.writeLock.Unlock()
}

func (t *Tunnel) onPong(msg []byte) {
	t.waitping = 0
}

func (t *Tunnel) onClose() {
	if t.rateLimit > 0 {
		t.rateLimit = 0
		close(t.rateChan)
	}

	t.reqq.cleanup()
	t.reverseServ.onTunnelClose(t)

}

func (t *Tunnel) onTunnelMessage(message []byte) error {
	if len(message) < 5 {
		return fmt.Errorf("invalid tunnel message")
	}

	cmd := message[0]
	if cmd == cMDDNSReq {
		// TODO: too many goroutines
		// go doDNSQuery(t, message)
		return nil
	}

	if cmd == cMDUDPReq {
		t.handleUDPReq(message)
		return nil
	}

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
		log.Printf("onTunnelMessage, unsupport tunnel cmd:%d", cmd)
	}

	return nil
}

func (t *Tunnel) handleRequestCreate(idx uint16, tag uint16, message []byte) {
	addressType := message[0]
	var port uint16
	var domain string
	switch addressType {
	case 0: // ipv4
		domain = fmt.Sprintf("%d.%d.%d.%d", message[1], message[2], message[3], message[4])
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
		log.Println("handleRequestCreate, not support addressType:", addressType)
		return
	}

	req, err := t.reqq.alloc(idx, tag)
	if err != nil {
		log.Println("handleRequestCreate, alloc req failed:", err)
		return
	}

	addr := fmt.Sprintf("%s:%d", domain, port)
	log.Println("proxy to:", addr)

	ts := time.Second * 2
	c, err := net.DialTimeout("tcp", addr, ts)
	if err != nil {
		log.Println("proxy DialTCP failed: ", err)
		t.onRequestTerminate(req)
		return
	}

	req.conn = c.(*net.TCPConn)

	go req.proxy()
}

func (t *Tunnel) handleRequestData(idx uint16, tag uint16, message []byte) {
	req, err := t.reqq.get(idx, tag)
	if err != nil {
		log.Println("handleRequestData, get req failed:", err)
		return
	}

	req.onClientData(message)
}

func (t *Tunnel) handleRequestFinished(idx uint16, tag uint16) {
	req, err := t.reqq.get(idx, tag)
	if err != nil {
		//log.Println("handleRequestData, get req failed:", err)
		return
	}

	req.onClientFinished()
}

func (t *Tunnel) handleRequestClosed(idx uint16, tag uint16) {
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

	if t.rateLimit > 0 {
		t.rateChan <- buf
	} else {
		t.write(buf)
	}
}

func (t *Tunnel) handleUDPReq(data []byte) {
	// skip cmd
	src := parseAddress(data[1:])

	srcIPLen := net.IPv6len
	if src.IP.To4() != nil {
		srcIPLen = net.IPv4len
	}

	// 4 = cmd + iptype + port
	dest := parseAddress(data[4+srcIPLen:])

	destIPLen := net.IPv6len
	if dest.IP.To4() != nil {
		destIPLen = net.IPv4len
	}

	log.Debugf("handleUDPReq src %s dest %s", src.String(), dest.String())

	// 7 = cmd + iptype1 + iptype2 + port1 + port2
	skip := 7 + srcIPLen + destIPLen

	// write to reverse proxy of udp conn
	conn := t.reverseServ.getUDPConn(t.endpoint, src)
	if conn != nil {
		conn.WriteTo(data[skip:], dest)
		return
	}

	ustub := t.cache.get(src)
	if ustub == nil {
		udpConn, err := newUDPConn("0.0.0.0:0")
		if err != nil {
			log.Errorf("New UDPConn failed:%s", err.Error())
			return
		}
		ustub = newUstub(t, udpConn, src)
		t.cache.add(ustub)
		log.Infof("new ustub src %s", src.String())
	}

	ustub.writeTo(dest, data[skip:])
}

func (t *Tunnel) onServerUDPData(data []byte, src *net.UDPAddr, dest *net.UDPAddr) {
	log.Debugf("onServerData src %s dest %s", src, dest)

	srcAddrBuf := writeUDPAddress(src)
	destAddrBuf := writeUDPAddress(dest)

	buf := make([]byte, 1+len(srcAddrBuf)+len(destAddrBuf)+len(data))

	buf[0] = byte(cMDUDPReq)
	copy(buf[1:], srcAddrBuf)
	copy(buf[1+len(srcAddrBuf):], destAddrBuf)
	copy(buf[1+len(srcAddrBuf)+len(destAddrBuf):], data)

	t.write(buf)
}

func parseAddress(msg []byte) *net.UDPAddr {
	// skip cmd
	var ip []byte = nil

	offset := 0
	port := binary.LittleEndian.Uint16(msg[offset:])
	offset += 2

	ipType := msg[offset]
	offset += 1

	switch ipType {
	case 0:
		// ipv4
		ip = make([]byte, net.IPv4len)
		copy(ip[0:], msg[offset:offset+4])
	case 2:
		// ipv6
		ip = make([]byte, net.IPv6len)
		copy(ip[0:], msg[offset:offset+16])
	}

	return &net.UDPAddr{IP: ip, Port: int(port)}
}

func writeUDPAddress(address *net.UDPAddr) []byte {
	iplen := net.IPv6len
	if address.IP.To4() != nil {
		iplen = net.IPv4len
	}
	// 3 = iptype(1) + port(2)
	buf := make([]byte, 3+iplen)
	// add port
	binary.LittleEndian.PutUint16(buf[0:], uint16(address.Port))
	// set ip type
	if iplen > net.IPv4len {
		// ipv6
		buf[2] = 2
		copy(buf[3:], address.IP.To16())
	} else {
		// ipv4
		buf[2] = 0
		copy(buf[3:], address.IP.To4())
	}

	return buf
}

func writeTCPAddress(address *net.TCPAddr) []byte {
	iplen := net.IPv6len
	if address.IP.To4() != nil {
		iplen = net.IPv4len
	}
	// 3 = iptype(1) + port(2)
	buf := make([]byte, 3+iplen)
	// add port
	binary.LittleEndian.PutUint16(buf[0:], uint16(address.Port))
	// set ip type
	if iplen > net.IPv4len {
		// ipv6
		buf[2] = 2
		copy(buf[3:], address.IP.To16())
	} else {
		// ipv4
		buf[2] = 0
		copy(buf[3:], address.IP.To4())
	}

	return buf
}

func (t *Tunnel) acceptTCPConn(conn net.Conn, src *net.TCPAddr) error {
	tcpConn := conn.(*net.TCPConn)
	req, err := t.reqq.allocForConn(tcpConn)
	if err != nil {
		return err
	}

	addr := conn.RemoteAddr()

	dest, ok := addr.(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("can not convert net.Addr to net.TCPAddr")
	}

	t.onClientCreate(src, dest, req.idx, req.tag)

	// start a new goroutine to read data from 'conn'
	go req.proxy()

	return nil
}

func (t *Tunnel) onClientCreate(src, dest *net.TCPAddr, idx, tag uint16) {
	// log.Infof("Tunnel.onClientCreate src", src.String())

	srcAddrBuf := writeTCPAddress(src)
	destAddrBuf := writeTCPAddress(dest)

	buf := make([]byte, 5+len(srcAddrBuf)+len(destAddrBuf))

	buf[0] = byte(cMDReqCreated)
	binary.LittleEndian.PutUint16(buf[1:], idx)
	binary.LittleEndian.PutUint16(buf[3:], tag)

	copy(buf[5:], srcAddrBuf)
	copy(buf[5+len(srcAddrBuf):], destAddrBuf)

	t.write(buf)
}
