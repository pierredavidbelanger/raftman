package backend

import (
	"fmt"
	"github.com/pierredavidbelanger/raftman/spi"
	"net/url"
)

func NewBackend(e spi.LogEngine, backendURL *url.URL) (spi.LogBackend, error) {
	switch backendURL.Scheme {
	case "sqlite":
		return newSQLiteBackend(backendURL)
	}
	return nil, fmt.Errorf("Invalid backend %s", backendURL.Scheme)
}
