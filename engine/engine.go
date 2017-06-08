package engine

import (
	"net/url"
	"github.com/pierredavidbelanger/raftman/spi"
	"github.com/pierredavidbelanger/raftman/backend"
	"github.com/pierredavidbelanger/raftman/frontend"
	"fmt"
	"os"
	"os/signal"
	"log"
)

type engine struct {

	backURL   *url.URL
	frontURLs []*url.URL
	back      spi.LogBackend
	fronts    []spi.LogFrontend
}

func NewEngine(backendURL *url.URL, frontendURLs []*url.URL) (spi.LogEngine, error) {

	e := engine{}

	b, err := backend.NewBackend(&e, backendURL)
	if err != nil {
		return nil, fmt.Errorf("Unable to create backend '%s': %s", backendURL, err)
	}
	e.backURL = backendURL
	e.back = b

	for _, frontendURL := range frontendURLs {
		f, err := frontend.NewFrontend(&e, frontendURL)
		if err != nil {
			return nil, fmt.Errorf("Unable to create frontend '%s': %s", frontendURL, err)
		}
		e.frontURLs = append(e.frontURLs, frontendURL)
		e.fronts = append(e.fronts, f)
	}

	return &e, nil
}

func (e *engine) Start() error {

	log.Printf("Start backend '%s'", e.backURL)
	if err := e.back.Start(); err != nil {
		return fmt.Errorf("Unable to start backend '%s': %s", e.backURL, err)
	}

	for i, f := range e.fronts {
		log.Printf("Start frontend '%s'", e.frontURLs[i])
		if err := f.Start(); err != nil {
			return fmt.Errorf("Unable to start frontend '%s': %s", e.frontURLs[i], err)
		}
	}

	return nil
}

func (e *engine) Close() error {

	for i, f := range e.fronts {
		if err := f.Close(); err != nil {
			fmt.Printf("Unable to close frontend '%s': %s", e.frontURLs[i], err)
		}
	}

	if err := e.back.Close(); err != nil {
		fmt.Printf("Unable to start backend '%s': %s", e.backURL, err)
	}

	return nil
}

func (e *engine) Wait() error {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	return nil
}

func (e *engine) GetBackend() (*url.URL, spi.LogBackend) {
	return e.backURL, e.back
}

func (e *engine) GetFrontends() ([]*url.URL, []spi.LogFrontend) {
	return e.frontURLs, e.fronts
}
