package main

// Characterization tests. They run the real binary (this test binary re-executes
// itself with RAFTMAN_TEST_CHILD=1, which calls main) so they exercise the CLI,
// the network frontends, signal handling and the SQLite file exactly as users do.
// They must keep passing unchanged through the modernization; see spec/contract.md.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// legacyYear is the year testdata/legacy was generated in. RFC3164 packets carry
// no year, so go-syslog stamps them with the current one; goldens produced from a
// live ingest are compared after substituting the current year for this one.
const legacyYear = "2026"

const (
	legacyDir   = "testdata/legacy"
	packetCount = 12
)

func TestMain(m *testing.M) {
	if os.Getenv("RAFTMAN_TEST_CHILD") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// ---------------------------------------------------------------- child process

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type child struct {
	cmd    *exec.Cmd
	output *lockedBuffer
	exited chan struct{}
	err    error
}

func spawn(t *testing.T, args ...string) *child {
	t.Helper()
	c := &child{output: &lockedBuffer{}, exited: make(chan struct{})}
	c.cmd = exec.Command(os.Args[0], args...)
	c.cmd.Env = append(os.Environ(), "RAFTMAN_TEST_CHILD=1")
	c.cmd.Stdout = c.output
	c.cmd.Stderr = c.output
	if err := c.cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	go func() {
		c.err = c.cmd.Wait()
		close(c.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-c.exited:
		default:
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
	})
	return c
}

func (c *child) running() bool {
	select {
	case <-c.exited:
		return false
	default:
		return true
	}
}

func (c *child) signal(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := c.cmd.Process.Signal(sig); err != nil {
		t.Fatalf("signal child: %v", err)
	}
}

// wait blocks until the child exits and returns its exit code (-1 when killed by a signal).
func (c *child) wait(t *testing.T, d time.Duration) int {
	t.Helper()
	select {
	case <-c.exited:
	case <-time.After(d):
		// SIGQUIT makes the Go runtime dump all goroutines before dying.
		_ = c.cmd.Process.Signal(syscall.SIGQUIT)
		select {
		case <-c.exited:
		case <-time.After(5 * time.Second):
		}
		t.Fatalf("child did not exit within %s; output:\n%s", d, c.output)
	}
	if c.err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(c.err, &ee) {
		return ee.ExitCode()
	}
	t.Fatalf("wait child: %v", c.err)
	return -1
}

// ---------------------------------------------------------------- harness

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// harness is a child running the full default set of frontends on free localhost ports.
type harness struct {
	*child
	db      string
	udp5424 int
	tcp5424 int
	udp3164 int
	apiPort int
	api     string // http://127.0.0.1:PORT/api/
}

func startHarness(t *testing.T, db string) *harness {
	t.Helper()
	if db == "" {
		db = filepath.Join(t.TempDir(), "logs.db")
	}
	h := &harness{
		db:      db,
		udp5424: freeUDPPort(t),
		tcp5424: freeTCPPort(t),
		udp3164: freeUDPPort(t),
		apiPort: freeTCPPort(t),
	}
	h.api = fmt.Sprintf("http://127.0.0.1:%d/api/", h.apiPort)
	h.child = spawn(t,
		"-backend", "sqlite://"+db,
		"-frontend", fmt.Sprintf("syslog+udp://127.0.0.1:%d", h.udp5424),
		"-frontend", fmt.Sprintf("syslog+tcp://127.0.0.1:%d", h.tcp5424),
		"-frontend", fmt.Sprintf("syslog+udp://127.0.0.1:%d?format=RFC3164", h.udp3164),
		"-frontend", fmt.Sprintf("api+http://127.0.0.1:%d/api/", h.apiPort),
	)
	waitReady(t, h.child, h.api+"stat")
	return h
}

func waitReady(t *testing.T, c *child, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !c.running() {
			t.Fatalf("child exited before becoming ready; output:\n%s", c.output)
		}
		res, err := http.Get(url)
		if err == nil {
			res.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child never answered on %s; output:\n%s", url, c.output)
}

func call(t *testing.T, url, method string, body *string) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(*body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, b
}

func str(s string) *string { return &s }

// count returns the number of entries visible through the stat endpoint.
func count(t *testing.T, api string) int {
	t.Helper()
	_, body := call(t, api+"stat", "POST", str(`{"Limit":500}`))
	var res struct {
		Stat map[string]map[string]uint64
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("stat response %q: %v", body, err)
	}
	n := 0
	for _, apps := range res.Stat {
		for _, c := range apps {
			n += int(c)
		}
	}
	return n
}

func waitCount(t *testing.T, api string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	got := -1
	for time.Now().Before(deadline) {
		if got = count(t, api); got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected %d entries, got %d", want, got)
}

// ---------------------------------------------------------------- fixture data

type packet struct {
	Transport string
	Data      string
}

type query struct {
	Name     string
	Endpoint string
	Method   string
	Body     *string
}

func loadJSON(t *testing.T, name string, v interface{}) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(legacyDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func loadPackets(t *testing.T) []packet {
	var p []packet
	loadJSON(t, "packets.json", &p)
	if len(p) != packetCount {
		t.Fatalf("packets.json has %d packets, expected %d", len(p), packetCount)
	}
	return p
}

func loadQueries(t *testing.T) []query {
	var q []query
	loadJSON(t, "queries.json", &q)
	return q
}

func readGolden(t *testing.T, name string) (int, []byte) {
	t.Helper()
	st, err := os.ReadFile(filepath.Join(legacyDir, "golden", name+".status"))
	if err != nil {
		t.Fatal(err)
	}
	var status int
	if _, err := fmt.Sscan(string(st), &status); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(legacyDir, "golden", name+".body"))
	if err != nil {
		t.Fatal(err)
	}
	return status, body
}

// fixYear rewrites the RFC3164 timestamps of the fixture to the current year.
func fixYear(b []byte) []byte {
	year := fmt.Sprint(time.Now().Year())
	if year == legacyYear {
		return b
	}
	return bytes.ReplaceAll(b, []byte(legacyYear+"-11-21"), []byte(year+"-11-21"))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func legacyDBCopy(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "legacy.db")
	copyFile(t, filepath.Join(legacyDir, "legacy.db"), dst)
	return dst
}

func sendPackets(t *testing.T, h *harness, packets []packet) {
	t.Helper()
	tcp, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", h.tcp5424))
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	for _, p := range packets {
		switch p.Transport {
		case "udp5424":
			sendUDP(t, h.udp5424, p.Data)
		case "udp3164":
			sendUDP(t, h.udp3164, p.Data)
		case "tcp5424":
			if _, err := io.WriteString(tcp, p.Data+"\n"); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unknown transport %q", p.Transport)
		}
		time.Sleep(10 * time.Millisecond) // keep rowid order equal to packet order
	}
}

func sendUDP(t *testing.T, port int, data string) {
	t.Helper()
	c, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := io.WriteString(c, data); err != nil {
		t.Fatal(err)
	}
}

func checkQueries(t *testing.T, api string, adjust func([]byte) []byte) {
	t.Helper()
	for _, q := range loadQueries(t) {
		t.Run(q.Name, func(t *testing.T) {
			wantStatus, wantBody := readGolden(t, q.Name)
			wantBody = adjust(wantBody)
			gotStatus, gotBody := call(t, api+q.Endpoint, q.Method, q.Body)
			if gotStatus != wantStatus {
				t.Errorf("status: got %d, want %d", gotStatus, wantStatus)
			}
			if !bytes.Equal(gotBody, wantBody) {
				t.Errorf("body:\n got: %s\nwant: %s", gotBody, wantBody)
			}
		})
	}
}

// dumpRows reproduces testdata/legacy/rows.txt from a database file.
func dumpRows(t *testing.T, path string) []byte {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var out bytes.Buffer
	dump := func(header, q string, n int) {
		fmt.Fprintf(&out, "-- %s\n", header)
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			cols := make([]string, n)
			ptrs := make([]interface{}, n)
			for i := range cols {
				ptrs[i] = &cols[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out.WriteString(strings.Join(cols, "\t") + "\n")
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	// CAST keeps the driver from converting the DATETIME column into time.Time,
	// so we see the exact string stored on disk.
	dump("SELECT rowid, ts, host, app FROM logh ORDER BY rowid",
		"SELECT rowid, CAST(ts AS TEXT), host, app FROM logh ORDER BY rowid", 4)
	dump("SELECT docid, msg FROM logb ORDER BY docid",
		"SELECT docid, msg FROM logb ORDER BY docid", 2)
	return out.Bytes()
}

// ---------------------------------------------------------------- tests

// TestLegacyFixture opens a database written by the pre-modernization binary and
// checks every query returns byte-for-byte what that binary returned.
func TestLegacyFixture(t *testing.T) {
	h := startHarness(t, legacyDBCopy(t))
	checkQueries(t, h.api, func(b []byte) []byte { return b })
}

// TestIngestMatchesLegacy feeds the fixture packets through the syslog frontends
// into a fresh database and checks both the API output and the raw table content
// match what the pre-modernization binary produced.
func TestIngestMatchesLegacy(t *testing.T) {
	h := startHarness(t, "")
	sendPackets(t, h, loadPackets(t))
	waitCount(t, h.api, packetCount)
	checkQueries(t, h.api, fixYear)

	h.signal(t, os.Interrupt)
	if code := h.wait(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d; output:\n%s", code, h.output)
	}

	want, err := os.ReadFile(filepath.Join(legacyDir, "rows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want = fixYear(want)
	if got := dumpRows(t, h.db); !bytes.Equal(got, want) {
		t.Errorf("raw rows differ:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestLegacyFixtureAcceptsNewRows checks a legacy database keeps working after
// the current binary appends to it.
func TestLegacyFixtureAcceptsNewRows(t *testing.T) {
	h := startHarness(t, legacyDBCopy(t))
	sendUDP(t, h.udp5424, "<134>1 2030-01-01T00:00:00Z newhost newapp - - - appended later")
	waitCount(t, h.api, packetCount+1)
	_, body := call(t, h.api+"list", "POST", str(`{"Limit":1}`))
	want := `{"Entries":[{"Timestamp":"2030-01-01T00:00:00Z","Hostname":"newhost","Application":"newapp","Message":"appended later"}]}` + "\n"
	if string(body) != want {
		t.Errorf("got %s want %s", body, want)
	}
	_, body = call(t, h.api+"list", "POST", str(`{"Limit":1,"Message":"favicon"}`))
	if !strings.Contains(string(body), "GET /favicon.ico 404") {
		t.Errorf("old rows not searchable anymore: %s", body)
	}
}

func TestCLIRejectsBadArguments(t *testing.T) {
	db := "sqlite://" + filepath.Join(t.TempDir(), "logs.db")
	api := fmt.Sprintf("api+http://127.0.0.1:%d/api/", freeTCPPort(t))
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"two backends", []string{"-backend", db, "-backend", db}, "At most one backend"},
		{"unknown backend scheme", []string{"-backend", "foo:///x"}, "Invalid backend foo"},
		{"sqlite without path", []string{"-backend", "sqlite://"}, "Invalid SQLite database file path"},
		{"unparseable URL", []string{"-backend", ":bad"}, "missing protocol scheme"},
		{"bad retention", []string{"-backend", db + "?retention=3x", "-frontend", api}, "invalid (INF|wdhm) duration '3x'"},
		{"bad batchSize", []string{"-backend", db + "?batchSize=abc", "-frontend", api}, "invalid syntax"},
		{"bad insertQueueSize", []string{"-backend", db + "?insertQueueSize=1.5", "-frontend", api}, "invalid syntax"},
		{"bad queryQueueSize", []string{"-backend", db + "?queryQueueSize=x", "-frontend", api}, "invalid syntax"},
		{"bad timeout", []string{"-backend", db + "?timeout=abc", "-frontend", api}, "invalid duration"},
		{"unknown frontend scheme", []string{"-backend", db, "-frontend", "foo://127.0.0.1:1"}, "Invalid frontend foo"},
		{"bad syslog format", []string{"-backend", db, "-frontend", "syslog+udp://127.0.0.1:0?format=RFC9999"}, "Invalid syslog format RFC9999"},
		{"bad syslog queueSize", []string{"-backend", db, "-frontend", "syslog+udp://127.0.0.1:0?queueSize=x"}, "invalid syntax"},
		{"bad syslog timeout", []string{"-backend", db, "-frontend", "syslog+udp://127.0.0.1:0?timeout=x"}, "invalid duration"},
		{"syslog without host", []string{"-backend", db, "-frontend", "syslog+udp://"}, "Empty host"},
		{"api without host", []string{"-backend", db, "-frontend", "api+http:///api/"}, "Empty host"},
		{"ui without host", []string{"-backend", db, "-frontend", "ui+http:///"}, "Empty host"},
		{"unknown flag", []string{"-nope"}, "flag provided but not defined: -nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := spawn(t, c.args...)
			code := ch.wait(t, 10*time.Second)
			out := ch.output.String()
			if code == 0 {
				t.Errorf("exit code 0, expected failure; output:\n%s", out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("output does not contain %q:\n%s", c.want, out)
			}
		})
	}
}

// TestHTTPMethodsAndPrefix pins the routing rules: the URL path is used as prefix
// verbatim, only POST bodies are decoded, anything else is an empty request.
func TestHTTPMethodsAndPrefix(t *testing.T) {
	port := freeTCPPort(t)
	ch := spawn(t,
		"-backend", "sqlite://"+legacyDBCopy(t),
		"-frontend", fmt.Sprintf("api+http://127.0.0.1:%d/custom/prefix/", port),
	)
	base := fmt.Sprintf("http://127.0.0.1:%d/custom/prefix/", port)
	waitReady(t, ch, base+"stat")

	cases := []struct {
		name, url, method string
		body              *string
		wantStatus        int
		wantBody          string
	}{
		{"GET list is an empty request", base + "list", "GET", nil, 200, "{}\n"},
		{"PUT body is ignored", base + "list", "PUT", str(`{"Limit":1}`), 200, "{}\n"},
		{"POST body is decoded", base + "list", "POST", str(`{"Limit":1}`), 200,
			`{"Entries":[{"Timestamp":"2026-11-21T10:00:09Z","Hostname":"host3","Application":"cron","Message":"job started"}]}` + "\n"},
		{"unknown path", base + "other", "GET", nil, 404, "404 page not found\n"},
		{"prefix without trailing path", fmt.Sprintf("http://127.0.0.1:%d/api/list", port), "GET", nil, 404, "404 page not found\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body := call(t, c.url, c.method, c.body)
			if status != c.wantStatus || string(body) != c.wantBody {
				t.Errorf("got %d %q, want %d %q", status, body, c.wantStatus, c.wantBody)
			}
		})
	}
}

// TestUIFrontend pins that the ui frontend serves the static files and the API
// under <path>api/.
func TestUIFrontend(t *testing.T) {
	port := freeTCPPort(t)
	ch := spawn(t,
		"-backend", "sqlite://"+legacyDBCopy(t),
		"-frontend", fmt.Sprintf("ui+http://127.0.0.1:%d/", port),
	)
	base := fmt.Sprintf("http://127.0.0.1:%d/", port)
	waitReady(t, ch, base+"api/stat")

	status, body := call(t, base, "GET", nil)
	if status != 200 || !strings.Contains(string(body), "<script") {
		t.Errorf("index: got %d %q", status, body)
	}
	for _, f := range []string{"index.html", "index.js", "favicon.ico", "logo-32.png", "logo-96.png"} {
		if status, body := call(t, base+f, "GET", nil); status != 200 || len(body) == 0 {
			t.Errorf("%s: got %d, %d bytes", f, status, len(body))
		}
	}
	if status, _ := call(t, base+"nope.txt", "GET", nil); status != 404 {
		t.Errorf("missing file: got %d, want 404", status)
	}
	if n := count(t, base+"api/"); n != packetCount {
		t.Errorf("api under ui: got %d entries, want %d", n, packetCount)
	}
}

// TestSyslogMissingTimestamp pins finding F14: an RFC5424 packet whose timestamp
// is "-" is stored with the zero time instead of the arrival time, because the
// parser yields a zero time.Time and the fallback in toLogEntry never fires.
// Candidate fix in phase 4; until then this is the observed behavior.
func TestSyslogMissingTimestamp(t *testing.T) {
	h := startHarness(t, "")
	sendUDP(t, h.udp5424, "<134>1 - myhost myapp - - - no timestamp")
	waitCount(t, h.api, 1)
	_, body := call(t, h.api+"list", "POST", str(`{"Limit":1}`))
	want := `{"Entries":[{"Timestamp":"0001-01-01T00:00:00Z","Hostname":"myhost","Application":"myapp","Message":"no timestamp"}]}` + "\n"
	if string(body) != want {
		t.Errorf("got %s want %s", body, want)
	}
}

func TestSyslogFormatIsCaseInsensitive(t *testing.T) {
	udp, api := freeUDPPort(t), freeTCPPort(t)
	ch := spawn(t,
		"-backend", "sqlite://"+filepath.Join(t.TempDir(), "logs.db"),
		"-frontend", fmt.Sprintf("syslog+udp://127.0.0.1:%d?format=rfc3164", udp),
		"-frontend", fmt.Sprintf("api+http://127.0.0.1:%d/api/", api),
	)
	base := fmt.Sprintf("http://127.0.0.1:%d/api/", api)
	waitReady(t, ch, base+"stat")
	sendUDP(t, udp, "<34>Nov 21 10:00:08 host3 su: hello")
	waitCount(t, base, 1)
	_, body := call(t, base+"stat", "POST", str(`{"Limit":10}`))
	if string(body) != `{"Stat":{"host3":{"su":1}}}`+"\n" {
		t.Errorf("got %s", body)
	}
}

func TestSyslogTCPMultipleLinesOneConnection(t *testing.T) {
	h := startHarness(t, "")
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", h.tcp5424))
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(c, "<134>1 2019-01-01T00:00:00Z h a - - - one\n<134>1 2019-01-01T00:00:01Z h a - - - two\n")
	io.WriteString(c, "<134>1 2019-01-01T00:00:02Z h a - - - three\n")
	c.Close()
	waitCount(t, h.api, 3)
}

// TestShutdownSIGINT checks that committed entries survive an interrupt and the
// process exits cleanly.
func TestShutdownSIGINT(t *testing.T) {
	h := startHarness(t, "")
	sendPackets(t, h, loadPackets(t))
	waitCount(t, h.api, packetCount)
	h.signal(t, os.Interrupt)
	if code := h.wait(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d; output:\n%s", code, h.output)
	}
	h2 := startHarness(t, h.db)
	waitCount(t, h2.api, packetCount)
}

// TestShutdownSIGTERM: docker stop sends SIGTERM. Finding F2: the current binary
// does not trap it and dies with the insert queue unflushed.
func TestShutdownSIGTERM(t *testing.T) {
	t.Skip("F2: SIGTERM is not handled yet; enable in phase 2")
	h := startHarness(t, "")
	sendPackets(t, h, loadPackets(t))
	waitCount(t, h.api, packetCount)
	h.signal(t, syscall.SIGTERM)
	if code := h.wait(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d; output:\n%s", code, h.output)
	}
	h2 := startHarness(t, h.db)
	waitCount(t, h2.api, packetCount)
}

// TestShutdownFlushesQueue: finding F3, entries still queued at shutdown may be
// dropped by the current binary.
func TestShutdownFlushesQueue(t *testing.T) {
	t.Skip("F3: the insert queue is not drained on shutdown yet; enable in phase 2")
	h := startHarness(t, "")
	for i := 0; i < 200; i++ {
		sendUDP(t, h.udp5424, fmt.Sprintf("<134>1 2019-01-01T00:00:00Z h a - - - line %d", i))
	}
	h.signal(t, os.Interrupt)
	if code := h.wait(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d; output:\n%s", code, h.output)
	}
	h2 := startHarness(t, h.db)
	waitCount(t, h2.api, 200)
}
