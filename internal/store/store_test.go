package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pierredavidbelanger/raftman/api"
)

func open(t *testing.T, retention Retention) *Store {
	t.Helper()
	s, err := Open(Config{
		Path:            filepath.Join(t.TempDir(), "r.db"),
		InsertQueueSize: 512,
		QueryQueueSize:  16,
		Timeout:         5 * time.Second,
		BatchSize:       32,
		Retention:       retention,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func list(t *testing.T, s *Store, req *api.QueryRequest) []*api.LogEntry {
	t.Helper()
	res, err := s.QueryList(context.Background(), req)
	if err != nil || res.Error != "" {
		t.Fatalf("%v %s", err, res.Error)
	}
	return res.Entries
}

func waitInserted(t *testing.T, s *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(list(t, s, &api.QueryRequest{Limit: 100})) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d entries", want)
}

func messages(t *testing.T, s *Store) []string {
	t.Helper()
	var out []string
	for _, e := range list(t, s, &api.QueryRequest{Limit: 100}) {
		out = append(out, e.Message)
	}
	return out
}

func seed(t *testing.T, s *Store, now time.Time) {
	t.Helper()
	s.Insert(&api.LogEntry{Timestamp: now.Add(-48 * time.Hour), Hostname: "h", Application: "a", Message: "old"})
	s.Insert(&api.LogEntry{Timestamp: now.Add(-24 * time.Hour), Hostname: "h", Application: "a", Message: "edge"})
	s.Insert(&api.LogEntry{Timestamp: now.Add(-1 * time.Hour), Hostname: "h", Application: "a", Message: "fresh"})
	waitInserted(t, s, 3)
}

func TestRetentionPurgesOlderThanRetention(t *testing.T) {
	s := open(t, Retention(24*time.Hour))
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	seed(t, s, now)
	s.purge(now)
	got := messages(t, s)
	if len(got) != 2 || got[0] != "fresh" || got[1] != "edge" {
		t.Errorf("got %v, want [fresh edge]", got)
	}
}

func TestRetentionInfiniteKeepsEverything(t *testing.T) {
	s := open(t, Infinite)
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	seed(t, s, now)
	s.purge(now)
	if got := messages(t, s); len(got) != 3 {
		t.Errorf("got %v, want 3 entries", got)
	}
}

func TestRetentionRemovesSearchableText(t *testing.T) {
	s := open(t, Retention(24*time.Hour))
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	seed(t, s, now)
	s.purge(now)
	if got := list(t, s, &api.QueryRequest{Limit: 100, Message: "old"}); len(got) != 0 {
		t.Errorf("purged entry still matches: %v", got)
	}
}

func TestCloseWritesQueuedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.db")
	cfg := Config{Path: path, InsertQueueSize: 1000, QueryQueueSize: 2, Timeout: time.Second, BatchSize: 1, Retention: Infinite}
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		s.Insert(&api.LogEntry{Timestamp: time.Now(), Hostname: "h", Application: "a", Message: "m"})
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s.Insert(&api.LogEntry{Message: "after close"}) // must not panic
	s, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, err := s.QueryStat(context.Background(), &api.QueryRequest{Limit: 10})
	if err != nil || res.Stat["h"]["a"] != 300 {
		t.Errorf("got %v %v, want 300 entries", res, err)
	}
}

func TestQueryTimeoutIsAnError(t *testing.T) {
	s, err := Open(Config{Path: filepath.Join(t.TempDir(), "t.db"), InsertQueueSize: 1, QueryQueueSize: 1, Timeout: time.Nanosecond, BatchSize: 1, Retention: Infinite})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.QueryList(context.Background(), &api.QueryRequest{Limit: 1}); err == nil || err.Error() != "operation timed out after 1ns" {
		t.Errorf("got %v", err)
	}
	if _, err := s.QueryStat(context.Background(), &api.QueryRequest{Limit: 1}); err == nil {
		t.Error("stat: expected a timeout error")
	}
}

func TestSQLErrorIsReportedInResponse(t *testing.T) {
	s := open(t, Infinite)
	res, err := s.QueryList(context.Background(), &api.QueryRequest{Limit: 1, Message: `"`})
	if err != nil || res.Error == "" {
		t.Errorf("got %v %v, want a response with Error set", res, err)
	}
}
