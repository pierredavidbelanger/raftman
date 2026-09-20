package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	syslog "gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"

	"github.com/pierredavidbelanger/raftman/internal/server"
	"github.com/pierredavidbelanger/raftman/internal/store"
)

// storeConfig parses sqlite://<path>?insertQueueSize=&queryQueueSize=&timeout=&batchSize=&retention=
func storeConfig(u *url.URL) (store.Config, error) {
	cfg := store.Config{Path: u.Path}
	if u.Scheme != "sqlite" {
		return cfg, fmt.Errorf("Invalid backend %s", u.Scheme)
	}
	if cfg.Path == "" {
		return cfg, fmt.Errorf("Invalid SQLite database file path '%s'", u.Path)
	}
	var err error
	if cfg.InsertQueueSize, err = intParam(u, "insertQueueSize", 512); err != nil {
		return cfg, err
	}
	if cfg.QueryQueueSize, err = intParam(u, "queryQueueSize", 16); err != nil {
		return cfg, err
	}
	if cfg.Timeout, err = durationParam(u, "timeout", 5*time.Second); err != nil {
		return cfg, err
	}
	if cfg.BatchSize, err = intParam(u, "batchSize", 32); err != nil {
		return cfg, err
	}
	cfg.Retention = store.Infinite
	if s := u.Query().Get("retention"); s != "" {
		if cfg.Retention, err = store.ParseRetention(s); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// frontendConfig is one parsed -frontend URL: either a syslog listener or an
// HTTP server, never both.
type frontendConfig struct {
	url    *url.URL
	syslog *server.SyslogConfig
	http   *server.HTTPConfig
}

// parseFrontend parses syslog+udp://host:port?format=&queueSize=&timeout=,
// syslog+tcp://..., api+http://host:port/path/ and ui+http://host:port/path/.
func parseFrontend(u *url.URL) (frontendConfig, error) {
	fc := frontendConfig{url: u}
	switch u.Scheme {
	case "syslog+udp", "syslog+tcp":
		if u.Host == "" {
			return fc, fmt.Errorf("Empty host in frontend URL '%s'", u)
		}
		cfg := server.SyslogConfig{Network: strings.TrimPrefix(u.Scheme, "syslog+"), Addr: u.Host}
		var err error
		if cfg.Format, err = formatParam(u, "format", syslog.RFC5424); err != nil {
			return fc, err
		}
		if cfg.QueueSize, err = intParam(u, "queueSize", 512); err != nil {
			return fc, err
		}
		if cfg.Timeout, err = durationParam(u, "timeout", 0); err != nil {
			return fc, err
		}
		fc.syslog = &cfg
	case "api+http", "ui+http":
		if u.Host == "" {
			return fc, fmt.Errorf("Empty host in frontend URL '%s'", u)
		}
		fc.http = &server.HTTPConfig{Addr: u.Host, Path: u.Path, UI: u.Scheme == "ui+http"}
	default:
		return fc, fmt.Errorf("Invalid frontend %s", u.Scheme)
	}
	return fc, nil
}

func (fc frontendConfig) new(st server.Store) (frontend, error) {
	if fc.syslog != nil {
		return server.NewSyslog(*fc.syslog, st)
	}
	return server.NewHTTP(*fc.http, st)
}

func intParam(u *url.URL, name string, def int) (int, error) {
	s := u.Query().Get(name)
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}

func durationParam(u *url.URL, name string, def time.Duration) (time.Duration, error) {
	s := u.Query().Get(name)
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}

func formatParam(u *url.URL, name string, def format.Format) (format.Format, error) {
	s := u.Query().Get(name)
	switch strings.ToUpper(s) {
	case "":
		return def, nil
	case "RFC3164":
		return syslog.RFC3164, nil
	case "RFC5424":
		return syslog.RFC5424, nil
	}
	return nil, fmt.Errorf("Invalid syslog format %s", s)
}
