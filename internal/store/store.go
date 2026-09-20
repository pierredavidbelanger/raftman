// Package store persists log entries in an SQLite file with full text search.
//
// The on-disk layout is frozen: a logh table (ts, host, app) and an FTS4
// virtual table logb (msg) joined on rowid, timestamps stored as the string
// the mattn driver produces for time.Time. Databases written by every earlier
// raftman version must stay readable; see testdata/legacy.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/pierredavidbelanger/raftman/api"
)

type Config struct {
	Path            string        // database file; parent directories are created
	InsertQueueSize int           // entries buffered between Insert and the writer
	QueryQueueSize  int           // queries allowed to run concurrently
	Timeout         time.Duration // deadline for one query
	BatchSize       int           // extra entries the writer adds to a transaction when they are already queued
	Retention       Retention     // entries older than this are purged hourly
}

// Store owns one writer goroutine that commits queued entries in batches and
// purges old ones. Queries run concurrently on the connection pool.
type Store struct {
	cfg     Config
	db      *sql.DB
	inserts chan *api.LogEntry
	done    chan struct{}

	mu     sync.RWMutex // guards closed
	closed bool
}

const schema = `
CREATE TABLE IF NOT EXISTS logh (ts DATETIME, host VARCHAR(255), app VARCHAR(255));
CREATE INDEX IF NOT EXISTS logh_idx ON logh (ts, host, app);
CREATE VIRTUAL TABLE IF NOT EXISTS logb USING FTS4(msg, tokenize=unicode61);
`

func Open(cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("Invalid SQLite database file path ''")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), os.ModePerm); err != nil {
		return nil, err
	}
	// WAL lets queries run while the writer commits. It is persistent in the
	// file header and readable by every SQLite since 3.7, so old raftman
	// versions can still open the file.
	db, err := sql.Open("sqlite3", cfg.Path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.QueryQueueSize + 1) // +1 for the writer
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{
		cfg:     cfg,
		db:      db,
		inserts: make(chan *api.LogEntry, cfg.InsertQueueSize),
		done:    make(chan struct{}),
	}
	go s.writer()
	return s, nil
}

// Insert queues an entry. It blocks while the queue is full and drops the
// entry once the store is closed.
func (s *Store) Insert(e *api.LogEntry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	s.inserts <- e
}

// Close stops accepting entries, writes everything still queued, and closes
// the database.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.inserts)
	s.mu.Unlock()
	<-s.done
	return s.db.Close()
}

func (s *Store) writer() {
	defer close(s.done)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-s.inserts:
			if !ok {
				return
			}
			s.write(s.batch(e))
		case now := <-ticker.C:
			s.purge(now)
		}
	}
}

// batch returns e plus up to BatchSize entries that are already queued.
func (s *Store) batch(e *api.LogEntry) []*api.LogEntry {
	entries := []*api.LogEntry{e}
	for len(entries) <= s.cfg.BatchSize {
		select {
		case e, ok := <-s.inserts:
			if !ok {
				return entries
			}
			entries = append(entries, e)
		default:
			return entries
		}
	}
	return entries
}

func (s *Store) write(entries []*api.LogEntry) {
	err := s.tx(func(tx *sql.Tx) error {
		head, err := tx.Prepare("INSERT INTO logh (ts, host, app) VALUES (?, ?, ?)")
		if err != nil {
			return err
		}
		defer head.Close()
		body, err := tx.Prepare("INSERT INTO logb (docid, msg) VALUES (LAST_INSERT_ROWID(), ?)")
		if err != nil {
			return err
		}
		defer body.Close()
		for _, e := range entries {
			// Stored in UTC: ts is compared as a string, so a single offset
			// keeps ordering and range filters right across senders.
			if _, err := head.Exec(e.Timestamp.UTC(), e.Hostname, e.Application); err != nil {
				return err
			}
			if _, err := body.Exec(e.Message); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Unable to insert %d entries: %s", len(entries), err)
	}
}

func (s *Store) purge(now time.Time) {
	if s.cfg.Retention < 0 {
		return
	}
	upto := now.Add(-time.Duration(s.cfg.Retention))
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM logb WHERE docid IN (SELECT rowid FROM logh WHERE ts < ?)", upto); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM logh WHERE ts < ?", upto)
		return err
	})
	if err != nil {
		log.Printf("Unable to purge entries older than %s: %s", upto, err)
	}
}

func (s *Store) tx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// QueryStat counts entries per hostname and application. An SQL error is
// reported in the response; only a timeout is returned as an error.
func (s *Store) QueryStat(ctx context.Context, req *api.QueryRequest) (*api.QueryStatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	res := &api.QueryStatResponse{}
	where, args := where(req)
	rows, err := s.db.QueryContext(ctx,
		"SELECT h.host, h.app, COUNT(b.docid) "+where+" GROUP BY h.host, h.app ORDER BY h.host, h.app LIMIT ? OFFSET ?",
		append(args, limit(req), offset(req))...)
	if err != nil {
		res.Error, err = s.fail(ctx, err)
		return res, err
	}
	defer rows.Close()
	stat := make(map[string]map[string]uint64)
	for rows.Next() {
		var host, app string
		var n uint64
		if err := rows.Scan(&host, &app, &n); err != nil {
			res.Error, err = s.fail(ctx, err)
			return res, err
		}
		if stat[host] == nil {
			stat[host] = make(map[string]uint64)
		}
		stat[host][app] = n
	}
	if err := rows.Err(); err != nil {
		res.Error, err = s.fail(ctx, err)
		return res, err
	}
	res.Stat = stat
	return res, nil
}

// QueryList returns matching entries, newest first. An SQL error is reported
// in the response; only a timeout is returned as an error.
func (s *Store) QueryList(ctx context.Context, req *api.QueryRequest) (*api.QueryListResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	res := &api.QueryListResponse{}
	where, args := where(req)
	rows, err := s.db.QueryContext(ctx,
		"SELECT h.ts, h.host, h.app, b.msg "+where+" ORDER BY h.ts DESC LIMIT ? OFFSET ?",
		append(args, limit(req), offset(req))...)
	if err != nil {
		res.Error, err = s.fail(ctx, err)
		return res, err
	}
	defer rows.Close()
	var entries []*api.LogEntry
	for rows.Next() {
		e := &api.LogEntry{}
		if err := rows.Scan(&e.Timestamp, &e.Hostname, &e.Application, &e.Message); err != nil {
			res.Error, err = s.fail(ctx, err)
			return res, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		res.Error, err = s.fail(ctx, err)
		return res, err
	}
	res.Entries = entries
	return res, nil
}

// fail sorts a query failure: a timeout becomes the returned error, anything
// else becomes the message reported in the response.
func (s *Store) fail(ctx context.Context, err error) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("operation timed out after %s", s.cfg.Timeout)
	}
	return err.Error(), nil
}

func where(req *api.QueryRequest) (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString("FROM logh AS h JOIN logb AS b ON b.docid = h.rowid WHERE 1=1")
	if !req.FromTimestamp.IsZero() {
		b.WriteString(" AND h.ts >= ?")
		args = append(args, req.FromTimestamp)
	}
	if !req.ToTimestamp.IsZero() {
		b.WriteString(" AND h.ts < ?")
		args = append(args, req.ToTimestamp)
	}
	if req.Hostname != "" {
		b.WriteString(" AND h.host = ?")
		args = append(args, req.Hostname)
	}
	if req.Application != "" {
		b.WriteString(" AND h.app = ?")
		args = append(args, req.Application)
	}
	if req.Message != "" {
		b.WriteString(" AND b.msg MATCH ?")
		args = append(args, req.Message)
	}
	return b.String(), args
}

func limit(req *api.QueryRequest) int  { return min(max(req.Limit, 0), 256) }
func offset(req *api.QueryRequest) int { return min(max(req.Offset, 0), math.MaxInt16) }
