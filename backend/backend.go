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
	case "bleve":
		return newBleveBackend(backendURL)
	}
	return nil, fmt.Errorf("Invalid backend %s", backendURL.Scheme)
}

func clamp(min, v, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
