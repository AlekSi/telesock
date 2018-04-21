// telesock - Fast and simple SOCKS5 proxy.
// Written in 2018 by Alexey Palazhchenko.
//
// To the extent possible under law, the author(s) have dedicated all copyright and related and neighboring rights
// to this software to the public domain worldwide. This software is distributed without any warranty.
//
// You should have received a copy of the CC0 Public Domain Dedication along with this software.
// If not, see <http://creativecommons.org/publicdomain/zero/1.0/>.

package internal

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"io"
	"net"

	"go.uber.org/zap"
)

// Config represents Telesock configuration.
type Config struct {
	Users []struct {
		Username string
		Password string
	}
}

// TCPConn represents TCP connection between SOCKS5 client and server.
type TCPConn struct {
	l    *zap.SugaredLogger
	conf *Config

	clientR *bufio.Reader
	clientW io.WriteCloser

	server *net.TCPConn
}

// NewTCPConn creates new TCPConn for given network connection.
func NewTCPConn(c *net.TCPConn, l *zap.SugaredLogger, conf *Config) *TCPConn {
	l.Info("Connection established.")

	return &TCPConn{
		l:    l,
		conf: conf,

		clientR: bufio.NewReaderSize(c, 128),
		clientW: c,
	}
}

func (tcp *TCPConn) Close() {
	if tcp.server != nil {
		tcp.server.Close()
	}

	tcp.clientW.Close()
	tcp.l.Info("Connection closed.")
	tcp.l.Sync()
}

func (tcp *TCPConn) Auth(ctx context.Context) bool {
	l := tcp.l.With(zap.String("step", "auth"))

	ver, err := tcp.clientR.ReadByte()
	if err != nil {
		l.Error(err)
		return false
	}
	if ver != 5 {
		l.Errorf("Unsupported SOCKS protocol version %d.", ver)
		return false
	}

	nmethod, err := tcp.clientR.ReadByte()
	if err != nil {
		l.Error(err)
		return false
	}
	methods := make([]byte, nmethod)
	if _, err = io.ReadFull(tcp.clientR, methods); err != nil {
		l.Error(err)
		return false
	}
	method := byte(255)
	for _, m := range methods {
		if m == 2 {
			method = m
			break
		}
	}

	b := []byte{5, method}
	if _, err = tcp.clientW.Write(b); err != nil {
		l.Error(err)
		return false
	}
	if method == 255 {
		l.Errorf("Supported authentication method not found in %#v.", methods)
		return false
	}

	ver, err = tcp.clientR.ReadByte()
	if err != nil {
		l.Error(err)
		return false
	}
	if ver != 1 {
		l.Errorf("Unsupported SOCKS username/password subnegotiation version %d.", ver)
		return false
	}

	len, err := tcp.clientR.ReadByte()
	if err != nil {
		l.Error(err)
		return false
	}
	if len == 0 {
		l.Errorf("Unexpected username length %d.", len)
		return false
	}
	username := make([]byte, len)
	if _, err = io.ReadFull(tcp.clientR, username); err != nil {
		l.Error(err)
		return false
	}

	len, err = tcp.clientR.ReadByte()
	if err != nil {
		l.Error(err)
		return false
	}
	if len == 0 {
		l.Errorf("Unexpected password length %d.", len)
		return false
	}
	password := make([]byte, len)
	if _, err = io.ReadFull(tcp.clientR, password); err != nil {
		l.Error(err)
		return false
	}

	var userFound bool
	for _, user := range tcp.conf.Users {
		usernameOk := subtle.ConstantTimeCompare(username, []byte(user.Username)) == 1
		passwordOk := subtle.ConstantTimeCompare(password, []byte(user.Password)) == 1
		if usernameOk && passwordOk {
			userFound = true
		}
	}

	b = []byte{1, 0}
	if !userFound {
		b[1] = 1
	}
	if _, err = tcp.clientW.Write(b); err != nil {
		l.Error(err)
		return false
	}

	if b[1] == 0 {
		l.Info("Connection authenticated.")
		return true
	}

	l.Errorf("Username or password is invalid (was %q / %q).", string(username), string(password))
	return false
}

type req struct {
	Ver  byte
	Cmd  byte
	Rsv  byte
	Atyp byte
}

type ipv4Addr struct {
	Addr [4]byte
	Port uint16
}

type res struct {
	Ver  byte
	Rep  byte
	Rsv  byte
	Atyp byte
}

func (tcp *TCPConn) Req(ctx context.Context) bool {
	l := tcp.l.With(zap.String("step", "req"))

	var req req
	if err := binary.Read(tcp.clientR, binary.BigEndian, &req); err != nil {
		l.Error(err)
		return false

	}
	if req.Ver != 5 {
		l.Errorf("Unexpected request version %d.", req.Ver)
		return false
	}
	if req.Cmd != 1 {
		l.Errorf("Unexpected command %d.", req.Cmd)
		return false
	}
	if req.Rsv != 0 {
		l.Errorf("Unexpected reserved byte %d.", req.Rsv)
		return false
	}
	if req.Atyp != 1 {
		l.Errorf("Unexpected atyp byte %d.", req.Atyp)
		return false
	}

	var ipv4AddrReq ipv4Addr
	if err := binary.Read(tcp.clientR, binary.BigEndian, &ipv4AddrReq); err != nil {
		l.Error(err)
		return false

	}

	res := &res{
		Ver:  5,
		Atyp: 1,
	}
	raddr := &net.TCPAddr{
		IP:   ipv4AddrReq.Addr[:],
		Port: int(ipv4AddrReq.Port),
	}
	l.Infof("Connecting to %s ...", raddr)
	server, err := net.DialTCP("tcp4", nil, raddr)
	if err != nil {
		l.Error(err)
		res.Rep = 1 // TODO return better error?
		binary.Write(tcp.clientW, binary.BigEndian, res)
		return false
	}

	if err = binary.Write(tcp.clientW, binary.BigEndian, res); err != nil {
		l.Error(err)
		return false
	}

	tcp.server = server
	laddr := server.LocalAddr().(*net.TCPAddr)
	var ipv4AddrRes ipv4Addr
	copy(ipv4AddrRes.Addr[:], laddr.IP.To4())
	ipv4AddrRes.Port = uint16(laddr.Port)

	if err := binary.Write(tcp.clientW, binary.BigEndian, &ipv4AddrRes); err != nil {
		l.Error(err)
		return false
	}

	l.Infof("Connection %s->%s is established.", laddr, raddr)
	return true
}

func (tcp *TCPConn) Run(ctx context.Context) {
	go func() {
		if _, err := io.Copy(tcp.server, tcp.clientR); err != nil {
			tcp.l.Errorf("Failed to read from the client: %s.", err)
		}
	}()
	if _, err := io.Copy(tcp.clientW, tcp.server); err != nil {
		tcp.l.Errorf("Failed to read from the server: %s.", err)
	}
}
