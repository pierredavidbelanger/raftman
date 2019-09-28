package backend

import (
	"fmt"
	"github.com/pierredavidbelanger/raftman/api"
	"github.com/pierredavidbelanger/raftman/utils"
	"net/url"
	"sync"
	"time"
)

type queryStatM struct {
	req *api.QueryRequest
	res chan *api.QueryStatResponse
}

func newQueryStatM(req *api.QueryRequest) *queryStatM {
	return &queryStatM{req, make(chan *api.QueryStatResponse, 1)}
}

func (m *queryStatM) push(c chan *queryStatM) *queryStatM {
	c <- m
	return m
}

func (m *queryStatM) pollWithTimeout(d time.Duration) (*api.QueryStatResponse, error) {
	t := time.NewTimer(d)
	select {
	case v := <-m.res:
		return v, nil
	case <-t.C:
		return nil, fmt.Errorf("operation timed out after %s", d)
	}
}

type queryListM struct {
	req *api.QueryRequest
	res chan *api.QueryListResponse
}

func newQueryListM(req *api.QueryRequest) *queryListM {
	return &queryListM{req, make(chan *api.QueryListResponse, 1)}
}

func (m *queryListM) push(c chan *queryListM) *queryListM {
	c <- m
	return m
}

func (m *queryListM) pollWithTimeout(d time.Duration) (*api.QueryListResponse, error) {
	t := time.NewTimer(d)
	select {
	case v := <-m.res:
		return v, nil
	case <-t.C:
		return nil, fmt.Errorf("operation timed out after %s", d)
	}
}

type asyncBackend struct {
	insertQ    chan *api.LogEntry
	queryStatQ chan *queryStatM
	queryListQ chan *queryListM
	stopQ      chan *sync.Cond
	timeout    time.Duration
}

func initAsyncBackend(backendURL *url.URL, b *asyncBackend) error {
	insertQueueSize, err := utils.GetIntQueryParam(backendURL, "insertQueueSize", 512)
	if err != nil {
		return err
	}
	queryQueueSize, err := utils.GetIntQueryParam(backendURL, "queryQueueSize", 16)
	if err != nil {
		return err
	}
	timeout, err := utils.GetDurationQueryParam(backendURL, "timeout", 5*time.Second)
	if err != nil {
		return err
	}
	b.insertQ = make(chan *api.LogEntry, insertQueueSize)
	b.queryStatQ = make(chan *queryStatM, queryQueueSize)
	b.queryListQ = make(chan *queryListM, queryQueueSize)
	b.stopQ = make(chan *sync.Cond, 1)
	b.timeout = timeout
	return nil
}
