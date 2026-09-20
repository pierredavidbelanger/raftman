# Legacy persistence fixture

`legacy.db` was written by the pre-modernization binary (commit `6a8f36a`, built
with Go 1.27 and the 2017 `mattn/go-sqlite3` pseudo-version) fed with the packets
in `packets.json`. The `golden/` responses are what that binary answered to
`queries.json`. `rows.txt` is the raw table content. The RFC3164 packets carry no
year and were stamped with 2026, the year of generation.

The tests in `main_test.go` replay `queries.json` against `legacy.db` and against
a fresh ingest of `packets.json`, and compare byte for byte.

Do not regenerate these files with a newer binary; that would defeat their purpose.
To regenerate with the reference binary:

    git worktree add /tmp/raftman-ref 6a8f36a
    (cd /tmp/raftman-ref && go install github.com/mjibson/esc@latest && go generate && go build -o raftman-ref)
    python3 testdata/legacy/generate.py /tmp/raftman-ref/raftman-ref
