package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/eschgi/share/server/internal/config"
)

// health asks the running server on this machine whether it answers, as a container's health
// check does.
func health(args []string) error {
	f := newFlags("health")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	u, err := healthURL(cfg)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	if strings.HasPrefix(u, "https:") {
		// Only "ok" comes back and nothing secret is sent, so any certificate will do.
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	res, err := client.Get(u)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", u, res.Status)
	}
	fmt.Println("ok")
	return nil
}

// healthURL is where `share health` asks: the http port on this machine, or the https port
// when plain http is switched off.
func healthURL(cfg *config.Config) (string, error) {
	scheme, listen := "http", ""
	switch {
	case cfg.HTTP != nil:
		listen = cfg.HTTP.Listen
	case cfg.HTTPS != nil:
		scheme, listen = "https", cfg.HTTPS.Listen
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	if port == "0" {
		return "", errors.New("the server listens on any free port (0); give it a fixed one to check it")
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/healthz", nil
}

// proxyLine says which proxy Share trusts, for `share check`.
func proxyLine(cfg *config.Config) string {
	if cfg.Proxy == nil {
		return "Proxy: none; requests that come through a proxy are refused"
	}
	return fmt.Sprintf("Proxy: %s, from %s", cfg.Proxy.Headers, strings.Join(cfg.Proxy.TrustedProxies, ", "))
}
