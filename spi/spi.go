package spi

import (
	"github.com/pierredavidbelanger/raftman/api"
	"io"
	"net/url"
)

type LogBackend interface {
	Start() error
	io.Closer
	Insert(*api.InsertRequest) (*api.InsertResponse, error)
	QueryStat(*api.QueryRequest) (*api.QueryStatResponse, error)
	QueryList(*api.QueryRequest) (*api.QueryListResponse, error)
}

type LogFrontend interface {
	Start() error
	io.Closer
}

type LogEngine interface {
	Start() error
	Wait() error
	io.Closer
	GetBackend() (*url.URL, LogBackend)
	GetFrontends() ([]*url.URL, []LogFrontend)
}
