package frontend

import (
	"github.com/pierredavidbelanger/raftman/spi"
	"net/url"
	"net/http"
	"net"
	"fmt"
)

type webFrontend struct {
	e    spi.LogEngine
	b    spi.LogBackend
	addr string
	path string
	s    *http.Server
}

func initWebFrontend(e spi.LogEngine, frontendURL *url.URL, f *webFrontend) error {
	f.e = e
	if frontendURL.Host == "" {
		return fmt.Errorf("Empty host in frontend URL '%s'", frontendURL)
	}
	f.addr = frontendURL.Host
	f.path = frontendURL.Path
	return nil
}

func (f *webFrontend) startHandler(h http.Handler) error {

	_, b := f.e.GetBackend()
	f.b = b

	f.s = &http.Server{Addr: f.addr, Handler: h}

	ln, err := net.Listen("tcp", f.addr)
	if err != nil {
		return err
	}

	go f.s.Serve(ln)

	return nil
}

func (f *webFrontend) close() error {
	return f.s.Close()
}
