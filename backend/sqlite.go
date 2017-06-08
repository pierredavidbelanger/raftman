package backend

import (
	"database/sql"
	"net/url"
	"github.com/pierredavidbelanger/raftman/api"
	_ "github.com/mattn/go-sqlite3"
	"sync"
	"log"
	"path/filepath"
	"os"
	"github.com/pierredavidbelanger/raftman/utils"
	"fmt"
	"bytes"
	"math"
	"time"
)

type sqliteBackend struct {
	asyncBackend
	batchSize  int
	retention  utils.Retention
	dbFilePath string
	db         *sql.DB
	hStmt      *sql.Stmt
	bStmt      *sql.Stmt
}

func newSQLiteBackend(backendURL *url.URL) (*sqliteBackend, error) {

	b := sqliteBackend{}
	err := initAsyncBackend(backendURL, &b.asyncBackend)
	if err != nil {
		return nil, err
	}

	batchSize, err := utils.GetIntQueryParam(backendURL, "batchSize", 32)
	if err != nil {
		return nil, err
	}
	b.batchSize = batchSize

	retention, err := utils.GetRetentionQueryParam(backendURL, "retention", utils.INF)
	if err != nil {
		return nil, err
	}
	b.retention = retention

	dbFilePath := backendURL.Path
	if dbFilePath == "" {
		return nil, fmt.Errorf("Invalid SQLite database file path '%s'", dbFilePath)
	}
	b.dbFilePath = dbFilePath

	return &b, nil
}

func (b *sqliteBackend) Start() error {

	var err error

	dbDir := filepath.Dir(b.dbFilePath)
	err = os.MkdirAll(dbDir, os.ModePerm)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlite3", b.dbFilePath)
	if err != nil {
		return err
	}
	b.db = db

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS logh (ts DATETIME, app VARCHAR(255), proc VARCHAR(255))")
	if err != nil {
		db.Close()
		return err
	}

	_, err = db.Exec("CREATE INDEX IF NOT EXISTS logh_idx ON logh (ts, app, proc)")
	if err != nil {
		db.Close()
		return err
	}

	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS logb USING FTS4(msg, tokenize=unicode61)")
	if err != nil {
		db.Close()
		return err
	}

	hStmt, err := db.Prepare("INSERT INTO logh (ts, app, proc) VALUES (?, ?, ?)")
	if err != nil {
		db.Close()
		return err
	}
	b.hStmt = hStmt

	bStmt, err := db.Prepare("INSERT INTO logb (docid, msg) VALUES (LAST_INSERT_ROWID(), ?)")
	if err != nil {
		hStmt.Close()
		db.Close()
		return err
	}
	b.bStmt = bStmt

	go b.run()

	return nil
}

func (b *sqliteBackend) Close() error {

	cond := sync.NewCond(&sync.Mutex{})
	cond.L.Lock()
	b.stopQ <- cond
	cond.Wait()
	cond.L.Unlock()

	if b.bStmt != nil {
		b.bStmt.Close()
		b.bStmt = nil
	}
	if b.hStmt != nil {
		b.hStmt.Close()
		b.hStmt = nil
	}
	if b.db != nil {
		b.db.Close()
		b.db = nil
	}

	return nil
}

func (b *sqliteBackend) Insert(req *api.InsertRequest) (*api.InsertResponse, error) {
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

func (b *sqliteBackend) QueryStat(req *api.QueryRequest) (*api.QueryStatResponse, error) {
	return newQueryStatM(req).push(b.queryStatQ).pollWithTimeout(b.timeout)
}

func (b *sqliteBackend) QueryList(req *api.QueryRequest) (*api.QueryListResponse, error) {
	return newQueryListM(req).push(b.queryListQ).pollWithTimeout(b.timeout)
}

func (b *sqliteBackend) run() {
	retentionTicker := time.NewTicker(1 * time.Hour)
	for {
		select {
		case e := <-b.insertQ:
			b.handleInsert(e)
		case m := <-b.queryStatQ:
			b.handleQueryStat(m)
		case m := <-b.queryListQ:
			b.handleQueryList(m)
		case now := <-retentionTicker.C:
			b.handleRetention(now)
		case cond := <-b.stopQ:
			cond.Broadcast()
			return
		}
	}
}

func (b *sqliteBackend) handleInsert(e *api.LogEntry) {

	var err error

	tx, err := b.db.Begin()
	if err != nil {
		log.Printf("Unable to begin transaction: %s", err)
		return
	}

	err = b.handleInsertBatch(tx, e)
	if err != nil {
		log.Printf("Unable to insert: %s", err)
		err = tx.Rollback()
		if err != nil {
			log.Printf("Unable to rollback: %s", err)
		}
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Unable to commit transaction: %s", err)
	}
}

func (b *sqliteBackend) handleInsertBatch(tx *sql.Tx, e *api.LogEntry) error {

	var err error

	err = b.insertEntry(tx, e)
	if err != nil {
		return err
	}

	for i := 0; i < b.batchSize; i++ {
		select {
		case e = <-b.insertQ:
			err = b.insertEntry(tx, e)
			if err != nil {
				return err
			}
		default:
			return nil
		}
	}

	return nil
}

func (b *sqliteBackend) insertEntry(tx *sql.Tx, e *api.LogEntry) error {
	if _, err := tx.Stmt(b.hStmt).Exec(e.Timestamp, e.Application, e.Process); err != nil {
		return err
	}
	if _, err := tx.Stmt(b.bStmt).Exec(e.Message); err != nil {
		return err
	}
	return nil
}

func (b *sqliteBackend) buildQueryFromAndWhere(req *api.QueryRequest, sqlBuf *bytes.Buffer, args *[]interface{}) {
	fmt.Fprint(sqlBuf, "FROM logh AS h JOIN logb AS b ON b.docid = h.rowid ")
	fmt.Fprint(sqlBuf, "WHERE 1=1 ")
	if !req.FromTimestamp.IsZero() {
		fmt.Fprint(sqlBuf, "AND h.ts >= ? ")
		*args = append(*args, req.FromTimestamp)
	}
	if !req.ToTimestamp.IsZero() {
		fmt.Fprint(sqlBuf, "AND h.ts < ? ")
		*args = append(*args, req.ToTimestamp)
	}
	if req.Application != "" {
		fmt.Fprint(sqlBuf, "AND h.app = ? ")
		*args = append(*args, req.Application)
		if req.Process != "" {
			fmt.Fprint(sqlBuf, "AND h.proc = ? ")
			*args = append(*args, req.Process)
		}
	}
	if req.Message != "" {
		fmt.Fprint(sqlBuf, "AND b.msg MATCH ? ")
		*args = append(*args, req.Message)
	}
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

func (b *sqliteBackend) buildQueryLimit(req *api.QueryRequest, sqlBuf *bytes.Buffer, args *[]interface{}) {
	fmt.Fprint(sqlBuf, "LIMIT ? OFFSET ? ")
	*args = append(*args, clamp(0, req.Limit, 256))
	*args = append(*args, clamp(0, req.Offset, math.MaxInt16))
}

func (b *sqliteBackend) handleQueryStat(m *queryStatM) {

	args := []interface{}{}

	sqlBuf := &bytes.Buffer{}
	fmt.Fprint(sqlBuf, "SELECT h.app, h.proc, COUNT(b.docid) ")
	b.buildQueryFromAndWhere(m.req, sqlBuf, &args)
	fmt.Fprint(sqlBuf, "GROUP BY h.app, h.proc ")
	fmt.Fprint(sqlBuf, "ORDER BY h.app, h.proc ")
	b.buildQueryLimit(m.req, sqlBuf, &args)

	res := api.QueryStatResponse{}

	rows, err := b.db.Query(sqlBuf.String(), args...)
	if err != nil {
		res.Error = err.Error()
		m.res <- &res
		return
	}
	defer rows.Close()

	stat := make(map[string]map[string]uint64)
	for rows.Next() {
		var app string
		var proc string
		var count uint64
		err = rows.Scan(&app, &proc, &count)
		if err != nil {
			res.Error = err.Error()
			m.res <- &res
			return
		}
		procs, ok := stat[app]
		if !ok {
			procs = make(map[string]uint64)
			stat[app] = procs
		}
		procs[proc] = count
	}

	err = rows.Err()
	if err != nil {
		res.Error = err.Error()
		m.res <- &res
		return
	}

	res.Stat = stat
	m.res <- &res
}

func (b *sqliteBackend) handleQueryList(m *queryListM) {

	args := []interface{}{}

	sqlBuf := &bytes.Buffer{}
	fmt.Fprint(sqlBuf, "SELECT h.ts, h.app, h.proc, b.msg ")
	b.buildQueryFromAndWhere(m.req, sqlBuf, &args)
	fmt.Fprint(sqlBuf, "ORDER BY h.ts DESC ")
	b.buildQueryLimit(m.req, sqlBuf, &args)

	res := api.QueryListResponse{}

	rows, err := b.db.Query(sqlBuf.String(), args...)
	if err != nil {
		res.Error = err.Error()
		m.res <- &res
		return
	}
	defer rows.Close()

	entries := make([]*api.LogEntry, 0, clamp(0, m.req.Limit, 500))
	for rows.Next() {
		entry := api.LogEntry{}
		err = rows.Scan(&entry.Timestamp, &entry.Application, &entry.Process, &entry.Message)
		if err != nil {
			res.Error = err.Error()
			m.res <- &res
			return
		}
		entries = append(entries, &entry)
	}

	err = rows.Err()
	if err != nil {
		res.Error = err.Error()
		m.res <- &res
		return
	}

	res.Entries = entries
	m.res <- &res
}

func (b *sqliteBackend) handleRetention(now time.Time) {

	if b.retention == utils.INF {
		// Keep all the things!
		return
	}

	upto := now.Add(-time.Duration(b.retention))

	tx, err := b.db.Begin()
	if err != nil {
		log.Printf("Unable to begin transaction: %s", err)
		return
	}

	err = b.handleRetentionBatch(tx, upto)
	if err != nil {
		log.Printf("Unable to delete: %s", err)
		err = tx.Rollback()
		if err != nil {
			log.Printf("Unable to rollback: %s", err)
		}
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Unable to commit transaction: %s", err)
	}
}

func (b *sqliteBackend) handleRetentionBatch(tx *sql.Tx, upto time.Time) error {

	var err error

	rows, err := tx.Query("SELECT rowid FROM logh AS h WHERE h.ts < ?", upto)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var rowid uint64
		err = rows.Scan(&rowid)
		if err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM logh WHERE rowid = ?", rowid)
		if err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM logb WHERE docid = ?", rowid)
		if err != nil {
			return err
		}
	}

	return nil
}
