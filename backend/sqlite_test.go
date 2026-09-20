package backend

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/pierredavidbelanger/raftman/api"
)

func startBackend(t *testing.T, query string) *sqliteBackend {
	t.Helper()
	u, err := url.Parse("sqlite://" + filepath.Join(t.TempDir(), "r.db") + query)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newSQLiteBackend(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func waitInserted(t *testing.T, b *sqliteBackend, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := b.QueryList(&api.QueryRequest{Limit: 100})
		if err == nil && res.Error == "" && len(res.Entries) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d entries", want)
}

func messages(t *testing.T, b *sqliteBackend) []string {
	t.Helper()
	res, err := b.QueryList(&api.QueryRequest{Limit: 100})
	if err != nil || res.Error != "" {
		t.Fatalf("%v %s", err, res.Error)
	}
	var out []string
	for _, e := range res.Entries {
		out = append(out, e.Message)
	}
	return out
}

func seed(t *testing.T, b *sqliteBackend, now time.Time) {
	t.Helper()
	_, err := b.Insert(&api.InsertRequest{Entries: []*api.LogEntry{
		{Timestamp: now.Add(-48 * time.Hour), Hostname: "h", Application: "a", Message: "old"},
		{Timestamp: now.Add(-24 * time.Hour), Hostname: "h", Application: "a", Message: "edge"},
		{Timestamp: now.Add(-1 * time.Hour), Hostname: "h", Application: "a", Message: "fresh"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitInserted(t, b, 3)
}

func TestRetentionPurgesOlderThanRetention(t *testing.T) {
	b := startBackend(t, "?retention=1d")
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	seed(t, b, now)
	b.handleRetention(now)
	got := messages(t, b)
	if len(got) != 2 || got[0] != "fresh" || got[1] != "edge" {
		t.Errorf("got %v, want [fresh edge]", got)
	}
}

func TestRetentionInfiniteKeepsEverything(t *testing.T) {
	b := startBackend(t, "")
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	seed(t, b, now)
	b.handleRetention(now)
	if got := messages(t, b); len(got) != 3 {
		t.Errorf("got %v, want 3 entries", got)
	}
}

func TestRetentionRemovesSearchableText(t *testing.T) {
	b := startBackend(t, "?retention=1d")
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	seed(t, b, now)
	b.handleRetention(now)
	res, err := b.QueryList(&api.QueryRequest{Limit: 100, Message: "old"})
	if err != nil || res.Error != "" || len(res.Entries) != 0 {
		t.Errorf("purged entry still matches: %v %s %v", err, res.Error, res.Entries)
	}
}
