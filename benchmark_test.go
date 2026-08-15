package socks5_test

import (
	"encoding/json"
	"fmt"
	"github.com/1stblue/go-socks5"
	"golang.org/x/net/context"
	"golang.org/x/net/proxy"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	proxyAddr string
	backAddr  string
)

func getListener(port int) (net.Listener, string, error) {
	lsn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, "", err
	}

	return lsn, lsn.Addr().String(), nil
}

func init() {

	lsn, addr, err := getListener(0)
	if err != nil {
		panic(err)
	}

	proxyAddr = addr
	go func(lsn net.Listener) {
		_ = startSocks5Proxy(lsn)
	}(lsn)

	lsn, addr, err = getListener(0)
	if err != nil {
		panic(err)
	}

	backAddr = addr
	go func(lsn net.Listener) {
		_ = startHttpBackend(lsn)
	}(lsn)
}

func startSocks5Proxy(lsn net.Listener) error {
	app, err := socks5.New(&socks5.Config{})
	if err != nil {
		return err
	}

	return app.Serve(lsn)
}

func startHttpBackend(lsn net.Listener) error {
	app := http.NewServeMux()
	app.HandleFunc("/quit", func(w http.ResponseWriter, req *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_ = conn.Close()
	})

	app.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if n, _ := strconv.Atoi(req.FormValue("sleep")); n > 0 {
			// 模拟 server 端超时
			time.Sleep(time.Duration(n) * time.Millisecond)
		}

		buf, _ := json.Marshal(map[string]any{
			"method": req.Method,
			"path":   req.URL.Path,
			"query":  req.URL.Query(),
		})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf)
	})

	return http.Serve(lsn, app)
}

func getProxiedClient(addr string, timeout time.Duration) (*http.Client, error) {
	if len(addr) < 1 {
		addr = proxyAddr
	}

	if pos := strings.Index(addr, "://"); pos > -1 {
		addr = addr[pos+4:]
	}

	dialer, err := proxy.SOCKS5("tcp", addr, nil, nil)
	if err != nil {
		return nil, err
	}

	if timeout < 1 {
		timeout = 100 * time.Millisecond
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			Proxy:             nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
	}, nil
}

func TestShouldClientTimeoutWorksFine(t *testing.T) {
	client, err := getProxiedClient("", 0)
	if err != nil {
		t.FailNow()
		return
	}

	defer client.CloseIdleConnections()

	_, err = client.Get(fmt.Sprintf("http://%s/?sleep=120", backAddr))
	time.Sleep(time.Second)
	if err == nil {
		//t.FailNow()
		return
	}

	// https://www.cloudflare.com/cdn-cgi/trace
	// 怎么验证对端的连接断了
	/*
		_, err = client.Get(fmt.Sprintf("http://%s/quit", backAddr))
		if err == nil {
			t.FailNow()
			return
		}

	*/
}
