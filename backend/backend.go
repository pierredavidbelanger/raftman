package backend

import (
	"net/url"
	"github.com/pierredavidbelanger/raftman/spi"
	"fmt"
)

func NewBackend(e spi.LogEngine, backendURL *url.URL) (spi.LogBackend, error) {
	switch backendURL.Scheme {
	case "sqlite":
		return newSQLiteBackend(backendURL)
	}
	return nil, fmt.Errorf("Invalid backend %s", backendURL.Scheme)
}
