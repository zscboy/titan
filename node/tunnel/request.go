package tunnel

import (
	"fmt"
	"io"
	"net"
)

type Request struct {
	idx    uint16
	tag    uint16
	inused bool
	conn   net.Conn
}

func newRequest(idx uint16) *Request {
	return &Request{idx: idx}
}

func (r *Request) write(data []byte) error {
	if r.conn == nil {
		return fmt.Errorf("request idx %d, writer is nil", r.idx)
	}
	return r.writeAll(data)
}

func (r *Request) writeAll(buf []byte) error {
	wrote := 0
	l := len(buf)
	for {
		n, err := r.conn.Write(buf[wrote:])
		if err != nil {
			return err
		}

		wrote = wrote + n
		if wrote == l {
			break
		}
	}
	return nil
}

func (r *Request) dofree() {
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

func (r *Request) proxy(tunclient *Tunclient) {
	if !r.inused {
		log.Errorf("request is unused")
		return
	}

	if r.conn == nil {
		log.Errorf("conn == nil")
		return
	}

	conn := r.conn
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if !r.inused {
			// request is free!
			log.Errorf("request is free, discard data:", n)
			break
		}

		if err != nil {
			log.Infof("read server message failed: %s", err.Error())
			if err == io.EOF || isNetErrCloseByRemoteHost(err) {
				tunclient.onRequestTerminate(r.idx, r.tag)
			}
			break
		}
		tunclient.onRequestData(r, buf[:n])
	}
}
