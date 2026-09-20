// Package api holds the JSON types of the HTTP API. Field names and omitempty
// tags are part of the public contract; do not change them.
package api

import "time"

type LogEntry struct {
	Timestamp   time.Time
	Hostname    string
	Application string
	Message     string
}

type QueryRequest struct {
	FromTimestamp time.Time
	ToTimestamp   time.Time
	Hostname      string
	Application   string
	Message       string
	Limit         int
	Offset        int
}

type QueryStatResponse struct {
	Stat  map[string]map[string]uint64 `json:",omitempty"`
	Error string                       `json:",omitempty"`
}

type QueryListResponse struct {
	Entries []*LogEntry `json:",omitempty"`
	Error   string      `json:",omitempty"`
}
