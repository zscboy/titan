package main

import (
	"crypto/tls"
	"net"
)

type dualListener struct {
	net.Listener
	tlsConfig *tls.Config
}

func (dl *dualListener) Accept() (c net.Conn, err error) {
	conn, err := dl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err != nil {
		return nil, err
	}

	if buf[0] == 0x16 {
		return tls.Server(conn, dl.tlsConfig), nil
	}

	// 否则返回原始连接
	return &peekedConn{conn, buf[0]}, nil
}

type peekedConn struct {
	net.Conn
	firstByte byte
}

func (pc *peekedConn) Read(b []byte) (n int, err error) {
	if pc.firstByte != 0 {
		b[0] = pc.firstByte
		pc.firstByte = 0
		return 1, nil
	}
	return pc.Conn.Read(b)
}
