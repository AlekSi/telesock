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
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

	go func() {
		<-ctx.Done()
		tcp.Close()
		l.Infof("Listener closed.")
	}()

	var wg sync.WaitGroup
	l.Infof("Listener started on %s.", tcp.Addr())
	for {
		c, err := tcp.Accept()
		if err != nil {
			// are we done?
			if ctx.Err() != nil {
				break
			}

			// wait a little before next accept attempt to give OS a chance to free resources
			l.Error(err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		conn := c.(*net.TCPConn)
		if err = conn.SetReadBuffer(4096); err != nil {
			l.Warn(err)
		}
		if err = conn.SetWriteBuffer(4096); err != nil {
			l.Warn(err)
		}

		wg.Add(1)
		go runTCPConn(ctx, conn, l.With(zap.String("client", c.RemoteAddr().String())), conf)
	}

	wg.Wait()
}

func loadConfig(path string, l *zap.SugaredLogger, port string) *internal.Config {
	// read and parse config
	b, err := ioutil.ReadFile(path)
	if err != nil {
		l.Fatalf("Can't read configuration file: %s.", err)
	}
	var config internal.Config
	if err = yaml.UnmarshalStrict(b, &config); err != nil {
		l.Fatalf("Can't read configuration: %s.", err)
	}

	l.Infof("Loaded %d users.", len(config.Users))
	if config.Server == "" {
		return &config
	}

	u := &url.URL{
		Scheme: "https",
		Host:   "t.me",
		Path:   "socks",
	}
	for _, user := range config.Users {
		q := make(url.Values)
		q.Set("server", config.Server)
		q.Set("port", port)
		q.Set("user", user.Username)
		q.Set("pass", user.Password)
		u.RawQuery = q.Encode()

		l.Infof("%20s: %s", user.Username, u.String())
	}

	return &config
}

func main() {
	// parse flags
	tcpListenF := kingpin.Flag("tcp-listen", "TCP address to listen").Default(":1080").String()
	configF := kingpin.Flag("config", "Config file name").Default("telesock.yaml").String()
	verboseF := kingpin.Flag("verbose", "Log INFO level log messages").Bool()
	debugF := kingpin.Flag("debug", "Log DEBUG level log messages (implies --verbose)").Bool()
	kingpin.Parse()

	// setup logger
	loggerConfig := zap.NewDevelopmentConfig()
	loggerConfig.DisableStacktrace = true
	logger, err := loggerConfig.Build()
	if err != nil {
		panic(err)
	}
	l := logger.Sugar()
	defer l.Sync()

	_, port, err := net.SplitHostPort(*tcpListenF)
	if err != nil {
		l.Fatal(err)
	}

	config := loadConfig(*configF, l, port)

	// set logger level after config is parsed
	switch {
	case *debugF:
		loggerConfig.Level.SetLevel(zap.DebugLevel)
	case *verboseF:
		loggerConfig.Level.SetLevel(zap.InfoLevel)
	default:
		loggerConfig.Level.SetLevel(zap.WarnLevel)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// handle termination signals
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-signals
		signal.Stop(signals)
		l.Warnf("Got %v (%d) signal, shutting down...", s, s)
		cancel()
	}()

	var wg sync.WaitGroup

	// start TCP listener
	wg.Add(1)
	go func() {
		defer wg.Done()
		runTCPListener(ctx, *tcpListenF, l.With(zap.String("component", "tcp")), config)
	}()

	wg.Wait()
}
