#!/usr/bin/env python3
"""Generate a golden set by running a raftman binary against packets.json.

Usage: generate.py /path/to/raftman OUTDIR [--keep-db]

Starts the binary with a fresh database, feeds it packets.json over the three
syslog transports, runs every query in queries.json against the API, and
writes into OUTDIR:

  golden/<name>.status HTTP status of each query
  golden/<name>.body   raw HTTP response body of each query
  rows.txt             raw content of the logh/logb tables
  legacy.db            the resulting SQLite database (only with --keep-db)

testdata/legacy was produced by the pre-modernization binary and pins how old
databases are read. testdata/ingest is produced by the current binary and pins
what a fresh ingest looks like; the diff between the two golden directories is
the list of deliberate behavior changes.
"""
import http.client, json, os, shutil, signal, socket, sqlite3, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))

def free_port():
    s = socket.socket(); s.bind(("127.0.0.1", 0)); p = s.getsockname()[1]; s.close(); return p

def main():
    ref, out = sys.argv[1], os.path.abspath(sys.argv[2])
    keep_db = "--keep-db" in sys.argv
    os.makedirs(out, exist_ok=True)
    udp5424, tcp5424, udp3164, api = (free_port() for _ in range(4))
    tmp = tempfile.mkdtemp(prefix="raftman-legacy-")
    db = os.path.join(tmp, "legacy.db")
    args = [ref,
            "-backend", f"sqlite://{db}",
            "-frontend", f"syslog+udp://127.0.0.1:{udp5424}",
            "-frontend", f"syslog+tcp://127.0.0.1:{tcp5424}",
            "-frontend", f"syslog+udp://127.0.0.1:{udp3164}?format=RFC3164",
            "-frontend", f"api+http://127.0.0.1:{api}/api/"]
    print("run:", " ".join(args))
    proc = subprocess.Popen(args)

    def call(endpoint, method, body=None):
        c = http.client.HTTPConnection("127.0.0.1", api, timeout=10)
        c.request(method, f"/api/{endpoint}", body=body.encode("utf-8") if body is not None else None)
        r = c.getresponse(); data = r.read(); c.close()
        return r.status, data

    for _ in range(100):
        try:
            if call("stat", "GET")[0] == 200: break
        except OSError:
            time.sleep(0.05)
    else:
        proc.kill(); sys.exit("server never became ready")

    packets = json.load(open(os.path.join(HERE, "packets.json"), encoding="utf-8"))
    tcp = socket.create_connection(("127.0.0.1", tcp5424))
    for p in packets:
        data = p["data"].encode("utf-8")
        if p["transport"] == "udp5424":
            socket.socket(socket.AF_INET, socket.SOCK_DGRAM).sendto(data, ("127.0.0.1", udp5424))
        elif p["transport"] == "udp3164":
            socket.socket(socket.AF_INET, socket.SOCK_DGRAM).sendto(data, ("127.0.0.1", udp3164))
        elif p["transport"] == "tcp5424":
            tcp.sendall(data + b"\n")
        else:
            sys.exit("unknown transport " + p["transport"])
        time.sleep(0.02)  # keep arrival order deterministic
    tcp.close()

    for _ in range(200):
        st, body = call("stat", "POST", '{"Limit":500}')
        total = sum(sum(apps.values()) for apps in json.loads(body).get("Stat", {}).values())
        if total == len(packets): break
        time.sleep(0.05)
    else:
        proc.kill(); sys.exit(f"only {total} of {len(packets)} packets ingested")

    golden = os.path.join(out, "golden")
    shutil.rmtree(golden, ignore_errors=True); os.makedirs(golden)
    for q in json.load(open(os.path.join(HERE, "queries.json"), encoding="utf-8")):
        st, body = call(q["endpoint"], q["method"], q.get("body"))
        open(os.path.join(golden, q["name"] + ".status"), "w").write(f"{st}\n")
        open(os.path.join(golden, q["name"] + ".body"), "wb").write(body)
        print(f"{q['name']:28s} {st} {body[:70]!r}")

    proc.send_signal(signal.SIGINT)
    proc.wait(timeout=10)
    print("exit code", proc.returncode)

    if keep_db:
        shutil.copy(db, os.path.join(out, "legacy.db"))
    con = sqlite3.connect(db)
    with open(os.path.join(out, "rows.txt"), "w", encoding="utf-8") as f:
        f.write("-- SELECT rowid, ts, host, app FROM logh ORDER BY rowid\n")
        for row in con.execute("SELECT rowid, ts, host, app FROM logh ORDER BY rowid"):
            f.write("\t".join(map(str, row)) + "\n")
        try:
            f.write("-- SELECT docid, msg FROM logb ORDER BY docid\n")
            for row in con.execute("SELECT docid, msg FROM logb ORDER BY docid"):
                f.write("\t".join(map(str, row)) + "\n")
        except sqlite3.OperationalError as e:
            f.write(f"-- logb not readable by python sqlite: {e}\n")
    con.close()
    shutil.rmtree(tmp)

if __name__ == "__main__":
    main()
