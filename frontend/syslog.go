package frontend

import (
	"fmt"
	"github.com/pierredavidbelanger/raftman/api"
	"github.com/pierredavidbelanger/raftman/spi"
	"github.com/pierredavidbelanger/raftman/utils"
	"gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"
	"net/url"
	"strings"
	"sync"
	"time"
)

type syslogServerFrontend struct {
	e      spi.LogEngine
	b      spi.LogBackend
	logsQ  syslog.LogPartsChannel
	stopQ  chan *sync.Cond
	format format.Format
	server *syslog.Server
}

func newSyslogServerFrontend(e spi.LogEngine, frontendURL *url.URL) (*syslogServerFrontend, error) {

	if frontendURL.Host == "" {
		return nil, fmt.Errorf("Empty host in frontend URL '%s'", frontendURL)
	}

	syslogFormat, err := utils.GetSyslogFormatQueryParam(frontendURL, "format", syslog.RFC5424)
	if err != nil {
		return nil, err
	}

	queueSize, err := utils.GetIntQueryParam(frontendURL, "queueSize", 512)
	if err != nil {
		return nil, err
	}

	timeout, err := utils.GetDurationQueryParam(frontendURL, "timeout", 0*time.Second)
	if err != nil {
		return nil, err
	}

	f := syslogServerFrontend{}
	f.e = e

	logsQ := make(syslog.LogPartsChannel, queueSize)
	f.logsQ = logsQ

	stopQ := make(chan *sync.Cond, 1)
	f.stopQ = stopQ

	f.format = syslogFormat

	server := syslog.NewServer()
	server.SetFormat(syslogFormat)
	server.SetTimeout(int64(timeout.Seconds() * 1000))
	server.SetHandler(syslog.NewChannelHandler(logsQ))
	switch strings.ToLower(frontendURL.Scheme) {
	case "syslog+tcp":
		err = server.ListenTCP(frontendURL.Host)
	case "syslog+udp":
		err = server.ListenUDP(frontendURL.Host)
	}
	if err != nil {
		return nil, err
	}
	f.server = server

	return &f, nil
}

func (f *syslogServerFrontend) Start() error {

	_, b := f.e.GetBackend()
	f.b = b

	err := f.server.Boot()
	if err != nil {
		return err
	}

	go f.run()

	return nil
}

func (f *syslogServerFrontend) Close() error {

	cond := sync.NewCond(&sync.Mutex{})
	cond.L.Lock()
	f.stopQ <- cond
	cond.Wait()
	cond.L.Unlock()

	return f.server.Kill()
}

func (f *syslogServerFrontend) run() {
	for {
		select {
		case logParts := <-f.logsQ:
			f.b.Insert(&api.InsertRequest{Entry: f.toLogEntry(logParts)})
		case cond := <-f.stopQ:
			cond.Broadcast()
			return
		}
	}
}

func (f *syslogServerFrontend) toLogEntry(logParts format.LogParts) *api.LogEntry {
	e := api.LogEntry{}
	switch f.format {
	case syslog.RFC3164:
		if val, ok := logParts["timestamp"].(time.Time); ok {
			e.Timestamp = val
		} else {
			e.Timestamp = time.Now()
		}
		if val, ok := logParts["hostname"].(string); ok {
			e.Hostname = val
		}
		if val, ok := logParts["tag"].(string); ok {
			e.Application = val
		}
		if val, ok := logParts["content"].(string); ok {
			e.Message = val
		}
	case syslog.RFC5424:
		if val, ok := logParts["timestamp"].(time.Time); ok {
			e.Timestamp = val
		} else {
			e.Timestamp = time.Now()
		}
		if val, ok := logParts["hostname"].(string); ok {
			e.Hostname = val
		}
		if val, ok := logParts["app_name"].(string); ok {
			e.Application = val
		}
		if val, ok := logParts["message"].(string); ok {
			e.Message = val
		}
	}
	return &e
}
