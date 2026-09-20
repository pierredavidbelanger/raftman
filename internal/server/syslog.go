package server

import (
	"fmt"
	"time"

	syslog "gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"

	"github.com/pierredavidbelanger/raftman/api"
)

type SyslogConfig struct {
	Network   string // "udp" or "tcp"
	Addr      string
	Format    format.Format
	QueueSize int           // parsed messages buffered before the store
	Timeout   time.Duration // TCP connection idle timeout; 0 disables it
}

// Syslog receives syslog packets and forwards them to the store.
type Syslog struct {
	cfg     SyslogConfig
	store   Store
	server  *syslog.Server
	parts   syslog.LogPartsChannel
	done    chan struct{}
	started bool
}

// NewSyslog binds the listening socket; Start begins receiving.
func NewSyslog(cfg SyslogConfig, store Store) (*Syslog, error) {
	s := &Syslog{
		cfg:    cfg,
		store:  store,
		server: syslog.NewServer(),
		parts:  make(syslog.LogPartsChannel, cfg.QueueSize),
		done:   make(chan struct{}),
	}
	s.server.SetFormat(cfg.Format)
	s.server.SetTimeout(cfg.Timeout.Milliseconds())
	s.server.SetHandler(syslog.NewChannelHandler(s.parts))
	var err error
	switch cfg.Network {
	case "udp":
		err = s.server.ListenUDP(cfg.Addr)
	case "tcp":
		err = s.server.ListenTCP(cfg.Addr)
	default:
		err = fmt.Errorf("unknown network %q", cfg.Network)
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Syslog) Start() error {
	if err := s.server.Boot(); err != nil {
		return err
	}
	s.started = true
	go s.forward()
	return nil
}

func (s *Syslog) forward() {
	defer close(s.done)
	for parts := range s.parts {
		s.store.Insert(toEntry(s.cfg.Format, parts))
	}
}

// Close stops listening, waits for go-syslog to hand over what it already
// parsed, then forwards everything still queued to the store.
func (s *Syslog) Close() error {
	err := s.server.Kill()
	if !s.started {
		return err
	}
	s.server.Wait()
	close(s.parts)
	<-s.done
	return err
}

func toEntry(f format.Format, parts format.LogParts) *api.LogEntry {
	e := &api.LogEntry{Timestamp: time.Now()}
	// A packet without a timestamp ("-" in RFC5424) yields the zero time;
	// the arrival time is used instead.
	if ts, ok := parts["timestamp"].(time.Time); ok && !ts.IsZero() {
		e.Timestamp = ts
	}
	e.Hostname, _ = parts["hostname"].(string)
	switch f {
	case syslog.RFC3164:
		e.Application, _ = parts["tag"].(string)
		e.Message, _ = parts["content"].(string)
	default:
		e.Application, _ = parts["app_name"].(string)
		e.Message, _ = parts["message"].(string)
	}
	return e
}
