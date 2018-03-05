package backend

import (
	"net/url"
	"github.com/pierredavidbelanger/raftman/api"
	"fmt"
	"github.com/blevesearch/bleve"
	"os"
	"github.com/pierredavidbelanger/raftman/utils"
	"github.com/pborman/uuid"
	"log"
	"time"
	"github.com/blevesearch/bleve/search/query"
)

type bleveBackend struct {
	asyncBackend
	indexPath string
	batchSize int
	index     bleve.Index
}

func newBleveBackend(backendURL *url.URL) (*bleveBackend, error) {

	b := bleveBackend{}
	err := initAsyncBackend(backendURL, &b.asyncBackend)
	if err != nil {
		return nil, err
	}

	indexPath := backendURL.Path
	if indexPath == "" {
		return nil, fmt.Errorf("Invalid index file path %q", indexPath)
	}
	b.indexPath = indexPath

	batchSize, err := utils.GetIntQueryParam(backendURL, "batchSize", 256)
	if err != nil {
		return nil, err
	}
	b.batchSize = batchSize

	return &b, nil
}

func (b *bleveBackend) Start() error {

	if _, err := os.Stat(b.indexPath); err == nil {
		index, err := bleve.Open(b.indexPath)
		if err != nil {
			return err
		}
		b.index = index
	} else {
		mapping := bleve.NewIndexMapping()
		index, err := bleve.New(b.indexPath, mapping)
		if err != nil {
			return err
		}
		b.index = index
	}

	go b.run()

	return nil
}

func (b *bleveBackend) Close() error {
	return nil
}

func (b *bleveBackend) Insert(req *api.InsertRequest) (*api.InsertResponse, error) {
	if req.Entry != nil {
		b.insertQ <- req.Entry
	}
	if len(req.Entries) > 0 {
		for _, e := range req.Entries {
			b.insertQ <- e
		}
	}
	return &api.InsertResponse{}, nil
}

func (b *bleveBackend) QueryStat(req *api.QueryRequest) (*api.QueryStatResponse, error) {
	return newQueryStatM(req).push(b.queryStatQ).pollWithTimeout(b.timeout)
}

func (b *bleveBackend) QueryList(req *api.QueryRequest) (*api.QueryListResponse, error) {
	return newQueryListM(req).push(b.queryListQ).pollWithTimeout(b.timeout)
}

func (b *bleveBackend) run() {

	batch := b.index.NewBatch()
	batchEndTicker := time.NewTicker(1 * time.Second)

	for {
		select {
		case e := <-b.insertQ:

			batch.Reset()

			err := batch.Index(uuid.New(), e)
			if err != nil {
				log.Printf("Unable to index log entry: %s", err)
				break
			}

		batchloop:
			for i := 0; i < b.batchSize; i++ {
				select {
				case e = <-b.insertQ:
					err := batch.Index(uuid.New(), e)
					if err != nil {
						log.Printf("Unable to index log entry: %s", err)
						break batchloop
					}
				case <-batchEndTicker.C:
					break batchloop
				}

			}
			log.Printf("Index %d entries", batch.Size())
			err = b.index.Batch(batch)
			if err != nil {
				log.Printf("Unable to index log enties batch: %s", err)
			}

		case m := <-b.queryStatQ:
			m.res <- &api.QueryStatResponse{}
		case m := <-b.queryListQ:

			var queries []query.Query

			if !m.req.FromTimestamp.IsZero() || !m.req.ToTimestamp.IsZero() {
				q := bleve.NewDateRangeQuery(m.req.FromTimestamp, m.req.ToTimestamp)
				queries = append(queries, q)
			}

			if m.req.Hostname != "" {
				q := bleve.NewMatchQuery(m.req.Hostname)
				q.SetField("Hostname")
				queries = append(queries, q)
			}

			if m.req.Application != "" {
				q := bleve.NewMatchQuery(m.req.Application)
				q.SetField("Application")
				queries = append(queries, q)
			}

			if m.req.Message != "" {
				q := bleve.NewMatchQuery(m.req.Message)
				q.SetField("Message")
				queries = append(queries, q)
			} else {
				q := bleve.NewMatchAllQuery()
				queries = append(queries, q)
			}

			q := bleve.NewConjunctionQuery(queries...)

			s := bleve.NewSearchRequestOptions(q, clamp(0, m.req.Limit, 256), m.req.Offset, false)
			s.SortBy([]string{"Timestamp"})
			s.Fields = []string{"Timestamp", "Hostname", "Application", "Message"}

			r, err := b.index.Search(s)
			if err != nil {
				m.res <- &api.QueryListResponse{Error: err.Error()}
				return
			}

			// log.Printf("%s", r)

			var entries []*api.LogEntry

			for _, documentMatch := range r.Hits {

				// log.Printf("%#v", documentMatch.Fields)

				e := api.LogEntry{}

				if timestampString, ok := documentMatch.Fields["Timestamp"].(string); ok {
					if timestamp, err := time.Parse(time.RFC3339, timestampString); err == nil {
						e.Timestamp = timestamp
					}
				}

				if hostname, ok := documentMatch.Fields["Hostname"].(string); ok {
					e.Hostname = hostname
				}

				if application, ok := documentMatch.Fields["Application"].(string); ok {
					e.Application = application
				}

				if message, ok := documentMatch.Fields["Message"].(string); ok {
					e.Message = message
				}

				entries = append(entries, &e)
			}

			m.res <- &api.QueryListResponse{Entries: entries}

		case cond := <-b.stopQ:
			cond.Broadcast()
			return
		}
	}
}
