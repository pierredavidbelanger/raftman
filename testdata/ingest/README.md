# Fresh-ingest golden set

Produced by the current binary from `../legacy/packets.json` and
`../legacy/queries.json` with `../legacy/generate.py`. `TestIngestMatchesGolden`
replays the packets through the syslog frontends and compares the API answers
and the raw rows against this directory.

`diff -r ../legacy/golden golden` lists every deliberate difference between
reading a database written by raftman 1.0.x and a fresh ingest. Regenerate
after an intended behavior change, never to make a failing test pass:

    go build -o /tmp/raftman ./cmd/raftman
    python3 testdata/legacy/generate.py /tmp/raftman testdata/ingest
