// Package server holds the network frontends: syslog listeners that feed a
// Store, and the HTTP API and web UI that query it.
package server

import (
	"context"

	"github.com/pierredavidbelanger/raftman/api"
)

type Store interface {
	Insert(*api.LogEntry)
	QueryStat(context.Context, *api.QueryRequest) (*api.QueryStatResponse, error)
	QueryList(context.Context, *api.QueryRequest) (*api.QueryListResponse, error)
}
