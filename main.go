// telesock - Fast and simple SOCKS5 proxy.
// Written in 2018 by Alexey Palazhchenko.
//
// To the extent possible under law, the author(s) have dedicated all copyright and related and neighboring rights
// to this software to the public domain worldwide. This software is distributed without any warranty.
//
// You should have received a copy of the CC0 Public Domain Dedication along with this software.
// If not, see <http://creativecommons.org/publicdomain/zero/1.0/>.

package main

import (
	"context"
	"io/ioutil"
	"net"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v2"

	"github.com/AlekSi/telesock/internal"
)

func runTCPConn(ctx context.Context, c *net.TCPConn, l *zap.SugaredLogger, conf *internal.Config) {
	tcp := internal.NewTCPConn(c, l, conf)
	defer tcp.Close()

	if !tcp.Auth(ctx) {
		return
	}
	if !tcp.Req(ctx) {
		return
	}
	tcp.Run(ctx)
}

func runTCPListener(ctx context.Context, addr string, l *zap.SugaredLogger, conf *internal.Config) {
	tcp, err := net.Listen("tcp", addr)
	if err != nil {
		l.Error(err)
		return
	}

	l.Infof("Listener started on %s.", tcp.Addr())
	for {
		c, err := tcp.Accept()
		if err != nil {
			l.Error(err)
			continue
		}

		conn := c.(*net.TCPConn)
		if err = conn.SetNoDelay(false); err != nil { // TODO do we need this?
			l.Warn(err)
		}
		if err = conn.SetReadBuffer(4096); err != nil {
			l.Warn(err)
		}
		if err = conn.SetWriteBuffer(4096); err != nil {
			l.Warn(err)
		}

		go runTCPConn(ctx, conn, l.With(zap.String("client", c.RemoteAddr().String())), conf)
	}
}

func main() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	l := logger.Sugar()
	defer l.Sync()

	tcpListenF := kingpin.Flag("tcp-listen", "TCP address to listen").Default(":1080").String()
	configF := kingpin.Flag("config", "Config file name").Default("telesock.yaml").String()
	kingpin.Parse()

	b, err := ioutil.ReadFile(*configF)
	if err != nil {
		l.Fatal(err)
	}

	var config internal.Config
	if err = yaml.UnmarshalStrict(b, &config); err != nil {
		l.Fatal(err)
	}
	l.Infof("Loaded %d users.", len(config.Users))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		runTCPListener(ctx, *tcpListenF, l.With(zap.String("component", "tcp")), &config)
	}()

	wg.Wait()
}
